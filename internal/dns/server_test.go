package dns

import (
	"encoding/binary"
	"net"
	"testing"
)

func buildQuery(t *testing.T, id uint16, name string, qtype uint16) []byte {
	t.Helper()
	out := make([]byte, 12)
	binary.BigEndian.PutUint16(out[0:2], id)
	binary.BigEndian.PutUint16(out[2:4], 0x0100) // RD
	binary.BigEndian.PutUint16(out[4:6], 1)      // QDCOUNT
	out = append(out, encodeName(name)...)
	var tmp [4]byte
	binary.BigEndian.PutUint16(tmp[0:2], qtype)
	binary.BigEndian.PutUint16(tmp[2:4], 1) // IN
	return append(out, tmp[:]...)
}

func TestParseQuery(t *testing.T) {
	data := buildQuery(t, 0x1234, "pc-01.lan", qtypeA)
	q, err := parseQuery(data)
	if err != nil {
		t.Fatalf("parseQuery: %v", err)
	}
	if q.id != 0x1234 {
		t.Errorf("id = %#x, want 0x1234", q.id)
	}
	if q.name != "pc-01.lan" {
		t.Errorf("name = %q", q.name)
	}
	if q.qtype != qtypeA {
		t.Errorf("qtype = %d", q.qtype)
	}
	// questionBytes must round-trip the original question section
	if len(q.questionBytes) != len(data)-12 {
		t.Errorf("questionBytes len = %d, want %d", len(q.questionBytes), len(data)-12)
	}
}

func TestParseQueryTooShort(t *testing.T) {
	if _, err := parseQuery([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error for short message")
	}
}

func TestEncodeDecodeNameRoundTrip(t *testing.T) {
	names := []string{"pc-01", "a.b.c", "very-long-hostname-example.lan"}
	for _, n := range names {
		enc := encodeName(n)
		dec, next, err := decodeName(enc, 0)
		if err != nil {
			t.Fatalf("decodeName(%s): %v", n, err)
		}
		if dec != n {
			t.Errorf("round trip: got %q want %q", dec, n)
		}
		if next != len(enc) {
			t.Errorf("next = %d, want %d", next, len(enc))
		}
	}
}

func TestParsePTRNameV4(t *testing.T) {
	ip, ok := parsePTRName("1.2.168.192.in-addr.arpa")
	if !ok {
		t.Fatal("parsePTRName v4 failed")
	}
	if !ip.Equal(net.ParseIP("192.168.2.1")) {
		t.Errorf("got %s, want 192.168.2.1", ip)
	}
}

func TestParsePTRNameV6(t *testing.T) {
	// 2001:db8::1
	ip, ok := parsePTRName("1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.ip6.arpa")
	if !ok {
		t.Fatal("parsePTRName v6 failed")
	}
	want := net.ParseIP("2001:db8::1")
	if !ip.Equal(want) {
		t.Errorf("got %s, want %s", ip, want)
	}
}

func TestParsePTRNameInvalid(t *testing.T) {
	for _, name := range []string{
		"1.2.3.in-addr.arpa",
		"1.2.3.999.in-addr.arpa",
		"a.b.c.ip6.arpa",
		"example.com",
	} {
		if _, ok := parsePTRName(name); ok {
			t.Errorf("parsePTRName(%q) should fail", name)
		}
	}
}

func TestBuildResponse(t *testing.T) {
	q, err := parseQuery(buildQuery(t, 0xABCD, "host.lan", qtypeA))
	if err != nil {
		t.Fatalf("parseQuery: %v", err)
	}
	rr := rrAddress(qtypeA, 300, net.ParseIP("192.168.1.10").To4())
	resp := buildResponse(q, [][]byte{rr}, rcodeNoError)

	if len(resp) < 12 {
		t.Fatal("response too short")
	}
	if got := binary.BigEndian.Uint16(resp[0:2]); got != 0xABCD {
		t.Errorf("id = %#x", got)
	}
	flags := binary.BigEndian.Uint16(resp[2:4])
	if flags&0x8000 == 0 {
		t.Error("QR bit not set")
	}
	if flags&0x0400 == 0 {
		t.Error("AA bit not set")
	}
	if flags&0x0100 == 0 {
		t.Error("RD bit not echoed")
	}
	if got := binary.BigEndian.Uint16(resp[6:8]); got != 1 {
		t.Errorf("ANCOUNT = %d, want 1", got)
	}
	// The answer should end with the IPv4 address.
	tail := resp[len(resp)-4:]
	if !net.IP(tail).Equal(net.ParseIP("192.168.1.10").To4()) {
		t.Errorf("rdata = %v", tail)
	}
}

func TestBuildResponseNXDOMAIN(t *testing.T) {
	q, err := parseQuery(buildQuery(t, 1, "missing.lan", qtypeA))
	if err != nil {
		t.Fatalf("parseQuery: %v", err)
	}
	resp := buildResponse(q, nil, rcodeNXDomain)
	flags := binary.BigEndian.Uint16(resp[2:4])
	if flags&0x000F != rcodeNXDomain {
		t.Errorf("rcode = %d, want %d", flags&0x000F, rcodeNXDomain)
	}
	if got := binary.BigEndian.Uint16(resp[6:8]); got != 0 {
		t.Errorf("ANCOUNT = %d, want 0", got)
	}
}

func TestRRPTR(t *testing.T) {
	rr := rrPTR(300, "pc-01.lan.")
	want := encodeName("pc-01.lan.")
	tail := rr[len(rr)-len(want):]
	if string(tail) != string(want) {
		t.Errorf("ptr target mismatch")
	}
}

func TestSplitQuery(t *testing.T) {
	cases := []struct {
		name, host, domain string
	}{
		{"pc-01.kfb.com", "pc-01", "kfb.com"},
		{"pc-01", "pc-01", ""},
		{"a.b.c.d", "a", "b.c.d"},
	}
	for _, c := range cases {
		host, domain := splitQuery(c.name)
		if host != c.host || domain != c.domain {
			t.Errorf("splitQuery(%q) = (%q, %q), want (%q, %q)", c.name, host, domain, c.host, c.domain)
		}
	}
}

func TestPtrFQDN(t *testing.T) {
	if got := ptrFQDN("pc-01", "kfb.com"); got != "pc-01.kfb.com." {
		t.Errorf("ptrFQDN with domain = %q", got)
	}
	if got := ptrFQDN("pc-01", ""); got != "pc-01." {
		t.Errorf("ptrFQDN without domain = %q", got)
	}
}
