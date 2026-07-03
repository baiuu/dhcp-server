package dhcp

import (
	"bytes"
	"net"
	"testing"
)

func TestParseAndMarshal(t *testing.T) {
	p := &Packet{
		Op:     BootRequest,
		HType:  1,
		HLen:   6,
		XID:    0x12345678,
		CHAddr: net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		Options: Options{
			OptDHCPMessageType: []byte{DHCPDiscover},
			OptHostName:        []byte("testhost"),
		},
	}
	data, err := p.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	parsed, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Op != p.Op {
		t.Errorf("op mismatch")
	}
	if parsed.XID != p.XID {
		t.Errorf("xid mismatch")
	}
	if !bytes.Equal(parsed.CHAddr, p.CHAddr) {
		t.Errorf("chaddr mismatch")
	}
	if parsed.Options.MessageType() != DHCPDiscover {
		t.Errorf("message type mismatch")
	}
	if parsed.Options.HostName() != "testhost" {
		t.Errorf("hostname mismatch")
	}
}

func TestReplyFromRequest(t *testing.T) {
	req := &Packet{
		Op:     BootRequest,
		HType:  1,
		HLen:   6,
		XID:    0xabcdef01,
		CHAddr: net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		Options: Options{
			OptDHCPMessageType: []byte{DHCPDiscover},
		},
	}
	reply := ReplyFromRequest(req, DHCPOffer)
	if reply.Op != BootReply {
		t.Errorf("reply op should be BootReply")
	}
	if reply.XID != req.XID {
		t.Errorf("reply xid mismatch")
	}
	if reply.Options.MessageType() != DHCPOffer {
		t.Errorf("reply type mismatch")
	}
}

func TestRequestedIP(t *testing.T) {
	ip := net.IPv4(192, 168, 1, 50)
	opts := Options{OptRequestedIPAddr: IPToBytes(ip)}
	got := opts.RequestedIP()
	if got == nil || !got.Equal(ip) {
		t.Errorf("requested ip mismatch")
	}
}
