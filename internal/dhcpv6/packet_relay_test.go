package dhcpv6

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

func TestParseRelayPacket(t *testing.T) {
	linkAddr := net.ParseIP("2001:db8:1::1")
	peerAddr := net.ParseIP("fe80::1")
	inner := NewPacket(MsgTypeSolicit, [3]byte{1, 2, 3})
	inner.Options.Add(OptClientID, []byte{0, 1, 2, 3})
	innerData, err := inner.Marshal()
	if err != nil {
		t.Fatalf("marshal inner: %v", err)
	}

	rp := &RelayPacket{
		MessageType: MsgTypeRelayForw,
		HopCount:    1,
		LinkAddr:    linkAddr,
		PeerAddr:    peerAddr,
	}
	rp.Options.Add(OptInterfaceID, []byte{0, 0, 0, 1})
	rp.Options.Add(OptRelayMsg, innerData)

	data, err := rp.Marshal()
	if err != nil {
		t.Fatalf("marshal relay: %v", err)
	}

	parsed, err := ParseRelayPacket(data)
	if err != nil {
		t.Fatalf("parse relay: %v", err)
	}
	if parsed.MessageType != MsgTypeRelayForw {
		t.Fatalf("message type mismatch")
	}
	if parsed.HopCount != 1 {
		t.Fatalf("hop count mismatch")
	}
	if !parsed.LinkAddr.Equal(linkAddr) {
		t.Fatalf("link addr mismatch: %v", parsed.LinkAddr)
	}
	if !parsed.PeerAddr.Equal(peerAddr) {
		t.Fatalf("peer addr mismatch: %v", parsed.PeerAddr)
	}

	ifaceID, ok := parsed.Options.InterfaceID()
	if !ok || !bytes.Equal(ifaceID, []byte{0, 0, 0, 1}) {
		t.Fatalf("interface id mismatch")
	}

	innerPacket, innerRelay, err := parsed.Options.GetRelayMsg()
	if err != nil {
		t.Fatalf("get relay msg: %v", err)
	}
	if innerRelay != nil {
		t.Fatalf("unexpected nested relay")
	}
	if innerPacket == nil {
		t.Fatalf("inner packet nil")
	}
	if innerPacket.MessageType != MsgTypeSolicit {
		t.Fatalf("inner message type mismatch")
	}
	if innerPacket.TransID != [3]byte{1, 2, 3} {
		t.Fatalf("inner trans id mismatch")
	}
	clientID, ok := innerPacket.Options.Get(OptClientID)
	if !ok || !bytes.Equal(clientID.Data, []byte{0, 1, 2, 3}) {
		t.Fatalf("inner client id mismatch")
	}
}

func TestRelayPacketTooShort(t *testing.T) {
	data := make([]byte, 33)
	data[0] = MsgTypeRelayForw
	_, err := ParseRelayPacket(data)
	if err == nil {
		t.Fatalf("expected error for short relay packet")
	}
}

func TestParsePacketRejectsRelay(t *testing.T) {
	data := make([]byte, 34)
	data[0] = MsgTypeRelayForw
	_, err := ParsePacket(data)
	if err == nil {
		t.Fatalf("expected error when parsing relay with ParsePacket")
	}
}

func TestCopyRelayOptions(t *testing.T) {
	var opts Options
	opts.Add(OptInterfaceID, []byte{1, 2, 3})
	opts.Add(OptRelayMsg, []byte{4, 5, 6})

	copied := opts.CopyRelayOptions()
	if len(copied) != 1 {
		t.Fatalf("expected 1 copied option, got %d", len(copied))
	}
	if copied[0].Code != OptInterfaceID {
		t.Fatalf("expected interface id option")
	}
}

func TestBuildRelayReply(t *testing.T) {
	linkAddr := net.ParseIP("2001:db8:1::1")
	peerAddr := net.ParseIP("fe80::1")

	reply := NewPacket(MsgTypeAdvertise, [3]byte{1, 2, 3})
	reply.Options.Add(OptServerID, []byte{9, 8, 7})

	rp := &RelayPacket{
		MessageType: MsgTypeRelayRepl,
		HopCount:    1,
		LinkAddr:    linkAddr,
		PeerAddr:    peerAddr,
	}
	rp.Options.Add(OptInterfaceID, []byte{0, 0, 0, 1})
	replyData, _ := reply.Marshal()
	rp.Options.Add(OptRelayMsg, replyData)

	data, err := rp.Marshal()
	if err != nil {
		t.Fatalf("marshal relay reply: %v", err)
	}

	if data[0] != MsgTypeRelayRepl {
		t.Fatalf("message type mismatch")
	}
	if data[1] != 1 {
		t.Fatalf("hop count mismatch")
	}
	parsedLink := net.IP(data[2:18])
	if !parsedLink.Equal(linkAddr) {
		t.Fatalf("link addr mismatch")
	}

	// Verify options can be parsed back.
	parsed, err := ParseRelayPacket(data)
	if err != nil {
		t.Fatalf("parse relay reply: %v", err)
	}
	inner, _, err := parsed.Options.GetRelayMsg()
	if err != nil {
		t.Fatalf("get relay msg: %v", err)
	}
	if inner.MessageType != MsgTypeAdvertise {
		t.Fatalf("inner reply type mismatch")
	}
}

// Helper to build an option with code and data.
func buildRawOption(code uint16, data []byte) []byte {
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint16(buf[0:2], code)
	binary.BigEndian.PutUint16(buf[2:4], uint16(len(data)))
	copy(buf[4:], data)
	return buf
}
