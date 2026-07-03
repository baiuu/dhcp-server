package store

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/dhcp-server/dhcp-server/internal/db"
	"github.com/dhcp-server/dhcp-server/internal/models"
	"github.com/google/uuid"
)

func testDB(t *testing.T) (*Store, func()) {
	ctx := context.Background()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://dhcp:dhcp@localhost:5432/dhcpdb_test?sslmode=disable"
	}
	database, err := db.New(ctx, databaseURL, 5)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := New(database.Pool)
	return store, func() { database.Close() }
}

func TestScopeCRUD(t *testing.T) {
	ctx := context.Background()
	store, cleanup := testDB(t)
	defer cleanup()

	_, ipnet, _ := net.ParseCIDR("10.0.0.0/24")
	scope := &models.Scope{
		ID:           uuid.New().String(),
		Name:         "test-scope-" + uuid.New().String()[:8],
		Subnet:       ipnet,
		StartIP:      net.ParseIP("10.0.0.10"),
		EndIP:        net.ParseIP("10.0.0.100"),
		Gateway:      []net.IP{net.ParseIP("10.0.0.1")},
		DNS:          []net.IP{net.ParseIP("8.8.8.8")},
		LeaseTime:    3600,
		MaxLeaseTime: 7200,
		Enabled:      true,
		Options:      models.OptionMap{}.ToRawMessage(),
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if err := store.CreateScope(ctx, scope); err != nil {
		t.Fatalf("create scope: %v", err)
	}

	got, err := store.GetScopeByID(ctx, scope.ID)
	if err != nil {
		t.Fatalf("get scope: %v", err)
	}
	if got.Name != scope.Name {
		t.Errorf("name mismatch")
	}

	scopes, err := store.ListScopes(ctx)
	if err != nil {
		t.Fatalf("list scopes: %v", err)
	}
	found := false
	for _, s := range scopes {
		if s.ID == scope.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("scope not in list")
	}

	if err := store.DeleteScope(ctx, scope.ID); err != nil {
		t.Fatalf("delete scope: %v", err)
	}
}

func TestLeaseCRUD(t *testing.T) {
	ctx := context.Background()
	store, cleanup := testDB(t)
	defer cleanup()

	_, ipnet, _ := net.ParseCIDR("10.1.0.0/24")
	scope := &models.Scope{
		ID:           uuid.New().String(),
		Name:         "test-lease-scope-" + uuid.New().String()[:8],
		Subnet:       ipnet,
		StartIP:      net.ParseIP("10.1.0.10"),
		EndIP:        net.ParseIP("10.1.0.100"),
		LeaseTime:    3600,
		MaxLeaseTime: 7200,
		Enabled:      true,
		Options:      models.OptionMap{}.ToRawMessage(),
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := store.CreateScope(ctx, scope); err != nil {
		t.Fatalf("create scope: %v", err)
	}

	lease := &models.Lease{
		ID:        uuid.New().String(),
		ScopeID:   scope.ID,
		MACAddr:   "00:11:22:33:44:55",
		IPAddr:    net.ParseIP("10.1.0.50"),
		State:     models.LeaseActive,
		StartsAt:  time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(time.Hour),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreateOrUpdateLease(ctx, lease); err != nil {
		t.Fatalf("create lease: %v", err)
	}

	got, err := store.GetLeaseByMAC(ctx, scope.ID, lease.MACAddr)
	if err != nil {
		t.Fatalf("get lease: %v", err)
	}
	if got.IPAddr.String() != lease.IPAddr.String() {
		t.Errorf("ip mismatch")
	}

	if err := store.UpdateLeaseState(ctx, lease.ID, models.LeaseReleased); err != nil {
		t.Fatalf("update state: %v", err)
	}
	got, _ = store.GetLeaseByMAC(ctx, scope.ID, lease.MACAddr)
	if got.State != models.LeaseReleased {
		t.Errorf("state not released")
	}
}
