package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dhcp-server/dhcp-server/internal/auth"
	"github.com/dhcp-server/dhcp-server/internal/config"
	"github.com/dhcp-server/dhcp-server/internal/db"
	"github.com/dhcp-server/dhcp-server/internal/dhcp"
	"github.com/dhcp-server/dhcp-server/internal/store"
	"github.com/google/uuid"
)

func generateTestKeys(t *testing.T) (privatePath, publicPath string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	privFile, err := os.CreateTemp("", "rsa-priv-*.pem")
	if err != nil {
		t.Fatalf("create temp private key: %v", err)
	}
	defer privFile.Close()
	privBytes := x509.MarshalPKCS1PrivateKey(key)
	if err := pem.Encode(privFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}); err != nil {
		t.Fatalf("encode private key: %v", err)
	}
	pubFile, err := os.CreateTemp("", "rsa-pub-*.pem")
	if err != nil {
		t.Fatalf("create temp public key: %v", err)
	}
	defer pubFile.Close()
	pubBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	if err := pem.Encode(pubFile, &pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}); err != nil {
		t.Fatalf("encode public key: %v", err)
	}
	return privFile.Name(), pubFile.Name()
}

func testAPI(t *testing.T) (*API, func()) {
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
	st := store.New(database.Pool)
	privPath, pubPath := generateTestKeys(t)
	cfg := &config.Config{
		Auth: config.AuthConfig{
			RSAPrivateKeyFile:    privPath,
			RSAPublicKeyFile:     pubPath,
			TokenTTL:             time.Hour,
			DefaultAdminUsername: "admin",
			DefaultAdminPassword: "admin",
		},
	}
	authSvc, err := auth.NewService(cfg, st)
	if err != nil {
		t.Fatalf("init auth: %v", err)
	}
	if err := authSvc.InitializeDefaultAdmin(ctx); err != nil {
		t.Fatalf("init admin: %v", err)
	}
	dhcpSrv := dhcp.NewServer(cfg, st, nil)
	api := New(cfg, st, authSvc, dhcpSrv, nil)
	return api, func() { database.Close() }
}

func TestLoginAndHealth(t *testing.T) {
	api, cleanup := testAPI(t)
	defer cleanup()

	// Health
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	api.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("health status: %d", rr.Code)
	}

	// Login
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin"})
	req = httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	api.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("login status: %d, body: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if resp["token"] == "" {
		t.Errorf("no token")
	}
}

func TestListScopesRequiresAuth(t *testing.T) {
	api, cleanup := testAPI(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/scopes", nil)
	rr := httptest.NewRecorder()
	api.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func getToken(t *testing.T, api *API) string {
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin"})
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	api.Handler().ServeHTTP(rr, req)
	var resp map[string]string
	json.Unmarshal(rr.Body.Bytes(), &resp)
	return resp["token"]
}

func TestCreateAndListScope(t *testing.T) {
	api, cleanup := testAPI(t)
	defer cleanup()
	token := getToken(t, api)

	body, _ := json.Marshal(map[string]interface{}{
		"name":           "api-test-scope-" + uuid.New().String()[:8],
		"subnet":         "192.168.200.0/24",
		"start_ip":       "192.168.200.10",
		"end_ip":         "192.168.200.200",
		"gateway":        []string{"192.168.200.1"},
		"dns":            []string{"8.8.8.8"},
		"lease_time":     3600,
		"max_lease_time": 7200,
		"enabled":        true,
		"options":        map[string]interface{}{},
	})
	req := httptest.NewRequest("POST", "/api/scopes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	api.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("create scope status: %d, body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "api-test-scope") {
		t.Errorf("scope name not in response")
	}

	req = httptest.NewRequest("GET", "/api/scopes", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr = httptest.NewRecorder()
	api.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("list scopes status: %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "api-test-scope") {
		t.Errorf("scope not in list")
	}
}
