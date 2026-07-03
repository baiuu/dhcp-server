package dhcp

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/dhcp-server/dhcp-server/internal/models"
)

// BuildOption converts a typed option value to raw DHCP bytes
func BuildOption(code byte, v models.OptionValue) ([]byte, error) {
	switch v.Type {
	case "ip":
		ip, err := parseIP(v.Value)
		if err != nil {
			return nil, err
		}
		return IPToBytes(ip), nil
	case "ips":
		ips, err := parseIPList(v.Value)
		if err != nil {
			return nil, err
		}
		return IPsToBytes(ips), nil
	case "string":
		s, err := toString(v.Value)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	case "uint8":
		n, err := toUint64(v.Value)
		if err != nil {
			return nil, err
		}
		return []byte{byte(n)}, nil
	case "uint16":
		n, err := toUint64(v.Value)
		if err != nil {
			return nil, err
		}
		b := make([]byte, 2)
		binary.BigEndian.PutUint16(b, uint16(n))
		return b, nil
	case "uint32":
		n, err := toUint64(v.Value)
		if err != nil {
			return nil, err
		}
		return Uint32ToBytes(uint32(n)), nil
	case "int32":
		n, err := toInt64(v.Value)
		if err != nil {
			return nil, err
		}
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(int32(n)))
		return b, nil
	case "hex":
		return parseHex(v.Value)
	case "bool":
		b, err := toBool(v.Value)
		if err != nil {
			return nil, err
		}
		if b {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	case "routes":
		return buildClasslessRoutes(v.Value)
	default:
		return nil, fmt.Errorf("unsupported option type %s", v.Type)
	}
}

// ParseOptionValue parses raw DHCP option bytes into a high-level value.
// It uses the IANA OptionRegistry when available and falls back to hex.
func ParseOptionValue(code byte, data []byte) models.OptionValue {
	if def, ok := OptionRegistry[code]; ok && def.Type != "none" {
		switch def.Type {
		case "ip":
			if len(data) >= 4 {
				return models.OptionValue{Type: "ip", Value: net.IP(append([]byte(nil), data[:4]...)).String()}
			}
		case "ips":
			var ips []string
			for i := 0; i+4 <= len(data); i += 4 {
				ips = append(ips, net.IP(append([]byte(nil), data[i:i+4]...)).String())
			}
			return models.OptionValue{Type: "ips", Value: ips}
		case "uint32":
			if len(data) >= 4 {
				return models.OptionValue{Type: "uint32", Value: binary.BigEndian.Uint32(data[:4])}
			}
		case "int32":
			if len(data) >= 4 {
				return models.OptionValue{Type: "int32", Value: int32(binary.BigEndian.Uint32(data[:4]))}
			}
		case "uint16":
			if len(data) >= 2 {
				return models.OptionValue{Type: "uint16", Value: binary.BigEndian.Uint16(data[:2])}
			}
		case "uint8":
			if len(data) >= 1 {
				return models.OptionValue{Type: "uint8", Value: data[0]}
			}
		case "string":
			return models.OptionValue{Type: "string", Value: string(data)}
		case "routes":
			return models.OptionValue{Type: "routes", Value: parseClasslessRoutes(data)}
		}
	}
	return models.OptionValue{Type: "hex", Value: fmt.Sprintf("%x", data)}
}

func parseClasslessRoutes(data []byte) []map[string]interface{} {
	var routes []map[string]interface{}
	for i := 0; i < len(data); {
		if i+1 > len(data) {
			break
		}
		mask := data[i]
		maskBytes := int((mask + 7) / 8)
		if i+1+maskBytes+4 > len(data) {
			break
		}
		dest := make([]byte, 4)
		copy(dest, data[i+1:i+1+maskBytes])
		router := net.IP(data[i+1+maskBytes : i+1+maskBytes+4])
		routes = append(routes, map[string]interface{}{
			"destination": net.IP(dest).String(),
			"mask":        mask,
			"router":      router.String(),
		})
		i += 1 + maskBytes + 4
	}
	return routes
}

func parseIP(v interface{}) (net.IP, error) {
	s, err := toString(v)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP: %s", s)
	}
	return ip, nil
}

func parseIPList(v interface{}) ([]net.IP, error) {
	switch val := v.(type) {
	case []string:
		var ips []net.IP
		for _, s := range val {
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP: %s", s)
			}
			ips = append(ips, ip)
		}
		return ips, nil
	case []interface{}:
		var ips []net.IP
		for _, item := range val {
			s, err := toString(item)
			if err != nil {
				return nil, err
			}
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP: %s", s)
			}
			ips = append(ips, ip)
		}
		return ips, nil
	case string:
		parts := strings.Split(val, ",")
		var ips []net.IP
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			ip := net.ParseIP(p)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP: %s", p)
			}
			ips = append(ips, ip)
		}
		return ips, nil
	default:
		return nil, fmt.Errorf("invalid IP list type")
	}
}

func parseHex(v interface{}) ([]byte, error) {
	s, err := toString(v)
	if err != nil {
		return nil, err
	}
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ":", "")
	return hexDecodeString(s)
}

func hexDecodeString(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("invalid hex string length")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		b, err := strconv.ParseUint(s[i:i+2], 16, 8)
		if err != nil {
			return nil, err
		}
		out[i/2] = byte(b)
	}
	return out, nil
}

func toString(v interface{}) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	case json.Number:
		return val.String(), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

func toUint64(v interface{}) (uint64, error) {
	switch val := v.(type) {
	case uint64:
		return val, nil
	case uint32:
		return uint64(val), nil
	case uint16:
		return uint64(val), nil
	case uint8:
		return uint64(val), nil
	case int:
		return uint64(val), nil
	case int64:
		return uint64(val), nil
	case float64:
		return uint64(val), nil
	case json.Number:
		i, err := val.Int64()
		if err != nil {
			return 0, err
		}
		return uint64(i), nil
	case string:
		return strconv.ParseUint(val, 10, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to uint64", v)
	}
}

func toInt64(v interface{}) (int64, error) {
	switch val := v.(type) {
	case int64:
		return val, nil
	case int32:
		return int64(val), nil
	case int16:
		return int64(val), nil
	case int8:
		return int64(val), nil
	case int:
		return int64(val), nil
	case uint64:
		return int64(val), nil
	case uint32:
		return int64(val), nil
	case uint16:
		return int64(val), nil
	case uint8:
		return int64(val), nil
	case float64:
		return int64(val), nil
	case json.Number:
		return val.Int64()
	case string:
		return strconv.ParseInt(val, 10, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to int64", v)
	}
}

func toBool(v interface{}) (bool, error) {
	switch val := v.(type) {
	case bool:
		return val, nil
	case string:
		return strconv.ParseBool(val)
	case int:
		return val != 0, nil
	case float64:
		return val != 0, nil
	default:
		return false, fmt.Errorf("cannot convert %T to bool", v)
	}
}

func buildClasslessRoutes(v interface{}) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var routes []struct {
		Destination string `json:"destination"`
		Mask        int    `json:"mask"`
		Router      string `json:"router"`
	}
	if err := json.Unmarshal(data, &routes); err != nil {
		return nil, err
	}
	var out []byte
	for _, r := range routes {
		dest := net.ParseIP(r.Destination).To4()
		router := net.ParseIP(r.Router).To4()
		if dest == nil || router == nil {
			return nil, fmt.Errorf("invalid route %v", r)
		}
		maskBytes := (r.Mask + 7) / 8
		out = append(out, byte(r.Mask))
		out = append(out, dest[:maskBytes]...)
		out = append(out, router...)
	}
	return out, nil
}
