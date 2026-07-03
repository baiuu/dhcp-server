package buildinfo

// Version is set at build time via -ldflags.
// Example:
//
//	go build -ldflags "-X github.com/dhcp-server/dhcp-server/internal/buildinfo.Version=v1.2.3" .
var Version = "dev"

// BuildTime is set at build time via -ldflags.
var BuildTime = "unknown"
