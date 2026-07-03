package dhcp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

const (
	BootRequest = 1
	BootReply   = 2

	// DHCP Message Types
	DHCPDiscover = 1
	DHCPOffer    = 2
	DHCPRequest  = 3
	DHCPDecline  = 4
	DHCPACK      = 5
	DHCPNAK      = 6
	DHCPRelease  = 7
	DHCPInform   = 8

	// Option Codes
	OptPad                           = 0
	OptSubnetMask                    = 1
	OptTimeOffset                    = 2
	OptRouter                        = 3
	OptTimeServer                    = 4
	OptNameServer                    = 5
	OptDomainNameServer              = 6
	OptLogServer                     = 7
	OptCookieServer                  = 8
	OptLPRServer                     = 9
	OptImpressServer                 = 10
	OptResourceLocationServer        = 11
	OptHostName                      = 12
	OptBootFileSize                  = 13
	OptMeritDumpFile                 = 14
	OptDomainName                    = 15
	OptSwapServer                    = 16
	OptRootPath                      = 17
	OptExtensionsPath                = 18
	OptIPForwarding                  = 19
	OptNonLocalSourceRouting         = 20
	OptPolicyFilter                  = 21
	OptMaxDatagramReassemblySize     = 22
	OptDefaultIPTimeToLive           = 23
	OptPathMTUAgingTimeout           = 24
	OptPathMTUPlateauTable           = 25
	OptInterfaceMTU                  = 26
	OptAllSubnetsAreLocal            = 27
	OptBroadcastAddress              = 28
	OptPerformMaskDiscovery          = 29
	OptMaskSupplier                  = 30
	OptPerformRouterDiscovery        = 31
	OptRouterSolicitationAddress     = 32
	OptStaticRoute                   = 33
	OptTrailerEncapsulation          = 34
	OptARPCacheTimeout               = 35
	OptEthernetEncapsulation         = 36
	OptTCPDefaultTTL                 = 37
	OptTCPKeepaliveInterval          = 38
	OptTCPKeepaliveGarbage           = 39
	OptNISDomain                     = 40
	OptNIServer                      = 41
	OptNTPServers                    = 42
	OptVendorSpecificInfo            = 43
	OptNetBIOSNameServer             = 44
	OptNetBIOSDatagramDistribution   = 45
	OptNetBIOSNodeType               = 46
	OptNetBIOSScope                  = 47
	OptXWindowSystemFontServer       = 48
	OptXWindowSystemDisplayManager   = 49
	OptRequestedIPAddr               = 50
	OptIPAddressLeaseTime            = 51
	OptOptionOverload                = 52
	OptDHCPMessageType               = 53
	OptServerIdentifier              = 54
	OptParameterRequestList          = 55
	OptMessage                       = 56
	OptMaxDHCPMessageSize            = 57
	OptRenewalTimeValue              = 58
	OptRebindingTimeValue            = 59
	OptVendorClassIdentifier         = 60
	OptClientIdentifier              = 61
	OptTFTPServerName                = 66
	OptBootfileName                  = 67
	OptMobileIPHomeAgent             = 68
	OptSMTPServer                    = 69
	OptPOP3Server                    = 70
	OptNNTPServer                    = 71
	OptWWWServer                     = 72
	OptFingerServer                  = 73
	OptIRCServer                     = 74
	OptStreetTalkServer              = 75
	OptStreetTalkDirectoryAssistance = 76
	OptEnd                           = 255
)

var (
	ErrInvalidPacket = errors.New("invalid dhcp packet")
	MagicCookie      = []byte{99, 130, 83, 99}
)

// Packet represents a DHCPv4 packet per RFC 2131
type Packet struct {
	Op       byte
	HType    byte
	HLen     byte
	Hops     byte
	XID      uint32
	Secs     uint16
	Flags    uint16
	CIAddr   net.IP
	YIAddr   net.IP
	SIAddr   net.IP
	GIAddr   net.IP
	CHAddr   net.HardwareAddr
	SName    [64]byte
	File     [128]byte
	Options  Options
	BootFile string // populated from OptBootfileName if present
	// IfIndex is set by the server to the index of the interface on which the
	// packet was received. It is not serialized on the wire.
	IfIndex int
}

type Options map[byte][]byte

func (o Options) Get(code byte) ([]byte, bool) {
	v, ok := o[code]
	return v, ok
}

func (o Options) Set(code byte, data []byte) {
	if data == nil {
		delete(o, code)
		return
	}
	o[code] = data
}

func (o Options) MessageType() byte {
	v, ok := o[OptDHCPMessageType]
	if !ok || len(v) == 0 {
		return 0
	}
	return v[0]
}

func (o Options) SetMessageType(t byte) {
	o.Set(OptDHCPMessageType, []byte{t})
}

func (o Options) RequestedIP() net.IP {
	v, ok := o[OptRequestedIPAddr]
	if !ok || len(v) < 4 {
		return nil
	}
	return net.IP(append([]byte(nil), v[:4]...))
}

func (o Options) ServerID() net.IP {
	v, ok := o[OptServerIdentifier]
	if !ok || len(v) < 4 {
		return nil
	}
	return net.IP(append([]byte(nil), v[:4]...))
}

func (o Options) ParameterRequestList() []byte {
	v, ok := o[OptParameterRequestList]
	if !ok {
		return nil
	}
	return v
}

func (o Options) HostName() string {
	v, ok := o[OptHostName]
	if !ok {
		return ""
	}
	return string(v)
}

func (o Options) ClientIdentifier() []byte {
	v, ok := o[OptClientIdentifier]
	if !ok {
		return nil
	}
	return v
}

// ParsePacket parses a raw DHCP packet from wire
func ParsePacket(data []byte) (*Packet, error) {
	if len(data) < 240 {
		return nil, fmt.Errorf("%w: too short (%d bytes)", ErrInvalidPacket, len(data))
	}
	p := &Packet{
		Options: make(Options),
	}
	p.Op = data[0]
	p.HType = data[1]
	p.HLen = data[2]
	p.Hops = data[3]
	p.XID = binary.BigEndian.Uint32(data[4:8])
	p.Secs = binary.BigEndian.Uint16(data[8:10])
	p.Flags = binary.BigEndian.Uint16(data[10:12])
	p.CIAddr = net.IP(append([]byte(nil), data[12:16]...))
	p.YIAddr = net.IP(append([]byte(nil), data[16:20]...))
	p.SIAddr = net.IP(append([]byte(nil), data[20:24]...))
	p.GIAddr = net.IP(append([]byte(nil), data[24:28]...))
	hlen := int(p.HLen)
	if hlen > 16 {
		hlen = 16
	}
	p.CHAddr = net.HardwareAddr(append([]byte(nil), data[28:28+hlen]...))
	copy(p.SName[:], data[44:108])
	copy(p.File[:], data[108:236])

	if !bytes.Equal(data[236:240], MagicCookie) {
		return nil, fmt.Errorf("%w: bad magic cookie", ErrInvalidPacket)
	}

	if err := p.Options.parse(data[240:]); err != nil {
		return nil, err
	}
	if p.Options.MessageType() == 0 {
		return nil, fmt.Errorf("%w: missing message type", ErrInvalidPacket)
	}
	return p, nil
}

func (o Options) parse(data []byte) error {
	i := 0
	for i < len(data) {
		code := data[i]
		if code == OptEnd {
			return nil
		}
		if code == OptPad {
			i++
			continue
		}
		if i+1 >= len(data) {
			return fmt.Errorf("%w: truncated option header", ErrInvalidPacket)
		}
		length := int(data[i+1])
		if i+2+length > len(data) {
			return fmt.Errorf("%w: truncated option data", ErrInvalidPacket)
		}
		o[code] = append([]byte(nil), data[i+2:i+2+length]...)
		i += 2 + length
	}
	return nil
}

// Marshal serializes a DHCP packet to wire format
func (p *Packet) Marshal() ([]byte, error) {
	buf := make([]byte, 240)
	buf[0] = p.Op
	buf[1] = p.HType
	buf[2] = p.HLen
	buf[3] = p.Hops
	binary.BigEndian.PutUint32(buf[4:8], p.XID)
	binary.BigEndian.PutUint16(buf[8:10], p.Secs)
	binary.BigEndian.PutUint16(buf[10:12], p.Flags)
	copy(buf[12:16], p.CIAddr.To4())
	copy(buf[16:20], p.YIAddr.To4())
	copy(buf[20:24], p.SIAddr.To4())
	copy(buf[24:28], p.GIAddr.To4())
	hlen := len(p.CHAddr)
	if hlen > 16 {
		hlen = 16
	}
	copy(buf[28:28+hlen], p.CHAddr)
	copy(buf[44:108], p.SName[:])
	copy(buf[108:236], p.File[:])
	copy(buf[236:240], MagicCookie)

	optBytes := p.Options.marshal()
	out := append(buf, optBytes...)
	if len(out) < 300 {
		// pad to minimum BOOTP/DHCP packet size
		padding := make([]byte, 300-len(out))
		out = append(out, padding...)
	}
	return out, nil
}

func (o Options) marshal() []byte {
	var buf []byte
	for code, data := range o {
		if code == OptEnd || code == OptPad {
			continue
		}
		for len(data) > 255 {
			buf = append(buf, code, 255)
			buf = append(buf, data[:255]...)
			data = data[255:]
		}
		buf = append(buf, code, byte(len(data)))
		buf = append(buf, data...)
	}
	buf = append(buf, OptEnd)
	return buf
}

// ReplyFromRequest creates a reply packet from a request
func ReplyFromRequest(req *Packet, msgType byte) *Packet {
	reply := &Packet{
		Op:      BootReply,
		HType:   req.HType,
		HLen:    req.HLen,
		Hops:    0,
		XID:     req.XID,
		Secs:    0,
		Flags:   req.Flags,
		CIAddr:  req.CIAddr,
		GIAddr:  req.GIAddr,
		CHAddr:  append(net.HardwareAddr(nil), req.CHAddr...),
		Options: make(Options),
		IfIndex: req.IfIndex,
	}
	reply.Options.SetMessageType(msgType)
	return reply
}

// AppendOption appends a DHCP option to a packet
func (p *Packet) AppendOption(code byte, data []byte) {
	p.Options.Set(code, data)
}

func IPToBytes(ip net.IP) []byte {
	v4 := ip.To4()
	if v4 == nil {
		return nil
	}
	return append([]byte(nil), v4...)
}

func IPsToBytes(ips []net.IP) []byte {
	var out []byte
	for _, ip := range ips {
		v4 := ip.To4()
		if v4 != nil {
			out = append(out, v4...)
		}
	}
	return out
}

func Uint32ToBytes(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func Uint16ToBytes(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

func ByteToBytes(v byte) []byte {
	return []byte{v}
}
