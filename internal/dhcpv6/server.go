package dhcpv6

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/dhcp-server/dhcp-server/internal/config"
	"github.com/dhcp-server/dhcp-server/internal/metrics"
	"github.com/dhcp-server/dhcp-server/internal/models"
	"github.com/dhcp-server/dhcp-server/internal/store"
	"github.com/google/uuid"
	"golang.org/x/net/ipv6"
)

const (
	DefaultV6LeaseTime    = 3600
	DefaultV6MaxLeaseTime = 86400
)

// sanitizeUTF8 removes invalid UTF-8 byte sequences and NUL bytes so that
// client-provided strings can safely be stored in PostgreSQL TEXT/VARCHAR columns.
func sanitizeUTF8(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimSpace(s)
}

func (s *Server) logIPAllocation(ctx context.Context, scope *models.Scope, duid, iaid string, ip net.IP, prefix *net.IPNet, action string, relay *relayContext, hostname string) {
	if scope == nil {
		return
	}
	var relayIP net.IP
	if relay != nil {
		relayIP = relay.linkAddr
	}
	info := map[string]string{
		"iaid":     iaid,
		"hostname": hostname,
	}
	infoBytes, _ := json.Marshal(info)
	log := &models.IPAllocationLog{
		ID:         uuid.New().String(),
		NodeID:     s.cfg.Cluster.NodeID,
		ScopeID:    scope.ID,
		ScopeName:  scope.Name,
		DUID:       duid,
		IAID:       iaid,
		IPAddr:     ip,
		Prefix:     prefix,
		Action:     action,
		ClientInfo: infoBytes,
		RelayIP:    relayIP,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.store.CreateIPAllocationLog(ctx, log); err != nil {
		s.logger.Error("create ip allocation log", "err", err)
	}
}

// relayHop records one relay layer of an incoming relay-forward chain, from
// outermost to innermost. It is everything needed to re-encapsulate the
// relay-reply for that layer.
type relayHop struct {
	hopCount byte
	linkAddr net.IP
	peerAddr net.IP
	options  Options // relay options excluding Relay-Message
}

// relayContext holds information from a DHCPv6 relay-forward message.
type relayContext struct {
	hopCount     byte
	linkAddr     net.IP
	peerAddr     net.IP
	interfaceID  []byte
	relayAddr    *net.UDPAddr
	relayOptions Options // original relay options (excluding Relay-Message)
	hops         []relayHop
}

type Server struct {
	cfg        *config.Config
	store      *store.Store
	logger     *slog.Logger
	conn       *net.UDPConn
	mu         sync.RWMutex
	scopes     []*models.Scope
	serverDUID []byte
	quit       chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup
}

func NewServer(cfg *config.Config, s *store.Store, logger *slog.Logger) *Server {
	return &Server{
		cfg:    cfg,
		store:  s,
		logger: logger,
		quit:   make(chan struct{}),
	}
}

func (s *Server) Start(ctx context.Context) error {
	if err := s.reloadScopes(ctx); err != nil {
		return err
	}
	go s.refreshLoop(ctx)

	// Load the persistent server DUID (generated once and stored in the
	// database) so that it survives restarts and stays identical across
	// cluster nodes sharing the same database.
	s.serverDUID = s.loadOrCreateDUID(ctx)

	addr, err := net.ResolveUDPAddr("udp6", s.cfg.Server.V6Listen)
	if err != nil {
		return fmt.Errorf("resolve udp6: %w", err)
	}
	conn, err := net.ListenUDP("udp6", addr)
	if err != nil {
		return fmt.Errorf("listen udp6: %w", err)
	}
	s.conn = conn

	// Join the All_DHCP_Relay_Agents_and_Servers multicast group (ff02::1:2)
	// so that directly connected clients' Solicits reach us; without this
	// only relayed traffic is received.
	s.joinMulticastGroup()
	s.logger.Info("dhcpv6 server listening", "addr", addr.String())

	s.wg.Add(1)
	go s.serveLoop()
	return nil
}

func (s *Server) Stop() error {
	s.stopOnce.Do(func() {
		close(s.quit)
		if s.conn != nil {
			s.conn.Close()
		}
	})
	s.wg.Wait()
	return nil
}

// joinMulticastGroup subscribes the socket to ff02::1:2 on the configured
// interface, or on every multicast-capable interface when none is configured.
// Failures are logged but not fatal so relay-only deployments keep working.
func (s *Server) joinMulticastGroup() {
	pktConn := ipv6.NewPacketConn(s.conn)
	group := &net.UDPAddr{IP: AllDHCPRelayAgentsAndServers}

	if ifname := s.cfg.Server.Interface; ifname != "" {
		ifi, err := net.InterfaceByName(ifname)
		if err != nil {
			s.logger.Warn("multicast interface not found, relay-only mode", "interface", ifname, "err", err)
			return
		}
		if err := pktConn.JoinGroup(ifi, group); err != nil {
			s.logger.Warn("join dhcpv6 multicast group failed", "interface", ifname, "err", err)
		}
		return
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		s.logger.Warn("list interfaces for multicast join failed", "err", err)
		return
	}
	joined := 0
	for i := range ifaces {
		ifi := &ifaces[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if err := pktConn.JoinGroup(ifi, group); err != nil {
			s.logger.Debug("join dhcpv6 multicast group skipped", "interface", ifi.Name, "err", err)
			continue
		}
		joined++
	}
	if joined == 0 {
		s.logger.Warn("not joined to any dhcpv6 multicast group; only relayed traffic will be received")
	}
}

func (s *Server) reloadScopes(ctx context.Context) error {
	all, err := s.store.ListScopes(ctx)
	if err != nil {
		return err
	}
	var v6 []*models.Scope
	for _, sc := range all {
		if sc.V6 {
			v6 = append(v6, sc)
		}
	}
	s.mu.Lock()
	s.scopes = v6
	s.mu.Unlock()
	return nil
}

func (s *Server) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.quit:
			return
		case <-ticker.C:
			if err := s.store.ReleaseExpiredV6Leases(ctx); err != nil {
				s.logger.Error("release expired v6 leases", "err", err)
			}
			if err := s.store.ReleaseExpiredV6Prefixes(ctx); err != nil {
				s.logger.Error("release expired v6 prefixes", "err", err)
			}
			if err := s.reloadScopes(ctx); err != nil {
				s.logger.Error("reload v6 scopes", "err", err)
			}
			if leases, err := s.store.ListActiveV6Leases(ctx); err == nil {
				metrics.LeasesActive.WithLabelValues("v6").Set(float64(len(leases)))
			}
		}
	}
}

func (s *Server) serveLoop() {
	defer s.wg.Done()
	buf := make([]byte, 4096)
	for {
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				s.logger.Error("read udp6", "err", err)
				continue
			}
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		go s.handlePacket(data, addr)
	}
}

func (s *Server) handlePacket(data []byte, addr *net.UDPAddr) {
	// First byte determines if this is a relay message.
	if len(data) < 1 {
		return
	}
	if data[0] == MsgTypeRelayForw {
		s.handleRelayForward(data, addr)
		return
	}

	req, err := ParsePacket(data)
	if err != nil {
		s.logger.Warn("parse v6 packet", "err", err, "from", addr)
		return
	}

	s.logger.Info("v6 packet", "type", req.MessageType, "from", addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch req.MessageType {
	case MsgTypeSolicit:
		s.handleSolicit(ctx, req, addr, nil)
	case MsgTypeRequest, MsgTypeRenew, MsgTypeRebind:
		s.handleRequest(ctx, req, addr, nil)
	case MsgTypeConfirm:
		s.handleConfirm(ctx, req, addr, nil)
	case MsgTypeRelease:
		s.handleRelease(ctx, req, addr, nil)
	case MsgTypeDecline:
		s.handleDecline(ctx, req, addr, nil)
	case MsgTypeInformationRequest:
		s.handleInformationRequest(ctx, req, addr, nil)
	default:
		s.logger.Warn("unsupported v6 message type", "type", req.MessageType)
	}
}

func (s *Server) handleRelayForward(data []byte, addr *net.UDPAddr) {
	rp, err := ParseRelayPacket(data)
	if err != nil {
		s.logger.Warn("parse v6 relay packet", "err", err, "from", addr)
		return
	}

	s.logger.Info("v6 relay-forward", "from", addr, "link_addr", rp.LinkAddr, "peer_addr", rp.PeerAddr, "hop_count", rp.HopCount)

	// Recursively unwrap relay messages until we reach the inner client message.
	innerPacket, innerRelay, err := rp.Options.GetRelayMsg()
	if err != nil {
		s.logger.Warn("relay message missing", "err", err, "from", addr)
		return
	}

	relay := &relayContext{
		hopCount:     rp.HopCount,
		linkAddr:     rp.LinkAddr,
		peerAddr:     rp.PeerAddr,
		relayAddr:    addr,
		relayOptions: rp.Options.CopyRelayOptions(),
	}
	relay.hops = append(relay.hops, relayHop{
		hopCount: rp.HopCount,
		linkAddr: rp.LinkAddr,
		peerAddr: rp.PeerAddr,
		options:  rp.Options.CopyRelayOptions(),
	})
	if ifaceID, ok := rp.Options.InterfaceID(); ok {
		relay.interfaceID = ifaceID
	}

	// Handle multi-hop relays: keep unwrapping, remember every layer for the
	// relay-reply encapsulation, and use the innermost relay context for
	// scope selection.
	for innerRelay != nil {
		rp = innerRelay
		relay.hopCount = rp.HopCount
		relay.linkAddr = rp.LinkAddr
		relay.peerAddr = rp.PeerAddr
		relay.relayOptions = rp.Options.CopyRelayOptions()
		relay.hops = append(relay.hops, relayHop{
			hopCount: rp.HopCount,
			linkAddr: rp.LinkAddr,
			peerAddr: rp.PeerAddr,
			options:  rp.Options.CopyRelayOptions(),
		})
		if ifaceID, ok := rp.Options.InterfaceID(); ok {
			relay.interfaceID = ifaceID
		}
		innerPacket, innerRelay, err = rp.Options.GetRelayMsg()
		if err != nil {
			s.logger.Warn("nested relay message missing", "err", err, "from", addr)
			return
		}
	}

	if innerPacket == nil {
		s.logger.Warn("relay message did not contain a client message", "from", addr)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch innerPacket.MessageType {
	case MsgTypeSolicit:
		s.handleSolicit(ctx, innerPacket, addr, relay)
	case MsgTypeRequest, MsgTypeRenew, MsgTypeRebind:
		s.handleRequest(ctx, innerPacket, addr, relay)
	case MsgTypeConfirm:
		s.handleConfirm(ctx, innerPacket, addr, relay)
	case MsgTypeRelease:
		s.handleRelease(ctx, innerPacket, addr, relay)
	case MsgTypeDecline:
		s.handleDecline(ctx, innerPacket, addr, relay)
	case MsgTypeInformationRequest:
		s.handleInformationRequest(ctx, innerPacket, addr, relay)
	default:
		s.logger.Warn("unsupported inner v6 message type", "type", innerPacket.MessageType)
	}
}

// serverIDMatches reports whether the message may be processed by this
// server per RFC 8415: when the client includes a Server ID option it must
// equal our DUID, otherwise the message must be discarded.
func (s *Server) serverIDMatches(req *Packet) bool {
	opt, ok := req.Options.Get(OptServerID)
	if !ok {
		return true
	}
	return bytes.Equal(opt.Data, s.serverDUID)
}

func (s *Server) handleSolicit(ctx context.Context, req *Packet, addr *net.UDPAddr, relay *relayContext) {
	// A message carrying both IA_NA and IA_PD (typical for CPE routers) must
	// get answers for every IA in a single Reply (RFC 8415 §18.3.1).
	if hasIANAOpt(req) && s.hasPD(req) {
		s.handleSolicitCombined(ctx, req, addr, relay)
		return
	}
	if s.hasPD(req) {
		s.handleSolicitPD(ctx, req, addr, relay)
		return
	}
	scope, clientID, iaid, err := s.parseCommon(req, relay)
	if err != nil {
		s.logger.Warn("solicit parse", "err", err)
		return
	}
	if scope == nil || !scope.Enabled {
		return
	}

	duidStr := ParseDUID(clientID)
	iaidStr := fmt.Sprintf("%d", iaid)

	// In multi-node deployments, optionally delay Solicit replies. Nodes with a
	// shorter delay will write their offered lease first; delayed nodes skip if
	// another node already offered.
	if s.cfg.Cluster.Enabled && s.cfg.Cluster.DiscoverReplyDelay > 0 {
		if existing, _ := s.store.GetV6LeaseByDUID(ctx, scope.ID, duidStr, iaidStr); existing != nil && (existing.State == models.LeaseActive || existing.State == models.LeaseOffered) {
			s.logger.Debug("solicit skipped, peer already offered", "duid", duidStr, "ip", existing.IPAddr)
			return
		}
		select {
		case <-time.After(s.cfg.Cluster.DiscoverReplyDelay):
		case <-ctx.Done():
			return
		}
		if existing, _ := s.store.GetV6LeaseByDUID(ctx, scope.ID, duidStr, iaidStr); existing != nil && (existing.State == models.LeaseActive || existing.State == models.LeaseOffered) {
			s.logger.Debug("solicit skipped after delay, peer already offered", "duid", duidStr, "ip", existing.IPAddr)
			return
		}
	}

	reservation, _ := s.store.GetV6ReservationByDUID(ctx, scope.ID, duidStr)
	groupOpts := s.groupOptionsForReservation(ctx, reservation)

	leaseTime := scope.LeaseTime
	if leaseTime == 0 {
		leaseTime = DefaultV6LeaseTime
	}
	maxLeaseTime := scope.MaxLeaseTime
	if maxLeaseTime == 0 {
		maxLeaseTime = DefaultV6MaxLeaseTime
	}

	lease, err := s.store.AllocateV6Lease(ctx, scope, duidStr, iaidStr, "", nil, leaseTime, maxLeaseTime)
	if err != nil {
		s.logger.Warn("allocate v6 lease failed", "duid", duidStr, "scope", scope.Name, "err", err)
		return
	}

	reply := ReplyFromRequest(req, MsgTypeAdvertise)
	reply.Options.Add(OptServerID, s.serverDUID)
	reply.Options.Add(OptClientID, clientID)

	// Rapid Commit: client wants immediate assignment (common on Android).
	if _, ok := req.Options.Get(OptRapidCommit); ok {
		if err := s.store.UpdateV6LeaseState(ctx, lease.ID, models.LeaseActive); err != nil {
			s.logger.Error("activate v6 lease failed", "duid", duidStr, "ip", lease.IPAddr, "err", err)
			return
		}
		reply.MessageType = MsgTypeReply
		reply.Options.Add(OptRapidCommit, []byte{})
		s.logger.Info("v6 rapid commit", "duid", duidStr, "ip", lease.IPAddr, "scope", scope.Name)
	}

	s.applyIA(reply, iaid, lease.IPAddr, scope)
	s.applyOptions(req, reply, scope, reservation, groupOpts)
	s.sendReply(reply, addr, relay)
	if reply.MessageType == MsgTypeReply {
		s.logger.Info("v6 rapid commit", "duid", duidStr, "ip", lease.IPAddr, "scope", scope.Name)
		s.logIPAllocation(ctx, scope, duidStr, iaidStr, lease.IPAddr, nil, "ack", relay, "")
	} else {
		s.logger.Info("v6 advertise/reply sent", "duid", duidStr, "ip", lease.IPAddr, "scope", scope.Name, "rapid_commit", reply.MessageType == MsgTypeReply)
		s.logIPAllocation(ctx, scope, duidStr, iaidStr, lease.IPAddr, nil, "offer", relay, "")
	}
}

func (s *Server) handleRequest(ctx context.Context, req *Packet, addr *net.UDPAddr, relay *relayContext) {
	// Rebind is addressed to any available server and carries no Server ID;
	// Request and Renew must be validated (RFC 8415 §16.4/§16.6).
	if req.MessageType != MsgTypeRebind && !s.serverIDMatches(req) {
		s.logger.Debug("ignoring v6 request for another server", "type", req.MessageType)
		return
	}
	if hasIANAOpt(req) && s.hasPD(req) {
		s.handleRequestCombined(ctx, req, addr, relay)
		return
	}
	if s.hasPD(req) {
		s.handleRequestPD(ctx, req, addr, relay)
		return
	}
	scope, clientID, iaid, err := s.parseCommon(req, relay)
	if err != nil {
		s.logger.Warn("request parse", "err", err)
		return
	}
	if scope == nil || !scope.Enabled {
		s.sendReplyWithStatus(req, addr, relay, 2, "no scope") // NoAddrsAvail
		return
	}

	duidStr := ParseDUID(clientID)
	iaidStr := fmt.Sprintf("%d", iaid)
	requestedIP := s.extractRequestedIP(req, iaid)
	reservation, _ := s.store.GetV6ReservationByDUID(ctx, scope.ID, duidStr)
	groupOpts := s.groupOptionsForReservation(ctx, reservation)

	if reservation != nil && requestedIP != nil && !requestedIP.Equal(reservation.IPAddr) {
		s.sendReplyWithStatus(req, addr, relay, 3, "binding mismatch") // NoBinding
		return
	}

	leaseTime := scope.LeaseTime
	if leaseTime == 0 {
		leaseTime = DefaultV6LeaseTime
	}
	maxLeaseTime := scope.MaxLeaseTime
	if maxLeaseTime == 0 {
		maxLeaseTime = DefaultV6MaxLeaseTime
	}

	lease, err := s.store.AllocateV6Lease(ctx, scope, duidStr, iaidStr, "", requestedIP, leaseTime, maxLeaseTime)
	if err != nil {
		s.logger.Warn("confirm v6 lease failed", "duid", duidStr, "requested_ip", requestedIP, "scope", scope.Name, "err", err)
		s.sendReplyWithStatus(req, addr, relay, 2, "address unavailable")
		return
	}

	if err := s.store.UpdateV6LeaseState(ctx, lease.ID, models.LeaseActive); err != nil {
		s.logger.Error("activate v6 lease failed", "duid", duidStr, "ip", lease.IPAddr, "err", err)
		s.sendReplyWithStatus(req, addr, relay, 2, "address unavailable")
		return
	}

	reply := ReplyFromRequest(req, MsgTypeReply)
	reply.Options.Add(OptServerID, s.serverDUID)
	reply.Options.Add(OptClientID, clientID)
	s.applyIA(reply, iaid, lease.IPAddr, scope)
	s.applyOptions(req, reply, scope, reservation, groupOpts)
	s.sendReply(reply, addr, relay)
	s.logger.Info("v6 lease ack", "duid", duidStr, "ip", lease.IPAddr, "scope", scope.Name)
	s.logIPAllocation(ctx, scope, duidStr, iaidStr, lease.IPAddr, nil, "ack", relay, "")
}

func (s *Server) extractRequestedIP(req *Packet, iaid uint32) net.IP {
	iana, ok := req.Options.Get(OptIANA)
	if !ok {
		iana, ok = req.Options.Get(OptIATA)
	}
	if !ok || len(iana.Data) < 12 {
		return nil
	}
	iaidGot := binary.BigEndian.Uint32(iana.Data[0:4])
	if iaidGot != iaid {
		return nil
	}
	// Parse nested options inside IA_NA
	data := iana.Data[12:]
	for len(data) >= 4 {
		code := binary.BigEndian.Uint16(data[0:2])
		length := binary.BigEndian.Uint16(data[2:4])
		if len(data) < int(4+length) {
			break
		}
		if code == OptIAAddr && length >= 16 {
			ip := net.IP(append([]byte(nil), data[4:20]...))
			return ip
		}
		data = data[4+length:]
	}
	return nil
}

func (s *Server) hasPD(req *Packet) bool {
	_, ok := req.Options.Get(OptIAPD)
	return ok
}

// hasIANA reports whether the message carries an IA_NA or IA_TA option.
func hasIANAOpt(req *Packet) bool {
	if _, ok := req.Options.Get(OptIANA); ok {
		return true
	}
	_, ok := req.Options.Get(OptIATA)
	return ok
}

func (s *Server) handleSolicitPD(ctx context.Context, req *Packet, addr *net.UDPAddr, relay *relayContext) {
	scope, clientID, iaid, err := s.parseCommonPD(req, relay)
	if err != nil {
		s.logger.Warn("solicit pd parse", "err", err)
		return
	}
	if scope == nil || !scope.Enabled || scope.Prefix == nil {
		return
	}

	duidStr := ParseDUID(clientID)
	iaidStr := fmt.Sprintf("%d", iaid)

	// In multi-node deployments, optionally delay PD Solicit replies.
	if s.cfg.Cluster.Enabled && s.cfg.Cluster.DiscoverReplyDelay > 0 {
		if existing, _ := s.store.GetV6PrefixByDUID(ctx, scope.ID, duidStr, iaidStr); existing != nil && (existing.State == models.LeaseActive || existing.State == models.LeaseOffered) {
			s.logger.Debug("solicit pd skipped, peer already offered", "duid", duidStr, "prefix", existing.Prefix.String())
			return
		}
		select {
		case <-time.After(s.cfg.Cluster.DiscoverReplyDelay):
		case <-ctx.Done():
			return
		}
		if existing, _ := s.store.GetV6PrefixByDUID(ctx, scope.ID, duidStr, iaidStr); existing != nil && (existing.State == models.LeaseActive || existing.State == models.LeaseOffered) {
			s.logger.Debug("solicit pd skipped after delay, peer already offered", "duid", duidStr, "prefix", existing.Prefix.String())
			return
		}
	}

	leaseTime := scope.LeaseTime
	if leaseTime == 0 {
		leaseTime = DefaultV6LeaseTime
	}
	maxLeaseTime := scope.MaxLeaseTime
	if maxLeaseTime == 0 {
		maxLeaseTime = DefaultV6MaxLeaseTime
	}

	p, err := s.store.AllocateV6Prefix(ctx, scope, duidStr, iaidStr, leaseTime, maxLeaseTime)
	if err != nil {
		s.logger.Warn("allocate v6 prefix failed", "err", err)
		return
	}

	reply := ReplyFromRequest(req, MsgTypeAdvertise)
	reply.Options.Add(OptServerID, s.serverDUID)
	reply.Options.Add(OptClientID, clientID)
	s.applyPD(reply, iaid, p.Prefix, leaseTime)
	s.applyOptions(req, reply, scope, nil, nil)
	s.sendReply(reply, addr, relay)
	s.logger.Info("v6 prefix advertised", "duid", duidStr, "prefix", p.Prefix.String(), "scope", scope.Name)
	s.logIPAllocation(ctx, scope, duidStr, iaidStr, nil, p.Prefix, "offer", relay, "")
}

func (s *Server) handleRequestPD(ctx context.Context, req *Packet, addr *net.UDPAddr, relay *relayContext) {
	scope, clientID, iaid, err := s.parseCommonPD(req, relay)
	if err != nil {
		s.logger.Warn("request pd parse", "err", err)
		return
	}
	if scope == nil || !scope.Enabled || scope.Prefix == nil {
		s.sendReplyWithStatus(req, addr, relay, 6, "no prefixes") // NoPrefixAvail
		return
	}

	duidStr := ParseDUID(clientID)
	iaidStr := fmt.Sprintf("%d", iaid)

	leaseTime := scope.LeaseTime
	if leaseTime == 0 {
		leaseTime = DefaultV6LeaseTime
	}
	maxLeaseTime := scope.MaxLeaseTime
	if maxLeaseTime == 0 {
		maxLeaseTime = DefaultV6MaxLeaseTime
	}

	p, err := s.store.AllocateV6Prefix(ctx, scope, duidStr, iaidStr, leaseTime, maxLeaseTime)
	if err != nil {
		s.logger.Warn("confirm v6 prefix failed", "duid", duidStr, "scope", scope.Name, "err", err)
		s.sendReplyWithStatus(req, addr, relay, 6, "no prefixes")
		return
	}

	if err := s.store.UpdateV6PrefixState(ctx, p.ID, models.LeaseActive); err != nil {
		s.logger.Error("activate v6 prefix failed", "duid", duidStr, "prefix", p.Prefix.String(), "err", err)
		s.sendReplyWithStatus(req, addr, relay, 6, "no prefixes")
		return
	}

	reply := ReplyFromRequest(req, MsgTypeReply)
	reply.Options.Add(OptServerID, s.serverDUID)
	reply.Options.Add(OptClientID, clientID)
	s.applyPD(reply, iaid, p.Prefix, leaseTime)
	s.applyOptions(req, reply, scope, nil, nil)
	s.sendReply(reply, addr, relay)
	s.logger.Info("v6 prefix delegated", "duid", duidStr, "prefix", p.Prefix.String(), "scope", scope.Name)
	s.logIPAllocation(ctx, scope, duidStr, iaidStr, nil, p.Prefix, "ack", relay, "")
}

// handleSolicitCombined answers a Solicit that carries both IA_NA and IA_PD
// in a single Advertise/Reply (RFC 8415 §18.3.1).
func (s *Server) handleSolicitCombined(ctx context.Context, req *Packet, addr *net.UDPAddr, relay *relayContext) {
	scope, clientID, iaid, err := s.parseCommon(req, relay)
	if err != nil {
		s.logger.Warn("solicit combined parse", "err", err)
		return
	}
	if scope != nil && !scope.Enabled {
		scope = nil
	}
	pdScope, _, pdIAID, pdErr := s.parseCommonPD(req, relay)
	if pdErr != nil || pdScope == nil || !pdScope.Enabled || pdScope.Prefix == nil {
		pdScope = nil
	}
	if scope == nil && pdScope == nil {
		return
	}

	duidStr := ParseDUID(clientID)
	iaidStr := fmt.Sprintf("%d", iaid)
	pdIAIDStr := fmt.Sprintf("%d", pdIAID)

	var lease *models.V6Lease
	if scope != nil {
		leaseTime := normalizedV6LeaseTimes(scope)
		l, err := s.store.AllocateV6Lease(ctx, scope, duidStr, iaidStr, "", nil, leaseTime, scopeMaxLeaseTime(scope))
		if err != nil {
			s.logger.Warn("allocate v6 lease failed", "duid", duidStr, "scope", scope.Name, "err", err)
		} else {
			lease = l
		}
	}

	var prefix *models.V6Prefix
	if pdScope != nil {
		p, err := s.store.AllocateV6Prefix(ctx, pdScope, duidStr, pdIAIDStr, normalizedV6LeaseTimes(pdScope), scopeMaxLeaseTime(pdScope))
		if err != nil {
			s.logger.Warn("allocate v6 prefix failed", "duid", duidStr, "scope", pdScope.Name, "err", err)
		} else {
			prefix = p
		}
	}

	if lease == nil && prefix == nil {
		return
	}

	reply := ReplyFromRequest(req, MsgTypeAdvertise)
	reply.Options.Add(OptServerID, s.serverDUID)
	reply.Options.Add(OptClientID, clientID)

	rapidCommit := false
	if _, ok := req.Options.Get(OptRapidCommit); ok {
		if lease != nil {
			if err := s.store.UpdateV6LeaseState(ctx, lease.ID, models.LeaseActive); err != nil {
				s.logger.Error("activate v6 lease failed", "duid", duidStr, "ip", lease.IPAddr, "err", err)
				lease = nil
			}
		}
		if prefix != nil {
			if err := s.store.UpdateV6PrefixState(ctx, prefix.ID, models.LeaseActive); err != nil {
				s.logger.Error("activate v6 prefix failed", "duid", duidStr, "prefix", prefix.Prefix.String(), "err", err)
				prefix = nil
			}
		}
		if lease == nil && prefix == nil {
			return
		}
		rapidCommit = true
		reply.MessageType = MsgTypeReply
		reply.Options.Add(OptRapidCommit, []byte{})
	}

	if lease != nil {
		s.applyIA(reply, iaid, lease.IPAddr, scope)
	}
	if prefix != nil {
		s.applyPD(reply, pdIAID, prefix.Prefix, normalizedV6LeaseTimes(pdScope))
	}
	optScope := scope
	if optScope == nil {
		optScope = pdScope
	}
	s.applyOptions(req, reply, optScope, nil, nil)
	s.sendReply(reply, addr, relay)

	action := "offer"
	if rapidCommit {
		action = "ack"
	}
	if lease != nil {
		s.logger.Info("v6 lease advertised (combined)", "duid", duidStr, "ip", lease.IPAddr, "scope", scope.Name)
		s.logIPAllocation(ctx, scope, duidStr, iaidStr, lease.IPAddr, nil, action, relay, "")
	}
	if prefix != nil {
		s.logger.Info("v6 prefix advertised (combined)", "duid", duidStr, "prefix", prefix.Prefix.String(), "scope", pdScope.Name)
		s.logIPAllocation(ctx, pdScope, duidStr, pdIAIDStr, nil, prefix.Prefix, action, relay, "")
	}
}

// handleRequestCombined answers a Request/Renew/Rebind that carries both
// IA_NA and IA_PD in a single Reply (RFC 8415 §18.3.2).
func (s *Server) handleRequestCombined(ctx context.Context, req *Packet, addr *net.UDPAddr, relay *relayContext) {
	scope, clientID, iaid, err := s.parseCommon(req, relay)
	if err != nil {
		s.logger.Warn("request combined parse", "err", err)
		return
	}
	if scope != nil && !scope.Enabled {
		scope = nil
	}
	pdScope, _, pdIAID, pdErr := s.parseCommonPD(req, relay)
	if pdErr != nil || pdScope == nil || !pdScope.Enabled || pdScope.Prefix == nil {
		pdScope = nil
	}
	if scope == nil && pdScope == nil {
		s.sendReplyWithStatus(req, addr, relay, 2, "no scope")
		return
	}

	duidStr := ParseDUID(clientID)
	iaidStr := fmt.Sprintf("%d", iaid)
	pdIAIDStr := fmt.Sprintf("%d", pdIAID)

	var lease *models.V6Lease
	if scope != nil {
		requestedIP := s.extractRequestedIP(req, iaid)
		reservation, _ := s.store.GetV6ReservationByDUID(ctx, scope.ID, duidStr)
		l, err := s.store.AllocateV6Lease(ctx, scope, duidStr, iaidStr, "", requestedIP, normalizedV6LeaseTimes(scope), scopeMaxLeaseTime(scope))
		if err != nil {
			s.logger.Warn("confirm v6 lease failed", "duid", duidStr, "requested_ip", requestedIP, "scope", scope.Name, "err", err)
		} else if err := s.store.UpdateV6LeaseState(ctx, l.ID, models.LeaseActive); err != nil {
			s.logger.Error("activate v6 lease failed", "duid", duidStr, "ip", l.IPAddr, "err", err)
		} else {
			lease = l
		}
		_ = reservation
	}

	var prefix *models.V6Prefix
	if pdScope != nil {
		p, err := s.store.AllocateV6Prefix(ctx, pdScope, duidStr, pdIAIDStr, normalizedV6LeaseTimes(pdScope), scopeMaxLeaseTime(pdScope))
		if err != nil {
			s.logger.Warn("confirm v6 prefix failed", "duid", duidStr, "scope", pdScope.Name, "err", err)
		} else if err := s.store.UpdateV6PrefixState(ctx, p.ID, models.LeaseActive); err != nil {
			s.logger.Error("activate v6 prefix failed", "duid", duidStr, "prefix", p.Prefix.String(), "err", err)
		} else {
			prefix = p
		}
	}

	if lease == nil && prefix == nil {
		s.sendReplyWithStatus(req, addr, relay, 2, "address unavailable")
		return
	}

	reply := ReplyFromRequest(req, MsgTypeReply)
	reply.Options.Add(OptServerID, s.serverDUID)
	reply.Options.Add(OptClientID, clientID)
	if lease != nil {
		s.applyIA(reply, iaid, lease.IPAddr, scope)
	}
	if prefix != nil {
		s.applyPD(reply, pdIAID, prefix.Prefix, normalizedV6LeaseTimes(pdScope))
	}
	optScope := scope
	if optScope == nil {
		optScope = pdScope
	}
	s.applyOptions(req, reply, optScope, nil, nil)
	s.sendReply(reply, addr, relay)

	if lease != nil {
		s.logger.Info("v6 lease ack (combined)", "duid", duidStr, "ip", lease.IPAddr, "scope", scope.Name)
		s.logIPAllocation(ctx, scope, duidStr, iaidStr, lease.IPAddr, nil, "ack", relay, "")
	}
	if prefix != nil {
		s.logger.Info("v6 prefix delegated (combined)", "duid", duidStr, "prefix", prefix.Prefix.String(), "scope", pdScope.Name)
		s.logIPAllocation(ctx, pdScope, duidStr, pdIAIDStr, nil, prefix.Prefix, "ack", relay, "")
	}
}

func normalizedV6LeaseTimes(scope *models.Scope) int {
	if scope.LeaseTime == 0 {
		return DefaultV6LeaseTime
	}
	return scope.LeaseTime
}

func scopeMaxLeaseTime(scope *models.Scope) int {
	if scope.MaxLeaseTime == 0 {
		return DefaultV6MaxLeaseTime
	}
	return scope.MaxLeaseTime
}

func (s *Server) parseCommonPD(req *Packet, relay *relayContext) (*models.Scope, []byte, uint32, error) {
	clientIDOpt, ok := req.Options.Get(OptClientID)
	if !ok {
		return nil, nil, 0, fmt.Errorf("missing client id")
	}
	clientID := clientIDOpt.Data

	iaOpt, ok := req.Options.Get(OptIAPD)
	if !ok || len(iaOpt.Data) < 4 {
		return nil, nil, 0, fmt.Errorf("missing iapd")
	}
	iaid := binary.BigEndian.Uint32(iaOpt.Data[0:4])

	scope, err := s.matchScopePD(req, relay)
	if err != nil {
		return nil, nil, 0, err
	}
	return scope, clientID, iaid, nil
}

func (s *Server) applyPD(reply *Packet, iaid uint32, prefix *net.IPNet, leaseTime int) {
	if leaseTime == 0 {
		leaseTime = DefaultV6LeaseTime
	}
	t1 := uint32(leaseTime / 2)
	t2 := uint32(leaseTime * 7 / 8)
	prefixLen, _ := prefix.Mask.Size()
	prefixOpt := Option{Code: OptIAPrefix, Data: BuildIAPrefix(byte(prefixLen), prefix.IP, uint32(leaseTime), uint32(leaseTime))}
	reply.Options.Add(OptIAPD, BuildIAPD(iaid, t1, t2, []Option{prefixOpt}))
}

func (s *Server) handleRelease(ctx context.Context, req *Packet, addr *net.UDPAddr, relay *relayContext) {
	if !s.serverIDMatches(req) {
		s.logger.Debug("ignoring v6 release for another server")
		return
	}
	// A Release may carry both IA_NA and IA_PD; process every IA and answer
	// with a single Reply (RFC 8415 §18.3.7).
	var clientID []byte
	if hasIANAOpt(req) {
		clientID = s.releaseV6Lease(ctx, req, relay)
	}
	if s.hasPD(req) {
		if id := s.releaseV6Prefix(ctx, req, relay); clientID == nil {
			clientID = id
		}
	}
	if clientID == nil {
		if opt, ok := req.Options.Get(OptClientID); ok {
			clientID = opt.Data
		} else {
			return
		}
	}
	reply := ReplyFromRequest(req, MsgTypeReply)
	reply.Options.Add(OptServerID, s.serverDUID)
	reply.Options.Add(OptClientID, clientID)
	reply.Options.Add(OptStatusCode, BuildStatusCode(0, "release received"))
	s.sendReply(reply, addr, relay)
}

// releaseV6Lease releases the IA_NA binding carried by the message, if any.
// It returns the client ID when the message parsed successfully.
func (s *Server) releaseV6Lease(ctx context.Context, req *Packet, relay *relayContext) []byte {
	scope, clientID, iaid, err := s.parseCommon(req, relay)
	if err != nil || scope == nil {
		return nil
	}
	duidStr := ParseDUID(clientID)
	lease, _ := s.store.GetV6LeaseByDUID(ctx, scope.ID, duidStr, fmt.Sprintf("%d", iaid))
	if lease != nil {
		_ = s.store.UpdateV6LeaseState(ctx, lease.ID, models.LeaseReleased)
		metrics.LeasesReleased.Inc()
		s.logger.Info("v6 lease released", "duid", duidStr, "ip", lease.IPAddr)
		s.logIPAllocation(ctx, scope, duidStr, fmt.Sprintf("%d", iaid), lease.IPAddr, nil, "release", relay, "")
	}
	return clientID
}

// releaseV6Prefix releases the IA_PD binding carried by the message, if any.
func (s *Server) releaseV6Prefix(ctx context.Context, req *Packet, relay *relayContext) []byte {
	scope, clientID, iaid, err := s.parseCommonPD(req, relay)
	if err != nil || scope == nil {
		return nil
	}
	duidStr := ParseDUID(clientID)
	iaidStr := fmt.Sprintf("%d", iaid)
	p, _ := s.store.GetV6PrefixByDUID(ctx, scope.ID, duidStr, iaidStr)
	if p != nil {
		_ = s.store.UpdateV6PrefixState(ctx, p.ID, models.LeaseReleased)
		metrics.LeasesReleased.Inc()
		s.logger.Info("v6 prefix released", "duid", duidStr, "prefix", p.Prefix.String())
		s.logIPAllocation(ctx, scope, duidStr, iaidStr, nil, p.Prefix, "release", relay, "")
	}
	return clientID
}

// handleConfirm implements RFC 8415 §18.3.3: the client asks whether its
// addresses are still appropriate for the link; we answer Success when every
// address in the message belongs to the matched scope, NotOnLink otherwise.
func (s *Server) handleConfirm(ctx context.Context, req *Packet, addr *net.UDPAddr, relay *relayContext) {
	scope, clientID, _, err := s.parseCommon(req, relay)
	if err != nil {
		return
	}
	status := uint16(0) // Success
	msg := "addresses are on-link"
	if scope == nil || scope.Subnet == nil {
		status = 4 // NotOnLink
		msg = "no matching link"
	} else {
		for _, ip := range extractIAAddrs(req) {
			if !scope.Subnet.Contains(ip) {
				status = 4
				msg = "address not on-link"
				break
			}
		}
	}
	reply := ReplyFromRequest(req, MsgTypeReply)
	reply.Options.Add(OptServerID, s.serverDUID)
	reply.Options.Add(OptClientID, clientID)
	reply.Options.Add(OptStatusCode, BuildStatusCode(status, msg))
	s.sendReply(reply, addr, relay)
}

// extractIAAddrs returns every IA Address carried by the IA_NA/IA_TA options
// of the message (Confirm may carry several).
func extractIAAddrs(req *Packet) []net.IP {
	var out []net.IP
	for _, opt := range req.Options {
		if opt.Code != OptIANA && opt.Code != OptIATA {
			continue
		}
		data := opt.Data
		if len(data) < 12 {
			continue
		}
		data = data[12:] // skip iaid/t1/t2 header
		for len(data) >= 4 {
			code := binary.BigEndian.Uint16(data[0:2])
			length := int(binary.BigEndian.Uint16(data[2:4]))
			if len(data) < 4+length {
				break
			}
			if code == OptIAAddr && length >= 16 {
				out = append(out, net.IP(append([]byte(nil), data[4:20]...)))
			}
			data = data[4+length:]
		}
	}
	return out
}

func (s *Server) handleDecline(ctx context.Context, req *Packet, addr *net.UDPAddr, relay *relayContext) {
	if !s.serverIDMatches(req) {
		s.logger.Debug("ignoring v6 decline for another server")
		return
	}
	scope, clientID, iaid, err := s.parseCommon(req, relay)
	if err != nil {
		return
	}
	duidStr := ParseDUID(clientID)
	lease, _ := s.store.GetV6LeaseByDUID(ctx, scope.ID, duidStr, fmt.Sprintf("%d", iaid))
	if lease != nil {
		_ = s.store.UpdateV6LeaseState(ctx, lease.ID, models.LeaseDeclined)
		metrics.LeasesDeclined.Inc()
		s.logger.Warn("v6 lease declined", "duid", duidStr, "ip", lease.IPAddr)
		s.logIPAllocation(ctx, scope, duidStr, fmt.Sprintf("%d", iaid), lease.IPAddr, nil, "decline", relay, "")
	}
	// RFC 8415 §18.3.8: a Decline is always answered with a Reply.
	reply := ReplyFromRequest(req, MsgTypeReply)
	reply.Options.Add(OptServerID, s.serverDUID)
	reply.Options.Add(OptClientID, clientID)
	reply.Options.Add(OptStatusCode, BuildStatusCode(0, "decline received"))
	s.sendReply(reply, addr, relay)
}

func (s *Server) handleInformationRequest(ctx context.Context, req *Packet, addr *net.UDPAddr, relay *relayContext) {
	if !s.serverIDMatches(req) {
		s.logger.Debug("ignoring v6 information-request for another server")
		return
	}
	scope, clientID, _, err := s.parseCommon(req, relay)
	if err != nil || scope == nil {
		return
	}
	duidStr := ParseDUID(clientID)
	reservation, _ := s.store.GetV6ReservationByDUID(ctx, scope.ID, duidStr)
	groupOpts := s.groupOptionsForReservation(ctx, reservation)

	reply := ReplyFromRequest(req, MsgTypeReply)
	reply.Options.Add(OptServerID, s.serverDUID)
	reply.Options.Add(OptClientID, clientID)
	s.applyOptions(req, reply, scope, reservation, groupOpts)
	s.sendReply(reply, addr, relay)
}

func (s *Server) parseCommon(req *Packet, relay *relayContext) (*models.Scope, []byte, uint32, error) {
	clientIDOpt, ok := req.Options.Get(OptClientID)
	if !ok {
		return nil, nil, 0, fmt.Errorf("missing client id")
	}
	clientID := clientIDOpt.Data

	iaOpt, ok := req.Options.Get(OptIANA)
	if !ok {
		// Try IATA
		iaOpt, ok = req.Options.Get(OptIATA)
	}
	var iaid uint32
	if ok && len(iaOpt.Data) >= 4 {
		iaid = binary.BigEndian.Uint32(iaOpt.Data[0:4])
	} else if req.MessageType != MsgTypeInformationRequest {
		return nil, nil, 0, fmt.Errorf("missing ia")
	}

	scope, err := s.matchScope(req, relay)
	if err != nil {
		return nil, nil, 0, err
	}
	return scope, clientID, iaid, nil
}

func (s *Server) matchScope(req *Packet, relay *relayContext) (*models.Scope, error) {
	return s.matchScopeWithNeed(req, false, relay)
}

func (s *Server) matchScopePD(req *Packet, relay *relayContext) (*models.Scope, error) {
	return s.matchScopeWithNeed(req, true, relay)
}

func (s *Server) matchScopeWithNeed(req *Packet, needPrefix bool, relay *relayContext) (*models.Scope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.scopes) == 0 {
		return nil, fmt.Errorf("no v6 scopes")
	}

	// Determine the source address used to select a scope.
	// For relayed requests, use link-address; for direct clients, fall back to peer address.
	var sourceIP net.IP
	if relay != nil && relay.linkAddr != nil && !relay.linkAddr.IsUnspecified() {
		sourceIP = relay.linkAddr
	} else if relay != nil && relay.peerAddr != nil && !relay.peerAddr.IsUnspecified() {
		sourceIP = relay.peerAddr
	}

	// First pass: try to match by source address against scope prefix/subnet.
	if sourceIP != nil {
		for _, sc := range s.scopes {
			if !sc.Enabled {
				continue
			}
			if needPrefix {
				if sc.Prefix != nil && sc.Prefix.Contains(sourceIP) {
					return sc, nil
				}
			} else {
				if sc.Subnet != nil && sc.Subnet.Contains(sourceIP) {
					return sc, nil
				}
			}
		}
	}

	// Fallback: return the first enabled scope that satisfies the prefix/subnet requirement.
	for _, sc := range s.scopes {
		if !sc.Enabled {
			continue
		}
		if needPrefix && sc.Prefix == nil {
			continue
		}
		if !needPrefix && sc.Subnet == nil {
			continue
		}
		return sc, nil
	}
	return nil, fmt.Errorf("no matching v6 scope")
}

func (s *Server) applyIA(reply *Packet, iaid uint32, ip net.IP, scope *models.Scope) {
	leaseTime := scope.LeaseTime
	if leaseTime == 0 {
		leaseTime = DefaultV6LeaseTime
	}
	t1 := uint32(leaseTime / 2)
	t2 := uint32(leaseTime * 7 / 8)
	addrOpt := Option{Code: OptIAAddr, Data: BuildIAAddress(ip, uint32(leaseTime), uint32(leaseTime))}
	reply.Options.Add(OptIANA, BuildIANA(iaid, t1, t2, []Option{addrOpt}))
}

func (s *Server) groupOptionsForReservation(ctx context.Context, r *models.V6Reservation) json.RawMessage {
	if r == nil || r.GroupID == "" {
		return nil
	}
	g, err := s.store.GetReservationGroupByID(ctx, r.GroupID)
	if err != nil {
		return nil
	}
	return g.Options
}

func (s *Server) applyOptions(req *Packet, reply *Packet, scope *models.Scope, reservation *models.V6Reservation, groupOpts json.RawMessage) {
	if len(scope.DNS) > 0 {
		reply.Options.Add(OptDNSServers, BuildDNSList(scope.DNS))
	}
	if scope.DomainName != "" {
		reply.Options.Add(OptDomainList, BuildDomainList([]string{scope.DomainName}))
	}

	// Merge options: group -> scope custom -> reservation.
	// Later layers override earlier layers.
	for k, v := range parseOptionMap(groupOpts) {
		code := v6OptionCodeFromKey(k)
		if code == 0 {
			continue
		}
		raw, err := buildV6Option(code, v)
		if err == nil {
			reply.Options.Add(code, raw)
		}
	}

	scopeOpts := parseOptionMap(scope.Options)
	for k, v := range scopeOpts {
		code := v6OptionCodeFromKey(k)
		if code == 0 {
			continue
		}
		raw, err := buildV6Option(code, v)
		if err == nil {
			reply.Options.Add(code, raw)
		}
	}

	if reservation != nil {
		for k, v := range parseOptionMap(reservation.Options) {
			code := v6OptionCodeFromKey(k)
			if code == 0 {
				continue
			}
			raw, err := buildV6Option(code, v)
			if err == nil {
				reply.Options.Add(code, raw)
			}
		}
	}

	// Respect Option Request
	if oro, ok := req.Options.Get(OptOptionRequest); ok {
		// Client requested specific options; we already included common ones
		_ = oro
	}
}

func (s *Server) sendReplyWithStatus(req *Packet, addr *net.UDPAddr, relay *relayContext, code uint16, msg string) {
	reply := ReplyFromRequest(req, MsgTypeReply)
	reply.Options.Add(OptServerID, s.serverDUID)
	reply.Options.Add(OptStatusCode, BuildStatusCode(code, msg))
	s.sendReply(reply, addr, relay)
}

func (s *Server) sendReply(reply *Packet, addr *net.UDPAddr, relay *relayContext) {
	data, err := reply.Marshal()
	if err != nil {
		s.logger.Error("marshal v6 reply", "err", err)
		return
	}

	dest := &net.UDPAddr{IP: addr.IP, Port: 546, Zone: addr.Zone}
	if relay != nil {
		// Relayed reply: re-encapsulate mirroring the incoming relay chain
		// (innermost layer first), so each relay on the path can strip its
		// own layer (RFC 8415 §19.3).
		hops := relay.hops
		if len(hops) == 0 {
			hops = []relayHop{{
				hopCount: relay.hopCount,
				linkAddr: relay.linkAddr,
				peerAddr: relay.peerAddr,
				options:  relay.relayOptions,
			}}
		}
		for i := len(hops) - 1; i >= 0; i-- {
			h := hops[i]
			rp := &RelayPacket{
				MessageType: MsgTypeRelayRepl,
				HopCount:    h.hopCount,
				LinkAddr:    h.linkAddr,
				PeerAddr:    h.peerAddr,
				Options:     h.options,
			}
			rp.Options.Add(OptRelayMsg, data)
			data, err = rp.Marshal()
			if err != nil {
				s.logger.Error("marshal v6 relay reply", "err", err)
				return
			}
		}
		dest = &net.UDPAddr{IP: relay.relayAddr.IP, Port: 547, Zone: relay.relayAddr.Zone}
	}

	if _, err := s.conn.WriteToUDP(data, dest); err != nil {
		s.logger.Error("send v6 reply", "err", err, "dest", dest)
	} else {
		s.logger.Info("v6 reply sent", "dest", dest)
	}
}

func (s *Server) generateDUID() []byte {
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if len(iface.HardwareAddr) == 6 {
				return DUIDLL(iface.HardwareAddr)
			}
		}
	}
	return DUIDLL(net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55})
}

// loadOrCreateDUID returns the stable server DUID, generating and persisting
// it on first use. A stable DUID is required for Server-ID validation:
// clients remember it across Solicit/Request and Renew/Release exchanges.
func (s *Server) loadOrCreateDUID(ctx context.Context) []byte {
	if v, err := s.store.GetServerState(ctx, "server_duid"); err == nil && v != "" {
		if raw, err := hex.DecodeString(v); err == nil && len(raw) >= 4 {
			return raw
		}
	}
	duid := s.generateDUID()
	if err := s.store.SetServerState(ctx, "server_duid", hex.EncodeToString(duid)); err != nil {
		s.logger.Warn("persist server duid failed", "err", err)
	}
	return duid
}

func ipInRange(ip, start, end net.IP) bool {
	ipU := ipToUint128(ip.To16())
	startU := ipToUint128(start.To16())
	endU := ipToUint128(end.To16())
	return ipU.Cmp(startU) >= 0 && ipU.Cmp(endU) <= 0
}

func ipToUint128(ip net.IP) *bigInt128 {
	ip = ip.To16()
	return &bigInt128{hi: binary.BigEndian.Uint64(ip[0:8]), lo: binary.BigEndian.Uint64(ip[8:16])}
}

func uint128ToIP(v *bigInt128) net.IP {
	ip := make([]byte, 16)
	binary.BigEndian.PutUint64(ip[0:8], v.hi)
	binary.BigEndian.PutUint64(ip[8:16], v.lo)
	return net.IP(ip)
}

type bigInt128 struct {
	hi, lo uint64
}

func (b *bigInt128) Cmp(other *bigInt128) int {
	if b.hi < other.hi {
		return -1
	}
	if b.hi > other.hi {
		return 1
	}
	if b.lo < other.lo {
		return -1
	}
	if b.lo > other.lo {
		return 1
	}
	return 0
}

func ipAddOne(v *bigInt128) *bigInt128 {
	out := &bigInt128{hi: v.hi, lo: v.lo}
	if out.lo == ^uint64(0) {
		out.lo = 0
		out.hi++
	} else {
		out.lo++
	}
	return out
}

func parseOptionMap(b json.RawMessage) map[string]interface{} {
	var m map[string]interface{}
	if len(b) == 0 {
		return map[string]interface{}{}
	}
	_ = json.Unmarshal(b, &m)
	if m == nil {
		return map[string]interface{}{}
	}
	return m
}

func v6OptionCodeFromKey(k string) uint16 {
	// supports "23" or name aliases
	var code int
	if _, err := fmt.Sscanf(k, "%d", &code); err == nil && code > 0 && code < 65536 {
		return uint16(code)
	}
	switch k {
	case "dns", "dns-servers":
		return OptDNSServers
	case "domain", "domain-list":
		return OptDomainList
	case "sntp":
		return OptSNTPServers
	}
	return 0
}

func buildV6Option(code uint16, v interface{}) ([]byte, error) {
	b, _ := json.Marshal(v)
	var ov struct {
		Type  string      `json:"type"`
		Value interface{} `json:"value"`
	}
	_ = json.Unmarshal(b, &ov)
	switch ov.Type {
	case "ips":
		ips, err := parseIPList(ov.Value)
		if err != nil {
			return nil, err
		}
		return BuildDNSList(ips), nil
	case "domains":
		list, err := parseStringList(ov.Value)
		if err != nil {
			return nil, err
		}
		return BuildDomainList(list), nil
	case "uint32":
		n, err := toUint32(ov.Value)
		if err != nil {
			return nil, err
		}
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, n)
		return b, nil
	case "string":
		s, err := toString(ov.Value)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	case "hex":
		return parseHex(ov.Value)
	default:
		return nil, fmt.Errorf("unsupported v6 option type %s", ov.Type)
	}
}

func parseIPList(v interface{}) ([]net.IP, error) {
	data, _ := json.Marshal(v)
	var list []string
	_ = json.Unmarshal(data, &list)
	var ips []net.IP
	for _, s := range list {
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP: %s", s)
		}
		ips = append(ips, ip)
	}
	return ips, nil
}

func parseStringList(v interface{}) ([]string, error) {
	data, _ := json.Marshal(v)
	var list []string
	_ = json.Unmarshal(data, &list)
	return list, nil
}

func toUint32(v interface{}) (uint32, error) {
	switch val := v.(type) {
	case float64:
		return uint32(val), nil
	case int:
		return uint32(val), nil
	case string:
		var n uint32
		_, err := fmt.Sscanf(val, "%d", &n)
		return n, err
	default:
		return 0, fmt.Errorf("cannot convert to uint32")
	}
}

func toString(v interface{}) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	default:
		return fmt.Sprintf("%v", val), nil
	}
}

func parseHex(v interface{}) ([]byte, error) {
	s, err := toString(v)
	if err != nil {
		return nil, err
	}
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("invalid hex")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		var b byte
		fmt.Sscanf(s[i:i+2], "%2x", &b)
		out[i/2] = b
	}
	return out, nil
}
