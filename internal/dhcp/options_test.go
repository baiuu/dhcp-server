package dhcp

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/dhcp-server/dhcp-server/internal/models"
)

func TestBuildOptionIP(t *testing.T) {
	b, err := BuildOption(OptRouter, models.OptionValue{Type: "ips", Value: []string{"192.168.1.1", "192.168.1.2"}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(b) != 8 {
		t.Errorf("expected 8 bytes, got %d", len(b))
	}
	ips := []net.IP{net.IPv4(192, 168, 1, 1), net.IPv4(192, 168, 1, 2)}
	for i, ip := range ips {
		if !net.IP(b[i*4 : (i+1)*4]).Equal(ip) {
			t.Errorf("ip mismatch at %d", i)
		}
	}
}

func TestBuildOptionUint32(t *testing.T) {
	b, err := BuildOption(OptIPAddressLeaseTime, models.OptionValue{Type: "uint32", Value: 3600})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(b) != 4 {
		t.Errorf("expected 4 bytes, got %d", len(b))
	}
	if binary.BigEndian.Uint32(b) != 3600 {
		t.Errorf("value mismatch")
	}
}

func TestBuildOptionString(t *testing.T) {
	b, err := BuildOption(OptDomainName, models.OptionValue{Type: "string", Value: "example.com"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if string(b) != "example.com" {
		t.Errorf("string mismatch")
	}
}

func TestParseOptionValue(t *testing.T) {
	v := ParseOptionValue(OptDomainNameServer, []byte{8, 8, 8, 8, 8, 8, 4, 4})
	if v.Type != "ips" {
		t.Errorf("expected ips type, got %s", v.Type)
	}
}

func TestOptionRoundTrip(t *testing.T) {
	raw := []byte{255, 255, 255, 0}
	v := ParseOptionValue(OptSubnetMask, raw)
	b, err := BuildOption(OptSubnetMask, v)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !net.IP(b).Equal(net.IPv4(255, 255, 255, 0)) {
		t.Errorf("roundtrip mismatch")
	}
}

func TestBuildOptionRegistryCodes(t *testing.T) {
	// Boot File Size (code 13) - uint16
	b, err := BuildOption(13, models.OptionValue{Type: "uint16", Value: 64})
	if err != nil {
		t.Fatalf("build boot file size: %v", err)
	}
	if len(b) != 2 || binary.BigEndian.Uint16(b) != 64 {
		t.Errorf("boot file size bytes mismatch: %v", b)
	}

	// MTU Plateau (code 25) - hex table of uint16
	b, err = BuildOption(25, models.OptionValue{Type: "hex", Value: "05dc057805b4"})
	if err != nil {
		t.Fatalf("build mtu plateau: %v", err)
	}
	if len(b) != 6 {
		t.Errorf("mtu plateau length mismatch: %d", len(b))
	}
}
