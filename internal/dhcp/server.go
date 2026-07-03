package dhcp

import (
	"context"
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
	"github.com/google/gopacket"
	"github.com/google/uuid"
	"github.com/google/gopacket/layers"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

const (
	DefaultLeaseTime    = 3600
	DefaultMaxLeaseTime = 86400
)

// normalizeIP converts an IPv4-mapped IPv6 address (::ffff:x.x.x.x) to a
// 4-byte IPv4 address. Other addresses are returned unchanged.
func normalizeIP(ip net.IP) net.IP {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

// sanitizeUTF8 removes invalid UTF-8 byte sequences and NUL bytes so that
// client-provided strings (hostname, client identifier) can safely be stored
// in PostgreSQL TEXT/VARCHAR columns.
func sanitizeUTF8(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimSpace(s)
}

// clientIDString returns a safe, printable representation of the DHCPv4
// Client Identifier (option 61). Since this option is usually binary data
// (type byte + hardware address), we always encode it as lowercase hex to
// avoid storing / displaying garbage characters in the UI.
func clientIDString(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", data)
}

type Server struct {
	cfg       *config.Config
	store     *store.Store
	logger    *slog.Logger
	conn      *net.UDPConn
	pktConn   *ipv4.PacketConn
	rawFd     int
	mu        sync.RWMutex
	scopes    []*models.Scope
	allocMu   map[string]*sync.Mutex
	allocLock sync.Mutex
	// pendingDiscover buffers local-broadcast Discovers for a short window so
	// that if a relayed copy of the same Discover arrives we can prefer the
	// relayed copy and avoid answering the same request twice.
	pendingMu sync.Mutex
	pending   map[string]*pendingDiscover
	quit      chan struct{}
	stopOnce  sync.Once
	wg        sync.WaitGroup
}

type pendingDiscover struct {
	req   *Packet
	addr  *net.UDPAddr
	ctx   context.Context
	timer *time.Timer
}

func NewServer(cfg *config.Config, s *store.Store, logger *slog.Logger) *Server {
	return &Server{
		cfg:     cfg,
		store:   s,
		logger:  logger,
		allocMu: make(map[string]*sync.Mutex),
		pending: make(map[string]*pendingDiscover),
		quit:    make(chan struct{}),
	}
}

func htons(v uint16) uint16 {
	return (v<<8)&0xff00 | (v>>8)&0x00ff
}

const discoverRelayWait = 60 * time.Millisecond

func (s *Server) schedulePendingDiscover(ctx context.Context, req *Packet, addr *net.UDPAddr, mac string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if existing, ok := s.pending[mac]; ok {
		// Already buffering a direct Discover for this client; keep the newer one.
		existing.timer.Stop()
		existing.req = req
		existing.addr = addr
		existing.ctx = ctx
		existing.timer = time.AfterFunc(discoverRelayWait, func() { s.firePendingDiscover(mac) })
		return
	}
	pd := &pendingDiscover{
		req:  req,
		addr: addr,
		ctx:  ctx,
	}
	pd.timer = time.AfterFunc(discoverRelayWait, func() { s.firePendingDiscover(mac) })
	s.pending[mac] = pd
}

func (s *Server) cancelPendingDiscover(mac string) bool {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	existing, ok := s.pending[mac]
	if ok {
		existing.timer.Stop()
		delete(s.pending, mac)
	}
	return ok
}

func (s *Server) firePendingDiscover(mac string) {
	s.pendingMu.Lock()
	pd, ok := s.pending[mac]
	if ok {
		delete(s.pending, mac)
	}
	s.pendingMu.Unlock()
	if !ok {
		return
	}
	s.processDiscover(pd.ctx, pd.req, pd.addr)
}

func (s *Server) scopeAllocMu(scopeID string) *sync.Mutex {
	s.allocLock.Lock()
	defer s.allocLock.Unlock()
	mu, ok := s.allocMu[scopeID]
	if !ok {
		mu = &sync.Mutex{}
		s.allocMu[scopeID] = mu
	}
	return mu
}

func (s *Server) Start(ctx context.Context) error {
	if err := s.reloadScopes(ctx); err != nil {
		return err
	}
	go s.refreshLoop(ctx)

	addr, err := net.ResolveUDPAddr("udp4", s.cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("resolve udp: %w", err)
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}
	s.conn = conn
	s.pktConn = ipv4.NewPacketConn(conn)
	if err := s.pktConn.SetControlMessage(ipv4.FlagInterface, true); err != nil {
		s.logger.Warn("failed to enable IP_PKTINFO", "err", err)
	}
	if fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(unix.ETH_P_IP))); err != nil {
		s.logger.Warn("failed to open raw socket for unicast replies", "err", err)
	} else {
		s.rawFd = fd
	}
	s.logger.Info("dhcp server listening", "addr", s.cfg.Server.Listen)

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
		if s.rawFd != 0 {
			_ = unix.Close(s.rawFd)
		}
	})
	s.wg.Wait()
	return nil
}

func (s *Server) reloadScopes(ctx context.Context) error {
	scopes, err := s.store.ListScopes(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.scopes = scopes
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
			if err := s.store.ReleaseExpiredLeases(ctx, time.Now().UTC()); err != nil {
				s.logger.Error("release expired leases", "err", err)
			}
			if err := s.reloadScopes(ctx); err != nil {
				s.logger.Error("reload scopes", "err", err)
			}
			if leases, err := s.store.ListActiveLeases(ctx); err == nil {
				metrics.LeasesActive.WithLabelValues("v4").Set(float64(len(leases)))
			}
		}
	}
}

func (s *Server) serveLoop() {
	defer s.wg.Done()
	buf := make([]byte, 2048)
	for {
		n, cm, src, err := s.pktConn.ReadFrom(buf)
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				s.logger.Error("read udp", "err", err)
				continue
			}
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		addr, ok := src.(*net.UDPAddr)
		if !ok {
			s.logger.Warn("non-udp source address", "src", src)
			continue
		}
		ifIndex := 0
		if cm != nil {
			ifIndex = cm.IfIndex
		}
		go s.handlePacket(data, addr, ifIndex)
	}
}

func (s *Server) handlePacket(data []byte, addr *net.UDPAddr, ifIndex int) {
	req, err := ParsePacket(data)
	if err != nil {
		s.logger.Warn("parse packet", "err", err, "from", addr)
		metrics.DHCPPacketsTotal.WithLabelValues("rx", "invalid").Inc()
		return
	}
	req.IfIndex = ifIndex
	if req.HType != 1 || req.HLen != 6 { // Ethernet only for now
		s.logger.Warn("unsupported hw type", "htype", req.HType, "hlen", req.HLen)
		metrics.DHCPPacketsTotal.WithLabelValues("rx", "unsupported").Inc()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msgType := messageTypeName(req.Options.MessageType())
	metrics.DHCPPacketsTotal.WithLabelValues("rx", msgType).Inc()

	switch req.Options.MessageType() {
	case DHCPDiscover:
		s.handleDiscover(ctx, req, addr)
	case DHCPRequest:
		s.handleRequest(ctx, req, addr)
	case DHCPRelease:
		s.handleRelease(ctx, req)
	case DHCPDecline:
		s.handleDecline(ctx, req)
	case DHCPInform:
		s.handleInform(ctx, req, addr)
	case DHCPOffer, DHCPACK, DHCPNAK:
		// These are server-to-client replies; ignore them. They can be
		// reflected back to us by a relay or another server on the same
		// broadcast domain.
		return
	default:
		s.logger.Warn("unsupported message type", "type", req.Options.MessageType())
	}
}

func (s *Server) isBlacklisted(ctx context.Context, mac string) bool {
	blacklisted, err := s.store.IsMACBlacklisted(ctx, mac)
	if err != nil {
		s.logger.Error("check mac blacklist", "mac", mac, "err", err)
	}
	return blacklisted
}

func (s *Server) handleDiscover(ctx context.Context, req *Packet, addr *net.UDPAddr) {
	if s.isBlacklisted(ctx, req.CHAddr.String()) {
		s.logger.Info("discarded discover from blacklisted mac", "mac", req.CHAddr)
		return
	}
	mac := req.CHAddr.String()
	if !req.GIAddr.Equal(net.IPv4zero) {
		// Relayed copy: answer immediately and drop any buffered local-broadcast
		// copy of the same Discover.
		if s.cancelPendingDiscover(mac) {
			s.logger.Debug("relay copy arrived, dropping buffered local broadcast", "mac", mac)
		}
		s.processDiscover(ctx, req, addr)
		return
	}
	// Local broadcast: wait a short window for a relayed copy. If one arrives,
	// the relayed path is preferred and this local copy is discarded.
	s.schedulePendingDiscover(ctx, req, addr, mac)
}

func (s *Server) processDiscover(ctx context.Context, req *Packet, addr *net.UDPAddr) {
	mac := req.CHAddr.String()
	scope, err := s.matchScope(req)
	if err != nil {
		s.logger.Warn("no scope for discover", "mac", mac, "err", err)
		return
	}
	if !scope.Enabled {
		return
	}

	// In multi-node deployments, optionally delay Discover replies. Nodes with a
	// shorter delay will write their offered lease first; delayed nodes check the
	// database again before replying and skip if another node already offered.
	if s.cfg.Cluster.Enabled && s.cfg.Cluster.DiscoverReplyDelay > 0 {
		if existing, _ := s.store.GetLeaseByMAC(ctx, scope.ID, mac); existing != nil && (existing.State == models.LeaseActive || existing.State == models.LeaseOffered) {
			s.logger.Debug("discover skipped, peer already offered", "mac", mac, "ip", existing.IPAddr)
			return
		}
		select {
		case <-time.After(s.cfg.Cluster.DiscoverReplyDelay):
		case <-ctx.Done():
			return
		}
		if existing, _ := s.store.GetLeaseByMAC(ctx, scope.ID, mac); existing != nil && (existing.State == models.LeaseActive || existing.State == models.LeaseOffered) {
			s.logger.Debug("discover skipped after delay, peer already offered", "mac", mac, "ip", existing.IPAddr)
			return
		}
	}

	// Serialize IP allocation within a scope as an extra local safeguard in
	// addition to the database-level advisory lock used by AllocateV4Lease.
	allocMu := s.scopeAllocMu(scope.ID)
	allocMu.Lock()
	defer allocMu.Unlock()

	clientID := clientIDString(req.Options.ClientIdentifier())
	if clientID == "" {
		clientID = mac
	}

	leaseTime := scope.LeaseTime
	if leaseTime == 0 {
		leaseTime = DefaultLeaseTime
	}
	maxLeaseTime := scope.MaxLeaseTime
	if maxLeaseTime == 0 {
		maxLeaseTime = DefaultMaxLeaseTime
	}

	reservation, _ := s.store.GetReservationByMAC(ctx, scope.ID, mac)
	groupOpts := s.groupOptionsForReservation(ctx, reservation)

	lease, err := s.store.AllocateV4Lease(ctx, scope, mac, clientID, req.Options.RequestedIP(), sanitizeUTF8(req.Options.HostName()), leaseTime, maxLeaseTime)
	if err != nil {
		s.logger.Warn("allocate v4 lease failed", "mac", mac, "scope", scope.Name, "err", err)
		return
	}

	s.logger.Info("lease offered", "mac", mac, "ip", lease.IPAddr, "scope", scope.Name, "giaddr", req.GIAddr, "ifindex", req.IfIndex)
	s.logIPAllocation(ctx, scope, mac, lease.IPAddr, "offer", req, lease.Hostname)

	reply := ReplyFromRequest(req, DHCPOffer)
	reply.YIAddr = lease.IPAddr
	s.setServerIdentifier(reply, req)
	s.applyOptions(req, reply, scope, reservation, groupOpts)
	s.sendReply(reply, addr)
}

func (s *Server) handleRequest(ctx context.Context, req *Packet, addr *net.UDPAddr) {
	if s.isBlacklisted(ctx, req.CHAddr.String()) {
		s.logger.Info("discarded request from blacklisted mac", "mac", req.CHAddr.String())
		s.sendNAK(req, addr, "mac blacklisted")
		return
	}
	scope, err := s.matchScope(req)
	if err != nil {
		s.logger.Warn("no scope for request", "mac", req.CHAddr.String(), "err", err)
		s.sendNAK(req, addr, "no scope")
		return
	}
	if !scope.Enabled {
		s.sendNAK(req, addr, "scope disabled")
		return
	}

	mac := req.CHAddr.String()
	clientID := clientIDString(req.Options.ClientIdentifier())
	if clientID == "" {
		clientID = mac
	}

	requestedIP := req.Options.RequestedIP()
	if requestedIP == nil {
		requestedIP = req.CIAddr
	}
	if requestedIP != nil {
		requestedIP = normalizeIP(requestedIP)
	}
	ciaddr := normalizeIP(req.CIAddr)
	serverID := req.Options.ServerID()
	s.logger.Debug("received request", "mac", mac, "giaddr", req.GIAddr, "ciaddr", req.CIAddr, "requested_ip", requestedIP, "server_id", serverID, "from", addr)
	if serverID != nil && !serverID.Equal(s.serverIdentifierFor(req)) {
		// Not for us, ignore
		s.logger.Info("ignoring request for another server", "mac", mac, "server_id", serverID, "our_id", s.serverIdentifierFor(req))
		return
	}

	reservation, _ := s.store.GetReservationByMAC(ctx, scope.ID, mac)
	groupOpts := s.groupOptionsForReservation(ctx, reservation)

	if reservation != nil && requestedIP != nil && ipKey(requestedIP) != ipKey(reservation.IPAddr) {
		s.logger.Warn("requested ip mismatch reservation", "mac", mac, "requested_ip", requestedIP, "reservation_ip", reservation.IPAddr, "scope", scope.Name)
		s.sendNAK(req, addr, "requested ip mismatch reservation")
		return
	}

	if requestedIP != nil && !scope.Subnet.Contains(requestedIP) {
		s.logger.Warn("requested ip out of subnet", "mac", mac, "requested_ip", requestedIP, "scope", scope.Name, "subnet", scope.Subnet)
		s.sendNAK(req, addr, "ip out of subnet")
		return
	}

	leaseTime := scope.LeaseTime
	if leaseTime == 0 {
		leaseTime = DefaultLeaseTime
	}
	// Respect client requested lease time if in range
	if requestedLease, ok := req.Options.Get(OptIPAddressLeaseTime); ok && len(requestedLease) == 4 {
		rl := int(time.Duration(binaryBigEndianUint32(requestedLease)) * time.Second / time.Second)
		if rl > 0 && rl < leaseTime {
			leaseTime = rl
		}
	}
	maxTime := scope.MaxLeaseTime
	if maxTime == 0 {
		maxTime = DefaultMaxLeaseTime
	}
	if leaseTime > maxTime {
		leaseTime = maxTime
	}

	// Allocate/confirm the lease atomically. In active/active mode this also
	// protects against a concurrent node assigning the same IP to another client.
	lease, err := s.store.AllocateV4Lease(ctx, scope, mac, clientID, requestedIP, sanitizeUTF8(req.Options.HostName()), leaseTime, maxTime)
	if err != nil {
		resIP := ""
		if reservation != nil {
			resIP = reservation.IPAddr.String()
		}
		s.logger.Warn("confirm v4 lease failed", "mac", mac, "requested_ip", requestedIP, "reservation_ip", resIP, "scope", scope.Name, "err", err)
		s.sendNAK(req, addr, "ip unavailable")
		return
	}

	// Activate the offered lease. The lease ID is stable because AllocateV4Lease
	// upserts by (scope_id, mac_addr).
	if err := s.store.UpdateLeaseState(ctx, lease.ID, models.LeaseActive); err != nil {
		s.logger.Error("activate lease", "err", err)
		s.sendNAK(req, addr, "database error")
		return
	}

	isRenewal := requestedIP != nil && ciaddr != nil && !ciaddr.Equal(net.IPv4zero) && ciaddr.Equal(requestedIP)

	reply := ReplyFromRequest(req, DHCPACK)
	reply.YIAddr = lease.IPAddr
	s.setServerIdentifier(reply, req)
	s.applyOptions(req, reply, scope, reservation, groupOpts)
	s.sendReply(reply, addr)
	if isRenewal {
		s.logger.Info("lease renewed", "mac", mac, "ip", lease.IPAddr, "scope", scope.Name)
		s.logIPAllocation(ctx, scope, mac, lease.IPAddr, "renew", req, lease.Hostname)
	} else {
		s.logger.Info("lease ack", "mac", mac, "ip", lease.IPAddr, "scope", scope.Name)
		s.logIPAllocation(ctx, scope, mac, lease.IPAddr, "ack", req, lease.Hostname)
	}
}

func (s *Server) handleRelease(ctx context.Context, req *Packet) {
	scope, err := s.matchScope(req)
	if err != nil {
		return
	}
	mac := req.CHAddr.String()
	lease, _ := s.store.GetLeaseByMAC(ctx, scope.ID, mac)
	if lease != nil {
		_ = s.store.UpdateLeaseState(ctx, lease.ID, models.LeaseReleased)
		metrics.LeasesReleased.Inc()
		s.logger.Info("lease released", "mac", mac, "ip", lease.IPAddr)
		s.logIPAllocation(ctx, scope, mac, lease.IPAddr, "release", req, lease.Hostname)
	}
}

func (s *Server) handleDecline(ctx context.Context, req *Packet) {
	scope, err := s.matchScope(req)
	if err != nil {
		return
	}
	mac := req.CHAddr.String()
	lease, _ := s.store.GetLeaseByMAC(ctx, scope.ID, mac)
	if lease != nil {
		_ = s.store.UpdateLeaseState(ctx, lease.ID, models.LeaseDeclined)
		metrics.LeasesDeclined.Inc()
		s.logger.Warn("lease declined", "mac", mac, "ip", lease.IPAddr)
		s.logIPAllocation(ctx, scope, mac, lease.IPAddr, "decline", req, lease.Hostname)
	}
}

func (s *Server) handleInform(ctx context.Context, req *Packet, addr *net.UDPAddr) {
	scope, err := s.matchScope(req)
	if err != nil {
		return
	}
	mac := req.CHAddr.String()
	reservation, _ := s.store.GetReservationByMAC(ctx, scope.ID, mac)
	groupOpts := s.groupOptionsForReservation(ctx, reservation)

	reply := ReplyFromRequest(req, DHCPACK)
	reply.YIAddr = net.IPv4zero
	s.setServerIdentifier(reply, req)
	s.applyOptions(req, reply, scope, reservation, groupOpts)
	s.sendReply(reply, addr)
}

func (s *Server) sendNAK(req *Packet, addr *net.UDPAddr, reason string) {
	reply := ReplyFromRequest(req, DHCPNAK)
	s.setServerIdentifier(reply, req)
	reply.Options.Set(OptMessage, []byte(reason))
	s.sendReply(reply, addr)
	s.logger.Info("sent nak", "mac", req.CHAddr.String(), "reason", reason)
}

func (s *Server) logIPAllocation(ctx context.Context, scope *models.Scope, mac string, ip net.IP, action string, req *Packet, hostname string) {
	if scope == nil {
		return
	}
	giaddr := normalizeIP(req.GIAddr)
	clientID := clientIDString(req.Options.ClientIdentifier())
	info := map[string]string{
		"client_id":    clientID,
		"hostname":     hostname,
		"message_type": messageTypeName(req.Options.MessageType()),
	}
	infoBytes, _ := json.Marshal(info)
	log := &models.IPAllocationLog{
		ID:         uuid.New().String(),
		NodeID:     s.cfg.Cluster.NodeID,
		ScopeID:    scope.ID,
		ScopeName:  scope.Name,
		MACAddr:    mac,
		IPAddr:     ip,
		Action:     action,
		ClientInfo: infoBytes,
		RelayIP:    giaddr,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.store.CreateIPAllocationLog(ctx, log); err != nil {
		s.logger.Error("create ip allocation log", "err", err)
	}
}

func (s *Server) sendReply(reply *Packet, addr *net.UDPAddr) {
	data, err := reply.Marshal()
	if err != nil {
		s.logger.Error("marshal reply", "err", err)
		return
	}
	metrics.DHCPRepliesTotal.WithLabelValues(messageTypeName(reply.Options.MessageType())).Inc()

	giaddr := normalizeIP(reply.GIAddr)
	ciaddr := normalizeIP(reply.CIAddr)

	// Relayed request: send reply back to the relay agent, preserving the
	// broadcast flag requested by the client (RFC 2131 compliant).
	if !giaddr.Equal(net.IPv4zero) {
		// Relayed traffic: always unicast to the relay's server port.
		// The relay is responsible for forwarding to the client, even for NAK.
		dest := &net.UDPAddr{IP: giaddr, Port: 67}
		if _, err := s.pktConn.WriteTo(data, nil, dest); err != nil {
			s.logger.Error("send reply", "err", err, "dest", dest)
		}
		return
	}

	// NAK must be broadcast; some clients also set the broadcast bit.
	isBroadcast := reply.Options.MessageType() == DHCPNAK ||
		reply.Flags&0x8000 != 0 ||
		reply.YIAddr.Equal(net.IPv4zero)

	if isBroadcast {
		dest := &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
		var cm *ipv4.ControlMessage
		if reply.IfIndex != 0 {
			cm = &ipv4.ControlMessage{IfIndex: reply.IfIndex}
		}
		if _, err := s.pktConn.WriteTo(data, cm, dest); err != nil {
			s.logger.Error("send reply", "err", err, "dest", dest)
		} else {
			s.logger.Debug("sent reply", "dest", dest, "type", messageTypeName(reply.Options.MessageType()), "ifindex", reply.IfIndex)
		}
		return
	}

	if !ciaddr.Equal(net.IPv4zero) {
		// Renewing/rebinding client has an address; unicast normally.
		dest := &net.UDPAddr{IP: ciaddr, Port: 68}
		if _, err := s.pktConn.WriteTo(data, nil, dest); err != nil {
			s.logger.Error("send reply", "err", err, "dest", dest)
		}
		return
	}

	// Client requested unicast (broadcast bit not set) and has no IP yet.
	// Send a raw L2 unicast frame to the client's hardware address.
	s.logger.Debug("sending raw unicast reply", "mac", reply.CHAddr.String(), "ip", reply.YIAddr, "type", messageTypeName(reply.Options.MessageType()))
	if err := s.sendRawUnicast(reply); err != nil {
		s.logger.Warn("raw unicast failed, falling back to broadcast", "mac", reply.CHAddr.String(), "err", err)
		dest := &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
		var cm *ipv4.ControlMessage
		if reply.IfIndex != 0 {
			cm = &ipv4.ControlMessage{IfIndex: reply.IfIndex}
		}
		if _, err := s.pktConn.WriteTo(data, cm, dest); err != nil {
			s.logger.Error("send reply", "err", err, "dest", dest)
		}
	}
}

func (s *Server) sendRawUnicast(reply *Packet) error {
	if reply.IfIndex == 0 {
		return fmt.Errorf("no receiving interface")
	}
	iface, err := net.InterfaceByIndex(reply.IfIndex)
	if err != nil {
		return err
	}
	if len(iface.HardwareAddr) == 0 {
		return fmt.Errorf("interface %d has no hardware address", reply.IfIndex)
	}
	srcIP := s.serverIdentifierFor(reply)
	if srcIP == nil {
		return fmt.Errorf("no source IP for interface %d", reply.IfIndex)
	}
	payload, err := reply.Marshal()
	if err != nil {
		return err
	}

	eth := &layers.Ethernet{
		SrcMAC:       iface.HardwareAddr,
		DstMAC:       reply.CHAddr,
		EthernetType: layers.EthernetTypeIPv4,
	}
	// Use the limited broadcast destination so that clients whose IP stack is
	// not yet configured can still accept the reply, while addressing the frame
	// directly to the client's MAC.
	ip := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    srcIP,
		DstIP:    net.IPv4bcast,
	}
	udp := &layers.UDP{
		SrcPort: layers.UDPPort(67),
		DstPort: layers.UDPPort(68),
	}
	udp.SetNetworkLayerForChecksum(ip)

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{ComputeChecksums: true, FixLengths: true}
	if err := gopacket.SerializeLayers(buf, opts, eth, ip, udp, gopacket.Payload(payload)); err != nil {
		return err
	}
	if s.rawFd == 0 {
		return fmt.Errorf("raw socket not available")
	}
	addr := &unix.SockaddrLinklayer{
		Ifindex:  reply.IfIndex,
		Protocol: htons(unix.ETH_P_IP),
		Halen:    6,
	}
	copy(addr.Addr[:], reply.CHAddr)
	if err := unix.Sendto(s.rawFd, buf.Bytes(), 0, addr); err != nil {
		return err
	}
	s.logger.Debug("raw unicast sent", "mac", reply.CHAddr.String(), "ip", reply.YIAddr, "ifindex", reply.IfIndex)
	return nil
}

func (s *Server) matchScope(req *Packet) (*models.Scope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	giaddr := normalizeIP(req.GIAddr)
	ciaddr := normalizeIP(req.CIAddr)

	var candidates []*models.Scope
	for _, sc := range s.scopes {
		if !sc.Enabled {
			continue
		}
		if sc.Subnet.Contains(giaddr) && !giaddr.Equal(net.IPv4zero) {
			candidates = append(candidates, sc)
		} else if sc.Subnet.Contains(ciaddr) && !ciaddr.Equal(net.IPv4zero) {
			candidates = append(candidates, sc)
		} else if giaddr.Equal(net.IPv4zero) && ciaddr.Equal(net.IPv4zero) {
			// Direct broadcast from client; match by the receiving interface subnet
			// so we don't answer broadcasts arriving on unrelated interfaces.
			localSubnet := s.interfaceSubnet(req.IfIndex)
			if localSubnet == nil {
				localSubnet = &net.IPNet{IP: s.serverIdentifier().Mask(net.IPv4Mask(255, 255, 255, 255)), Mask: net.IPv4Mask(255, 255, 255, 255)}
			}
			if sc.Subnet.Contains(localSubnet.IP) {
				candidates = append(candidates, sc)
			}
		}
	}
	if len(candidates) == 0 {
		var subnets []string
		for _, sc := range s.scopes {
			subnets = append(subnets, sc.Subnet.String())
		}
		s.logger.Warn("no matching scope debug", "giaddr", req.GIAddr, "ciaddr", req.CIAddr, "subnets", subnets, "server_id", s.serverIdentifier())
		return nil, fmt.Errorf("no matching scope")
	}
	// Return smallest subnet (most specific)
	best := candidates[0]
	for _, sc := range candidates[1:] {
		if onesBest, _ := best.Subnet.Mask.Size(); onesBest > 0 {
			if ones, _ := sc.Subnet.Mask.Size(); ones > onesBest {
				best = sc
			}
		}
	}
	return best, nil
}

func (s *Server) groupOptionsForReservation(ctx context.Context, r *models.Reservation) json.RawMessage {
	if r == nil || r.GroupID == "" {
		return nil
	}
	g, err := s.store.GetReservationGroupByID(ctx, r.GroupID)
	if err != nil {
		return nil
	}
	return g.Options
}

func (s *Server) applyOptions(req *Packet, reply *Packet, scope *models.Scope, reservation *models.Reservation, groupOpts json.RawMessage) {
	scopeOpts := models.ParseOptionMap(scope.Options)
	groupOptMap := models.ParseOptionMap(groupOpts)
	resOptMap := models.OptionMap{}
	if reservation != nil {
		resOptMap = models.ParseOptionMap(reservation.Options)
	}

	// Helper to add option
	addOpt := func(code byte, value interface{}) {
		raw, err := BuildOption(code, value.(models.OptionValue))
		if err == nil {
			reply.Options.Set(code, raw)
		}
	}

	// Common options from scope fields
	reply.Options.Set(OptIPAddressLeaseTime, Uint32ToBytes(uint32(scope.LeaseTime)))
	reply.Options.Set(OptRenewalTimeValue, Uint32ToBytes(uint32(scope.LeaseTime/2)))
	reply.Options.Set(OptRebindingTimeValue, Uint32ToBytes(uint32(scope.LeaseTime*7/8)))
	if len(scope.Gateway) > 0 {
		reply.Options.Set(OptRouter, IPsToBytes(scope.Gateway))
	}
	if len(scope.DNS) > 0 {
		reply.Options.Set(OptDomainNameServer, IPsToBytes(scope.DNS))
	}
	if scope.DomainName != "" {
		reply.Options.Set(OptDomainName, []byte(scope.DomainName))
	}
	if scope.Subnet != nil {
		reply.Options.Set(OptSubnetMask, IPToBytes(net.IP(scope.Subnet.Mask)))
	}

	// Merge options: group -> scope custom -> reservation.
	// Later layers override earlier layers.
	for k, v := range groupOptMap {
		code := parseOptionCode(k)
		if code == 0 {
			continue
		}
		raw, err := BuildOption(code, optionMapToValue(v))
		if err == nil {
			reply.Options.Set(code, raw)
		}
	}

	for k, v := range scopeOpts {
		code := parseOptionCode(k)
		if code == 0 {
			continue
		}
		raw, err := BuildOption(code, optionMapToValue(v))
		if err == nil {
			reply.Options.Set(code, raw)
		}
	}

	if reservation != nil {
		for k, v := range resOptMap {
			code := parseOptionCode(k)
			if code == 0 {
				continue
			}
			raw, err := BuildOption(code, optionMapToValue(v))
			if err == nil {
				reply.Options.Set(code, raw)
			}
		}
	}

	// Filter by parameter request list, but always keep mandatory options that
	// every client needs to accept an Offer/ACK.
	prl := req.Options.ParameterRequestList()
	if len(prl) > 0 {
		filtered := make(Options)
		filtered.Set(OptDHCPMessageType, reply.Options[OptDHCPMessageType])
		for _, code := range prl {
			if v, ok := reply.Options.Get(code); ok {
				filtered.Set(code, v)
			}
		}
		for _, code := range []byte{
			OptServerIdentifier,
			OptIPAddressLeaseTime,
			OptRenewalTimeValue,
			OptRebindingTimeValue,
		} {
			if v, ok := reply.Options.Get(code); ok {
				filtered.Set(code, v)
			}
		}
		reply.Options = filtered
	}

	_ = addOpt // suppress unused warning if build path changes
}

func optionMapToValue(v interface{}) models.OptionValue {
	b, _ := json.Marshal(v)
	var ov models.OptionValue
	_ = json.Unmarshal(b, &ov)
	return ov
}

func parseOptionCode(s string) byte {
	// supports "53" or "dhcp-message-type" aliases
	code, err := strconvAtoi(s)
	if err == nil && code >= 0 && code <= 255 {
		return byte(code)
	}
	return 0
}

func (s *Server) setServerIdentifier(reply *Packet, req *Packet) {
	reply.Options.Set(OptServerIdentifier, IPToBytes(s.serverIdentifierFor(req)))
}

func (s *Server) serverIdentifier() net.IP {
	return s.serverIdentifierFor(&Packet{})
}

func (s *Server) serverIdentifierFor(req *Packet) net.IP {
	// For relayed traffic use the configured server interface address so that
	// replies routed back through the relay are sourced from an address the
	// relay/client expects.
	useConfigured := !req.GIAddr.Equal(net.IPv4zero)
	if req.GIAddr.Equal(net.IPv4zero) && req.IfIndex != 0 {
		// Direct broadcast: use the address of the receiving interface so the
		// client sees a server identifier on its own subnet.
		if ip := s.interfaceIPv4(req.IfIndex); ip != nil {
			return ip
		}
		useConfigured = true
	}
	if useConfigured && s.cfg.Server.Interface != "" {
		iface, err := net.InterfaceByName(s.cfg.Server.Interface)
		if err == nil {
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
					return ipnet.IP.To4()
				}
			}
		}
	}
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
				return ipnet.IP.To4()
			}
		}
	}
	return net.IPv4(127, 0, 0, 1)
}

func (s *Server) interfaceIPv4(ifIndex int) net.IP {
	iface, err := net.InterfaceByIndex(ifIndex)
	if err != nil {
		return nil
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
			return ipnet.IP.To4()
		}
	}
	return nil
}

func (s *Server) interfaceSubnet(ifIndex int) *net.IPNet {
	iface, err := net.InterfaceByIndex(ifIndex)
	if err != nil {
		return nil
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
			return &net.IPNet{IP: ipnet.IP.Mask(ipnet.Mask), Mask: ipnet.Mask}
		}
	}
	return nil
}

func ipInRange(ip, start, end net.IP) bool {
	ipU := ipToUint32(ip.To4())
	startU := ipToUint32(start.To4())
	endU := ipToUint32(end.To4())
	return ipU >= startU && ipU <= endU
}

// ipKey returns a canonical string for an IP address so that IPv4-mapped
// IPv6 addresses (e.g. ::ffff:192.168.1.1) and plain IPv4 addresses compare
// equal. This is needed because PostgreSQL inet columns may return
// IPv4-mapped addresses while our local allocations use net.IPv4.
func ipKey(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32ToIP(v uint32) net.IP {
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func binaryBigEndianUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func messageTypeName(t byte) string {
	switch t {
	case DHCPDiscover:
		return "discover"
	case DHCPOffer:
		return "offer"
	case DHCPRequest:
		return "request"
	case DHCPDecline:
		return "decline"
	case DHCPACK:
		return "ack"
	case DHCPNAK:
		return "nak"
	case DHCPRelease:
		return "release"
	case DHCPInform:
		return "inform"
	default:
		return "unknown"
	}
}

func strconvAtoi(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid digit")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
