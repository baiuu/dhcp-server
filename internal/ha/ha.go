package ha

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/dhcp-server/dhcp-server/internal/buildinfo"
	"github.com/dhcp-server/dhcp-server/internal/config"
	"github.com/dhcp-server/dhcp-server/internal/models"
	"github.com/dhcp-server/dhcp-server/internal/store"
	"github.com/google/uuid"
)

const RoleActive = "active"

type Manager struct {
	cfg   *config.Config
	store *store.Store
}

func NewManager(cfg *config.Config, store *store.Store) *Manager {
	return &Manager{cfg: cfg, store: store}
}

func (m *Manager) Run(ctx context.Context) {
	if !m.cfg.Cluster.Enabled {
		return
	}
	// Register immediately so the node appears in the cluster view right away.
	m.heartbeat(ctx)

	ticker := time.NewTicker(m.cfg.Cluster.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.heartbeat(ctx)
		}
	}
}

func (m *Manager) heartbeat(ctx context.Context) {
	node := &models.HANode{
		ID:         uuid.New().String(),
		ClusterID:  m.cfg.Cluster.ClusterID,
		NodeID:     m.cfg.Cluster.NodeID,
		Role:       RoleActive,
		ListenAddr: m.cfg.Cluster.ListenAddr,
		Version:    buildinfo.Version,
		Healthy:    true,
		LastSeen:   time.Now().UTC(),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	_ = m.store.UpsertHANode(ctx, node)
}

// IsHealthy always returns true for an active/active cluster. In the future it
// can be extended to pause local request processing when the node detects it is
// partitioned from the database or its peers.
func (m *Manager) IsHealthy() bool {
	return true
}

func (m *Manager) HealthCheck(addr string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: %d", resp.StatusCode)
	}
	return nil
}
