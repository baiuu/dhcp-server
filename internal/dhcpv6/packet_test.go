package dhcpv6

import (
	"bytes"
	"net"
	"testing"
)

func TestParseAndMarshalV6(t *testing.T) {
	p := NewPacket(MsgTypeSolicit, [3]byte{1, 2, 3})
	p.Options.Add(OptClientID, DUIDLL(net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}))
	p.Options.Add(OptIANA, BuildIANA(1, 0, 0, nil))

	data, err := p.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	parsed, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.MessageType != MsgTypeSolicit {
		t.Errorf("message type mismatch")
	}
	if !bytes.Equal(parsed.TransID[:], []byte{1, 2, 3}) {
		t.Errorf("trans id mismatch")
	}
	if _, ok := parsed.Options.Get(OptClientID); !ok {
		t.Errorf("missing client id")
	}
}

func TestBuildIAPrefix(t *testing.T) {
	prefix := net.ParseIP("2001:db8:ff00::")
	data := BuildIAPrefix(64, prefix, 3600, 7200)
	if len(data) != 25 {
		t.Fatalf("expected 25 bytes, got %d", len(data))
	}
	pl, ip, pref, valid, err := ParseIAPrefix(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pl != 64 {
		t.Errorf("prefix len mismatch")
	}
	if !ip.Equal(prefix) {
		t.Errorf("prefix mismatch")
	}
	if pref != 3600 || valid != 7200 {
		t.Errorf("lifetime mismatch")
	}
}

func TestBuildDomainList(t *testing.T) {
	data := BuildDomainList([]string{"example.com"})
	if len(data) == 0 {
		t.Errorf("empty domain list")
	}
	// 7 (example) + 3 (com) + 2 zeros
	if len(data) != 7+1+3+1+1 {
		t.Errorf("unexpected length %d", len(data))
	}
}

func TestBuildDNSList(t *testing.T) {
	ips := []net.IP{net.ParseIP("2001:db8::53")}
	data := BuildDNSList(ips)
	if len(data) != 16 {
		t.Errorf("expected 16 bytes, got %d", len(data))
	}
}
