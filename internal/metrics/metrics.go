package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	DHCPPacketsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dhcp_packets_total",
		Help: "Total DHCP packets processed",
	}, []string{"type", "message_type"})

	DHCPRepliesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dhcp_replies_total",
		Help: "Total DHCP replies sent",
	}, []string{"message_type"})

	LeasesActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dhcp_leases_active",
		Help: "Current active/offered DHCP leases",
	}, []string{"version"})

	LeasesReleased = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dhcp_leases_released_total",
		Help: "Total released DHCP leases",
	})

	LeasesDeclined = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dhcp_leases_declined_total",
		Help: "Total declined DHCP leases",
	})

	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dhcp_http_requests_total",
		Help: "Total HTTP API requests",
	}, []string{"method", "path", "status"})

	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "dhcp_http_request_duration_seconds",
		Help:    "HTTP request duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
)
