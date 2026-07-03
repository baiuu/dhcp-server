package models

import (
	"encoding/json"
	"net"
	"time"
)

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Scope struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	V6           bool            `json:"v6"`
	Subnet       *net.IPNet      `json:"-"`
	Prefix       *net.IPNet      `json:"-"`
	SubnetStr    string          `json:"subnet"`
	PrefixStr    string          `json:"prefix"`
	StartIP      net.IP          `json:"start_ip"`
	EndIP        net.IP          `json:"end_ip"`
	Gateway      []net.IP        `json:"gateway"`
	DNS          []net.IP        `json:"dns"`
	ExcludedIPs  []net.IP        `json:"excluded_ips"`
	DomainName   string          `json:"domain_name"`
	LeaseTime    int             `json:"lease_time"`
	MaxLeaseTime int             `json:"max_lease_time"`
	Enabled      bool            `json:"enabled"`
	Options      json.RawMessage `json:"options"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

func (s *Scope) Normalize() {
	if s.Subnet != nil {
		s.SubnetStr = s.Subnet.String()
	}
	if s.Prefix != nil {
		s.PrefixStr = s.Prefix.String()
	}
}

func (s Scope) MarshalJSON() ([]byte, error) {
	s.Normalize()
	type Alias Scope
	return json.Marshal((*Alias)(&s))
}

func (p V6Prefix) MarshalJSON() ([]byte, error) {
	type Alias V6Prefix
	prefixStr := ""
	if p.Prefix != nil {
		prefixStr = p.Prefix.String()
	}
	return json.Marshal(&struct {
		Prefix string `json:"prefix"`
		*Alias
	}{
		Prefix: prefixStr,
		Alias:  (*Alias)(&p),
	})
}

type Reservation struct {
	ID          string          `json:"id"`
	ScopeID     string          `json:"scope_id"`
	GroupID     string          `json:"group_id"`
	MACAddr     string          `json:"mac_addr"`
	IPAddr      net.IP          `json:"ip_addr"`
	Hostname    string          `json:"hostname"`
	Description string          `json:"description"`
	Options     json.RawMessage `json:"options"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type LeaseState string

const (
	LeaseActive   LeaseState = "active"
	LeaseOffered  LeaseState = "offered"
	LeaseReleased LeaseState = "released"
	LeaseExpired  LeaseState = "expired"
	LeaseDeclined LeaseState = "declined"
)

type Lease struct {
	ID        string     `json:"id"`
	ScopeID   string     `json:"scope_id"`
	MACAddr   string     `json:"mac_addr"`
	IPAddr    net.IP     `json:"ip_addr"`
	Hostname  string     `json:"hostname"`
	ClientID  string     `json:"client_id"`
	State     LeaseState `json:"state"`
	Reserved  bool       `json:"reserved"`
	OfferedAt *time.Time `json:"offered_at,omitempty"`
	StartsAt  time.Time  `json:"starts_at"`
	EndsAt    time.Time  `json:"ends_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type HANode struct {
	ID         string    `json:"id"`
	ClusterID  string    `json:"cluster_id"`
	NodeID     string    `json:"node_id"`
	Role       string    `json:"role"`
	ListenAddr string    `json:"listen_addr"`
	Version    string    `json:"version"`
	Healthy    bool      `json:"healthy"`
	LastSeen   time.Time `json:"last_seen"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type AuditLog struct {
	ID         string          `json:"id"`
	Username   string          `json:"username"`
	Action     string          `json:"action"`
	Resource   string          `json:"resource"`
	ResourceID string          `json:"resource_id"`
	Details    json.RawMessage `json:"details"`
	CreatedAt  time.Time       `json:"created_at"`
}

type IPAllocationLog struct {
	ID          string          `json:"id"`
	NodeID      string          `json:"node_id"`
	ScopeID     string          `json:"scope_id"`
	ScopeName   string          `json:"scope_name"`
	MACAddr     string          `json:"mac_addr"`
	DUID        string          `json:"duid"`
	IAID        string          `json:"iaid"`
	IPAddr      net.IP          `json:"ip_addr"`
	Prefix      *net.IPNet      `json:"prefix"`
	Action      string          `json:"action"`
	ClientInfo  json.RawMessage `json:"client_info"`
	RelayIP     net.IP          `json:"relay_ip"`
	CreatedAt   time.Time       `json:"created_at"`
}

type SystemLog struct {
	ID        string          `json:"id"`
	NodeID    string          `json:"node_id"`
	Level     string          `json:"level"`
	Message   string          `json:"message"`
	Attrs     json.RawMessage `json:"attrs"`
	CreatedAt time.Time       `json:"created_at"`
}

type V6Lease struct {
	ID        string     `json:"id"`
	ScopeID   string     `json:"scope_id"`
	DUID      string     `json:"duid"`
	IAID      string     `json:"iaid"`
	IPAddr    net.IP     `json:"ip_addr"`
	Hostname  string     `json:"hostname"`
	State     LeaseState `json:"state"`
	Reserved  bool       `json:"reserved"`
	OfferedAt *time.Time `json:"offered_at,omitempty"`
	StartsAt  time.Time  `json:"starts_at"`
	EndsAt    time.Time  `json:"ends_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type V6Reservation struct {
	ID          string          `json:"id"`
	ScopeID     string          `json:"scope_id"`
	GroupID     string          `json:"group_id"`
	DUID        string          `json:"duid"`
	IPAddr      net.IP          `json:"ip_addr"`
	Hostname    string          `json:"hostname"`
	Description string          `json:"description"`
	Options     json.RawMessage `json:"options"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type V6Prefix struct {
	ID                string     `json:"id"`
	ScopeID           string     `json:"scope_id"`
	DUID              string     `json:"duid"`
	IAID              string     `json:"iaid"`
	Prefix            *net.IPNet `json:"prefix"`
	PreferredLifetime int        `json:"preferred_lifetime"`
	ValidLifetime     int        `json:"valid_lifetime"`
	State             LeaseState `json:"state"`
	StartsAt          time.Time  `json:"starts_at"`
	EndsAt            time.Time  `json:"ends_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type ReservationGroup struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Options     json.RawMessage `json:"options"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type MACBlacklist struct {
	ID        string    `json:"id"`
	MACAddr   string    `json:"mac_addr"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// OptionMap 用于统一表示 options JSONB 字段
type OptionMap map[string]interface{}

// OptionValue 用于表示一个 DHCP Option 的值
type OptionValue struct {
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

func (o OptionMap) ToRawMessage() json.RawMessage {
	b, _ := json.Marshal(o)
	return b
}

func ParseOptionMap(b json.RawMessage) OptionMap {
	var m OptionMap
	if len(b) == 0 {
		return OptionMap{}
	}
	_ = json.Unmarshal(b, &m)
	if m == nil {
		return OptionMap{}
	}
	return m
}
