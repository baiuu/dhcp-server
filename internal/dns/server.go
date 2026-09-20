// Package dns implements a lightweight authoritative DNS server that answers
// queries from the DHCP lease database: hostnames of DHCP clients resolve to
// their leased addresses (A/AAAA) and addresses resolve back to hostnames
// (PTR), similar to dnsmasq's DHCP-derived DNS records.
package dns

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dhcp-server/dhcp-server/internal/config"
	"github.com/dhcp-server/dhcp-server/internal/store"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	qtypeA    = 1
	qtypePTR  = 12
	qtypeAAAA = 28
	qtypeANY  = 255

	rcodeNoError  = 0
	rcodeFormErr  = 1
	rcodeNXDomain = 3
)

type Server struct {
	cfg    *config.Config
	store  *store.Store
	logger *slog.Logger
	udp    *net.UDPConn
	p4     *ipv4.PacketConn
	p6     *ipv6.PacketConn
	tcp    net.Listener
	quit   chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
}

func NewServer(cfg *config.Config, st *store.Store, logger *slog.Logger) *Server {
	return &Server{
		cfg:    cfg,
		store:  st,
		logger: logger,
		quit:   make(chan struct{}),
	}
}

func (s *Server) Start(ctx context.Context) error {
	udpAddr, err := net.ResolveUDPAddr("udp", s.cfg.DNS.Listen)
	if err != nil {
		return fmt.Errorf("resolve dns udp: %w", err)
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen dns udp: %w", err)
	}
	s.udp = udpConn

	// Enable packet-info so replies are sent from the exact destination
	// address and interface the client queried (RFC: the response source
	// must match the query destination). Without this the kernel picks the
	// outgoing interface via the routing table, which can be wrong on hosts
	// with tunnel/VPN interfaces.
	if udpAddr.IP.To4() != nil {
		s.p4 = ipv4.NewPacketConn(udpConn)
		if err := s.p4.SetControlMessage(ipv4.FlagDst|ipv4.FlagInterface, true); err != nil {
			s.logger.Warn("dns: enable IP_PKTINFO failed, replies may use a wrong source", "err", err)
		}
	} else {
		s.p6 = ipv6.NewPacketConn(udpConn)
		if err := s.p6.SetControlMessage(ipv6.FlagDst|ipv6.FlagInterface, true); err != nil {
			s.logger.Warn("dns: enable IPV6_PKTINFO failed, replies may use a wrong source", "err", err)
		}
	}

	tcpLn, err := net.Listen("tcp", udpAddr.String())
	if err != nil {
		_ = udpConn.Close()
		return fmt.Errorf("listen dns tcp: %w", err)
	}
	s.tcp = tcpLn

	s.wg.Add(2)
	go s.serveUDP(ctx)
	go s.serveTCP(ctx)
	s.logger.Info("dns server listening", "addr", s.cfg.DNS.Listen, "ttl", s.cfg.DNS.TTL)
	return nil
}

func (s *Server) Stop() error {
	s.once.Do(func() {
		close(s.quit)
		if s.udp != nil {
			_ = s.udp.Close()
		}
		if s.tcp != nil {
			_ = s.tcp.Close()
		}
	})
	s.wg.Wait()
	return nil
}

func (s *Server) serveUDP(ctx context.Context) {
	defer s.wg.Done()
	buf := make([]byte, 4096)
	for {
		var (
			n       int
			rawAddr net.Addr
			cm4     *ipv4.ControlMessage
			cm6     *ipv6.ControlMessage
			err     error
		)
		if s.p4 != nil {
			n, cm4, rawAddr, err = s.p4.ReadFrom(buf)
		} else {
			n, cm6, rawAddr, err = s.p6.ReadFrom(buf)
		}
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				s.logger.Error("dns udp read", "err", err)
				continue
			}
		}
		addr, ok := rawAddr.(*net.UDPAddr)
		if !ok || addr == nil {
			s.logger.Warn("dns udp packet from non-udp address", "src", rawAddr)
			continue
		}
		data := append([]byte(nil), buf[:n]...)
		go func() {
			qctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			resp := s.handleQuery(qctx, data)
			if resp == nil {
				return
			}
			var werr error
			if s.p4 != nil {
				out := &ipv4.ControlMessage{}
				if cm4 != nil && validReplySource(cm4.Dst) {
					out.Src = cm4.Dst
					out.IfIndex = cm4.IfIndex
				}
				_, werr = s.p4.WriteTo(resp, out, addr)
			} else {
				out := &ipv6.ControlMessage{}
				if cm6 != nil && validReplySource(cm6.Dst) {
					out.Src = cm6.Dst
					out.IfIndex = cm6.IfIndex
				}
				_, werr = s.p6.WriteTo(resp, out, addr)
			}
			if werr != nil {
				s.logger.Error("dns udp write", "err", werr, "dest", addr)
			}
		}()
	}
}

// validReplySource reports whether addr can be used as a reply source: it
// must be a concrete unicast address (the one the client sent its query to).
func validReplySource(ip net.IP) bool {
	return ip != nil && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.Equal(net.IPv4bcast)
}

func (s *Server) serveTCP(ctx context.Context) {
	defer s.wg.Done()
	for {
		conn, err := s.tcp.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				s.logger.Error("dns tcp accept", "err", err)
				continue
			}
		}
		go s.handleTCPConn(conn)
	}
}

func (s *Server) handleTCPConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	var lenBuf [2]byte
	for {
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}
		n := int(binary.BigEndian.Uint16(lenBuf[:]))
		if n == 0 {
			return
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(conn, data); err != nil {
			return
		}
		qctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		resp := s.handleQuery(qctx, data)
		cancel()
		if resp == nil {
			return
		}
		binary.BigEndian.PutUint16(lenBuf[:], uint16(len(resp)))
		if _, err := conn.Write(lenBuf[:]); err != nil {
			return
		}
		if _, err := conn.Write(resp); err != nil {
			return
		}
	}
}

// handleQuery parses one DNS query message and builds the response message.
// It returns nil only when the message is so malformed that no response can
// be constructed at all.
func (s *Server) handleQuery(ctx context.Context, data []byte) []byte {
	q, err := parseQuery(data)
	if err != nil {
		return nil
	}

	rcode := byte(rcodeNoError)
	var answers [][]byte

	switch q.qtype {
	case qtypeA, qtypeAAAA, qtypeANY:
		v4, v6 := s.lookupName(ctx, q.name)
		if q.qtype == qtypeA || q.qtype == qtypeANY {
			for _, ip := range v4 {
				answers = append(answers, rrAddress(qtypeA, uint32(s.cfg.DNS.TTL), ip.To4()))
			}
		}
		if q.qtype == qtypeAAAA || q.qtype == qtypeANY {
			for _, ip := range v6 {
				answers = append(answers, rrAddress(qtypeAAAA, uint32(s.cfg.DNS.TTL), ip.To16()))
			}
		}
		if len(answers) == 0 && len(v4)+len(v6) == 0 {
			rcode = rcodeNXDomain
		}
	case qtypePTR:
		ip, ok := parsePTRName(q.name)
		if !ok {
			rcode = rcodeNXDomain
			break
		}
		host, domain, err := s.store.LookupPTR(ctx, ip)
		if err != nil {
			s.logger.Error("dns ptr lookup", "ip", ip, "err", err)
		}
		if host == "" {
			rcode = rcodeNXDomain
			break
		}
		answers = append(answers, rrPTR(uint32(s.cfg.DNS.TTL), ptrFQDN(host, domain)))
	default:
		// Unsupported types get a valid empty (NODATA) response.
	}

	if rcode == rcodeNXDomain {
		s.logger.Info("dns query not found", "name", q.name, "type", q.qtype)
	} else {
		s.logger.Debug("dns query", "name", q.name, "type", q.qtype, "answers", len(answers), "rcode", rcode)
	}
	return buildResponse(q, answers, rcode)
}

// lookupName resolves a query name to leased addresses. An FQDN query is
// split into host and domain: the domain must match the scope's domain_name
// so the answer comes from the right network. A bare short-name query (no
// dot) is searched across all scopes regardless of domain. When an FQDN
// query finds nothing in its own domain, it falls back to a domain-wide
// short-name search so hosts in scopes without a configured domain_name
// still resolve.
func (s *Server) lookupName(ctx context.Context, name string) (v4 []net.IP, v6 []net.IP) {
	host, domain := splitQuery(name)
	v4, v6, err := s.store.LookupHostname(ctx, host, domain)
	if err != nil {
		s.logger.Error("dns name lookup", "name", name, "err", err)
	}
	if len(v4)+len(v6) > 0 || domain == "" {
		return v4, v6
	}
	v4, v6, err = s.store.LookupHostname(ctx, host, "")
	if err != nil {
		s.logger.Error("dns name lookup fallback", "name", host, "err", err)
	}
	return v4, v6
}

// splitQuery splits a query name into host and domain parts:
// "pc-01.kfb.com" → ("pc-01", "kfb.com"), "pc-01" → ("pc-01", "").
func splitQuery(name string) (host, domain string) {
	if i := strings.Index(name, "."); i > 0 {
		return name[:i], name[i+1:]
	}
	return name, ""
}

// ptrFQDN assembles the PTR answer: hostname plus its scope's domain_name,
// or just the bare hostname when the scope has no domain configured.
func ptrFQDN(host, domain string) string {
	if domain == "" {
		return host + "."
	}
	return host + "." + domain + "."
}

// ---------- wire format ----------

type query struct {
	id            uint16
	flags         uint16
	name          string
	qtype         uint16
	questionBytes []byte // the question section verbatim, echoed in the response
}

var errShort = fmt.Errorf("message too short")

func parseQuery(data []byte) (*query, error) {
	if len(data) < 12 {
		return nil, errShort
	}
	qd := binary.BigEndian.Uint16(data[4:6])
	if qd != 1 {
		return nil, fmt.Errorf("unsupported question count %d", qd)
	}
	name, next, err := decodeName(data, 12)
	if err != nil {
		return nil, err
	}
	if next+4 > len(data) {
		return nil, errShort
	}
	qtype := binary.BigEndian.Uint16(data[next : next+2])
	qclass := binary.BigEndian.Uint16(data[next+2 : next+4])
	if qclass != 1 && qclass != 255 { // IN / ANY
		return nil, fmt.Errorf("unsupported qclass %d", qclass)
	}
	return &query{
		id:            binary.BigEndian.Uint16(data[0:2]),
		flags:         binary.BigEndian.Uint16(data[2:4]),
		name:          strings.ToLower(name),
		qtype:         qtype,
		questionBytes: append([]byte(nil), data[12:next+4]...),
	}, nil
}

func decodeName(data []byte, offset int) (string, int, error) {
	var labels []string
	for {
		if offset >= len(data) {
			return "", 0, errShort
		}
		l := int(data[offset])
		if l == 0 {
			offset++
			break
		}
		if l&0xC0 != 0 {
			return "", 0, fmt.Errorf("compressed name in question")
		}
		offset++
		if offset+l > len(data) {
			return "", 0, errShort
		}
		labels = append(labels, string(data[offset:offset+l]))
		offset += l
	}
	return strings.Join(labels, "."), offset, nil
}

func encodeName(name string) []byte {
	var out []byte
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			continue
		}
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	return append(out, 0)
}

// buildResponse assembles the response: header + the original question bytes
// (the answer name then uses a compression pointer to it).
func buildResponse(q *query, answers [][]byte, rcode byte) []byte {
	flags := uint16(0x8400) | // QR + AA (authoritative for local records)
		(q.flags & 0x0100) | // echo RD
		uint16(rcode)

	out := make([]byte, 12, 512)
	binary.BigEndian.PutUint16(out[0:2], q.id)
	binary.BigEndian.PutUint16(out[2:4], flags)
	binary.BigEndian.PutUint16(out[4:6], 1)
	binary.BigEndian.PutUint16(out[6:8], uint16(len(answers)))
	out = append(out, q.questionBytes...)
	for _, rr := range answers {
		out = append(out, rr...)
	}
	return out
}

func rrAddress(typ uint16, ttl uint32, ip net.IP) []byte {
	rdlen := 4
	if typ == qtypeAAAA {
		rdlen = 16
	}
	out := make([]byte, 0, 12+rdlen)
	out = append(out, 0xC0, 0x0C) // name compression pointer to the question name
	var tmp [8]byte
	binary.BigEndian.PutUint16(tmp[0:2], typ)
	binary.BigEndian.PutUint16(tmp[2:4], 1) // class IN
	binary.BigEndian.PutUint32(tmp[4:8], ttl)
	out = append(out, tmp[:]...)
	binary.BigEndian.PutUint16(tmp[0:2], uint16(rdlen))
	out = append(out, tmp[0:2]...)
	return append(out, ip...)
}

func rrPTR(ttl uint32, fqdn string) []byte {
	target := encodeName(fqdn)
	out := make([]byte, 0, 12+len(target))
	out = append(out, 0xC0, 0x0C)
	var tmp [8]byte
	binary.BigEndian.PutUint16(tmp[0:2], qtypePTR)
	binary.BigEndian.PutUint16(tmp[2:4], 1)
	binary.BigEndian.PutUint32(tmp[4:8], ttl)
	out = append(out, tmp[:]...)
	binary.BigEndian.PutUint16(tmp[0:2], uint16(len(target)))
	out = append(out, tmp[0:2]...)
	return append(out, target...)
}

// parsePTRName converts an in-addr.arpa / ip6.arpa query name back to an IP.
func parsePTRName(name string) (net.IP, bool) {
	if host, ok := strings.CutSuffix(name, ".in-addr.arpa"); ok {
		parts := strings.Split(host, ".")
		if len(parts) != 4 {
			return nil, false
		}
		ip := make(net.IP, 4)
		for i := 0; i < 4; i++ {
			n, err := strconv.Atoi(parts[3-i])
			if err != nil || n < 0 || n > 255 {
				return nil, false
			}
			ip[i] = byte(n)
		}
		return ip, true
	}
	if host, ok := strings.CutSuffix(name, ".ip6.arpa"); ok {
		nibbles := strings.Split(host, ".")
		if len(nibbles) != 32 {
			return nil, false
		}
		ip := make(net.IP, 16)
		for i := 0; i < 16; i++ {
			hi, ok1 := nibbleVal(nibbles[31-2*i])
			lo, ok2 := nibbleVal(nibbles[30-2*i])
			if !ok1 || !ok2 {
				return nil, false
			}
			ip[i] = hi<<4 | lo
		}
		return ip, true
	}
	return nil, false
}

func nibbleVal(s string) (byte, bool) {
	if len(s) != 1 {
		return 0, false
	}
	c := s[0]
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
