package dhcpv6

import (
	"encoding/binary"
	"fmt"
	"net"
)

const (
	// Message Types
	MsgTypeSolicit            = 1
	MsgTypeAdvertise          = 2
	MsgTypeRequest            = 3
	MsgTypeConfirm            = 4
	MsgTypeRenew              = 5
	MsgTypeRebind             = 6
	MsgTypeReply              = 7
	MsgTypeRelease            = 8
	MsgTypeDecline            = 9
	MsgTypeReconfigure        = 10
	MsgTypeInformationRequest = 11
	MsgTypeRelayForw          = 12
	MsgTypeRelayRepl          = 13

	// Option Codes
	OptClientID        = 1
	OptServerID        = 2
	OptIANA            = 3
	OptIATA            = 4
	OptIAAddr          = 5
	OptOptionRequest   = 6
	OptPreference      = 7
	OptElapsedTime     = 8
	OptRelayMsg        = 9
	OptAuth            = 11
	OptUnicast         = 12
	OptStatusCode      = 13
	OptRapidCommit     = 14
	OptUserClass       = 15
	OptVendorClass     = 16
	OptVendorOpts      = 17
	OptInterfaceID     = 18
	OptReconfMsg       = 19
	OptReconfAccept    = 20
	OptDNSServers      = 23
	OptDomainList      = 24
	OptIAPD            = 25
	OptIAPrefix        = 26
	OptSNTPServers     = 31
	OptInfoRefreshTime = 32
	OptSOLMaxRT        = 82
	OptINFMaxRT        = 83
)

var (
	AllDHCPRelayAgentsAndServers = net.ParseIP("ff02::1:2")
	AllDHCPServers               = net.ParseIP("ff05::1:3")
)

// Packet represents a DHCPv6 message
type Packet struct {
	MessageType byte
	TransID     [3]byte
	Options     Options
}

type Options []Option

type Option struct {
	Code uint16
	Data []byte
}

func (o Options) Get(code uint16) (Option, bool) {
	for _, opt := range o {
		if opt.Code == code {
			return opt, true
		}
	}
	return Option{}, false
}

func (o *Options) Add(code uint16, data []byte) {
	*o = append(*o, Option{Code: code, Data: data})
}

func (p *Packet) Marshal() ([]byte, error) {
	buf := make([]byte, 4)
	buf[0] = p.MessageType
	copy(buf[1:4], p.TransID[:])
	for _, opt := range p.Options {
		optBytes := make([]byte, 4+len(opt.Data))
		binary.BigEndian.PutUint16(optBytes[0:2], opt.Code)
		binary.BigEndian.PutUint16(optBytes[2:4], uint16(len(opt.Data)))
		copy(optBytes[4:], opt.Data)
		buf = append(buf, optBytes...)
	}
	return buf, nil
}

func ParsePacket(data []byte) (*Packet, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("packet too short")
	}
	msgType := data[0]
	if msgType == MsgTypeRelayForw || msgType == MsgTypeRelayRepl {
		return nil, fmt.Errorf("relay packet, use ParseRelayPacket")
	}
	p := &Packet{
		MessageType: msgType,
		TransID:     [3]byte{data[1], data[2], data[3]},
	}
	options := data[4:]
	for len(options) >= 4 {
		code := binary.BigEndian.Uint16(options[0:2])
		length := binary.BigEndian.Uint16(options[2:4])
		if len(options) < int(4+length) {
			return nil, fmt.Errorf("truncated option")
		}
		p.Options = append(p.Options, Option{Code: code, Data: append([]byte(nil), options[4:4+length]...)})
		options = options[4+length:]
	}
	return p, nil
}

// RelayPacket represents a DHCPv6 relay-forward/relay-reply message.
type RelayPacket struct {
	MessageType byte
	HopCount    byte
	LinkAddr    net.IP
	PeerAddr    net.IP
	Options     Options
}

func (rp *RelayPacket) Marshal() ([]byte, error) {
	buf := make([]byte, 34)
	buf[0] = rp.MessageType
	buf[1] = rp.HopCount
	copy(buf[2:18], rp.LinkAddr.To16())
	copy(buf[18:34], rp.PeerAddr.To16())
	for _, opt := range rp.Options {
		optBytes := make([]byte, 4+len(opt.Data))
		binary.BigEndian.PutUint16(optBytes[0:2], opt.Code)
		binary.BigEndian.PutUint16(optBytes[2:4], uint16(len(opt.Data)))
		copy(optBytes[4:], opt.Data)
		buf = append(buf, optBytes...)
	}
	return buf, nil
}

func ParseRelayPacket(data []byte) (*RelayPacket, error) {
	if len(data) < 34 {
		return nil, fmt.Errorf("relay packet too short")
	}
	rp := &RelayPacket{
		MessageType: data[0],
		HopCount:    data[1],
		LinkAddr:    net.IP(append([]byte(nil), data[2:18]...)),
		PeerAddr:    net.IP(append([]byte(nil), data[18:34]...)),
	}
	options := data[34:]
	for len(options) >= 4 {
		code := binary.BigEndian.Uint16(options[0:2])
		length := binary.BigEndian.Uint16(options[2:4])
		if len(options) < int(4+length) {
			return nil, fmt.Errorf("truncated option")
		}
		rp.Options = append(rp.Options, Option{Code: code, Data: append([]byte(nil), options[4:4+length]...)})
		options = options[4+length:]
	}
	return rp, nil
}

// GetRelayMsg extracts the inner message from a Relay-Message option.
// It recursively parses nested relay messages (multi-hop relays).
func (o Options) GetRelayMsg() (*Packet, *RelayPacket, error) {
	opt, ok := o.Get(OptRelayMsg)
	if !ok {
		return nil, nil, fmt.Errorf("no relay message option")
	}
	if len(opt.Data) < 1 {
		return nil, nil, fmt.Errorf("relay message empty")
	}
	msgType := opt.Data[0]
	if msgType == MsgTypeRelayForw || msgType == MsgTypeRelayRepl {
		rp, err := ParseRelayPacket(opt.Data)
		return nil, rp, err
	}
	p, err := ParsePacket(opt.Data)
	return p, nil, err
}

// InterfaceID returns the Interface-ID option (code 18) if present.
func (o Options) InterfaceID() ([]byte, bool) {
	opt, ok := o.Get(OptInterfaceID)
	if !ok {
		return nil, false
	}
	return opt.Data, true
}

// CopyRelayOptions copies all relay options except the Relay-Message option.
func (o Options) CopyRelayOptions() Options {
	var out Options
	for _, opt := range o {
		if opt.Code == OptRelayMsg {
			continue
		}
		out = append(out, Option{Code: opt.Code, Data: append([]byte(nil), opt.Data...)})
	}
	return out
}

func NewPacket(msgType byte, transID [3]byte) *Packet {
	return &Packet{
		MessageType: msgType,
		TransID:     transID,
		Options:     make(Options, 0),
	}
}

func ReplyFromRequest(req *Packet, msgType byte) *Packet {
	return NewPacket(msgType, req.TransID)
}

// DUID helper: DUID-LL (link-layer address)
func DUIDLL(hwAddr net.HardwareAddr) []byte {
	// DUID-LL: type(2) + hwtype(2) + link-layer-address
	duid := make([]byte, 4+len(hwAddr))
	binary.BigEndian.PutUint16(duid[0:2], 3) // DUID-LL
	binary.BigEndian.PutUint16(duid[2:4], 1) // Ethernet
	copy(duid[4:], hwAddr)
	return duid
}

func ParseDUID(data []byte) string {
	return fmt.Sprintf("%x", data)
}

// IA_NA option builder
func BuildIANA(iaid uint32, t1, t2 uint32, addrs []Option) []byte {
	body := make([]byte, 12)
	binary.BigEndian.PutUint32(body[0:4], iaid)
	binary.BigEndian.PutUint32(body[4:8], t1)
	binary.BigEndian.PutUint32(body[8:12], t2)
	for _, addr := range addrs {
		optBytes := make([]byte, 4+len(addr.Data))
		binary.BigEndian.PutUint16(optBytes[0:2], addr.Code)
		binary.BigEndian.PutUint16(optBytes[2:4], uint16(len(addr.Data)))
		copy(optBytes[4:], addr.Data)
		body = append(body, optBytes...)
	}
	return body
}

// IA_Address option builder
func BuildIAAddress(ip net.IP, preferredLifetime, validLifetime uint32) []byte {
	body := make([]byte, 24)
	copy(body[0:16], ip.To16())
	binary.BigEndian.PutUint32(body[16:20], preferredLifetime)
	binary.BigEndian.PutUint32(body[20:24], validLifetime)
	return body
}

func ParseIAAddress(data []byte) (net.IP, uint32, uint32, error) {
	if len(data) < 24 {
		return nil, 0, 0, fmt.Errorf("ia addr too short")
	}
	ip := net.IP(append([]byte(nil), data[0:16]...))
	pref := binary.BigEndian.Uint32(data[16:20])
	valid := binary.BigEndian.Uint32(data[20:24])
	return ip, pref, valid, nil
}

// IAPD option builder
func BuildIAPD(iaid uint32, t1, t2 uint32, prefixes []Option) []byte {
	body := make([]byte, 12)
	binary.BigEndian.PutUint32(body[0:4], iaid)
	binary.BigEndian.PutUint32(body[4:8], t1)
	binary.BigEndian.PutUint32(body[8:12], t2)
	for _, p := range prefixes {
		optBytes := make([]byte, 4+len(p.Data))
		binary.BigEndian.PutUint16(optBytes[0:2], p.Code)
		binary.BigEndian.PutUint16(optBytes[2:4], uint16(len(p.Data)))
		copy(optBytes[4:], p.Data)
		body = append(body, optBytes...)
	}
	return body
}

// IA Prefix option builder
func BuildIAPrefix(prefixLen byte, prefix net.IP, preferredLifetime, validLifetime uint32) []byte {
	body := make([]byte, 25)
	binary.BigEndian.PutUint32(body[0:4], preferredLifetime)
	binary.BigEndian.PutUint32(body[4:8], validLifetime)
	body[8] = prefixLen
	copy(body[9:25], prefix.To16())
	return body
}

func ParseIAPrefix(data []byte) (byte, net.IP, uint32, uint32, error) {
	if len(data) < 25 {
		return 0, nil, 0, 0, fmt.Errorf("ia prefix too short")
	}
	pref := binary.BigEndian.Uint32(data[0:4])
	valid := binary.BigEndian.Uint32(data[4:8])
	prefixLen := data[8]
	prefix := net.IP(append([]byte(nil), data[9:25]...))
	return prefixLen, prefix, pref, valid, nil
}

func BuildServerID(duid []byte) []byte {
	return append([]byte(nil), duid...)
}

func BuildStatusCode(code uint16, message string) []byte {
	body := make([]byte, 2+len(message))
	binary.BigEndian.PutUint16(body[0:2], code)
	copy(body[2:], message)
	return body
}

func BuildDNSList(ips []net.IP) []byte {
	var out []byte
	for _, ip := range ips {
		out = append(out, ip.To16()...)
	}
	return out
}

func BuildDomainList(domains []string) []byte {
	var out []byte
	for _, d := range domains {
		// RFC 1035 label encoding
		for _, label := range splitDomain(d) {
			out = append(out, byte(len(label)))
			out = append(out, []byte(label)...)
		}
		out = append(out, 0)
	}
	return out
}

func splitDomain(d string) []string {
	var labels []string
	start := 0
	for i := 0; i < len(d); i++ {
		if d[i] == '.' {
			if i > start {
				labels = append(labels, d[start:i])
			}
			start = i + 1
		}
	}
	if start < len(d) {
		labels = append(labels, d[start:])
	}
	return labels
}
