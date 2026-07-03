package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/dhcp-server/dhcp-server/internal/auth"
	"github.com/dhcp-server/dhcp-server/internal/config"
	"github.com/dhcp-server/dhcp-server/internal/dhcp"
	"github.com/dhcp-server/dhcp-server/internal/models"
	"github.com/dhcp-server/dhcp-server/internal/store"
	"github.com/dhcp-server/dhcp-server/internal/web"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var webFS, _ = fs.Sub(web.FS, "dist")

type API struct {
	cfg    *config.Config
	store  *store.Store
	auth   *auth.Service
	dhcp   *dhcp.Server
	logger *slog.Logger
	router *mux.Router
}

func New(cfg *config.Config, store *store.Store, authSvc *auth.Service, dhcpSrv *dhcp.Server, logger *slog.Logger) *API {
	a := &API{
		cfg:    cfg,
		store:  store,
		auth:   authSvc,
		dhcp:   dhcpSrv,
		logger: logger,
		router: mux.NewRouter(),
	}
	a.registerRoutes()
	return a
}

func (a *API) Handler() http.Handler {
	return a.metricsMiddleware(a.corsMiddleware(a.router))
}

func (a *API) registerRoutes() {
	// Public
	a.router.HandleFunc("/api/auth/login", a.handleLogin).Methods("POST", "OPTIONS")
	a.router.HandleFunc("/health", a.handleHealth).Methods("GET")
	a.router.Handle("/metrics", promhttp.Handler())

	// Protected API
	api := a.router.PathPrefix("/api").Subrouter()
	api.Use(a.authMiddleware)
	api.Use(a.readWriteOnly)

	api.HandleFunc("/dashboard", a.handleDashboard).Methods("GET")

	// Scopes
	api.HandleFunc("/scopes", a.handleListScopes).Methods("GET")
	api.HandleFunc("/scopes", a.handleCreateScope).Methods("POST")
	api.HandleFunc("/scopes/{id}", a.handleGetScope).Methods("GET")
	api.HandleFunc("/scopes/{id}", a.handleUpdateScope).Methods("PUT")
	api.HandleFunc("/scopes/{id}", a.handleDeleteScope).Methods("DELETE")

	// Reservations
	api.HandleFunc("/scopes/{id}/reservations", a.handleListReservations).Methods("GET")
	api.HandleFunc("/scopes/{id}/reservations", a.handleCreateReservation).Methods("POST")
	api.HandleFunc("/reservations/{id}", a.handleUpdateReservation).Methods("PUT")
	api.HandleFunc("/reservations/{id}", a.handleDeleteReservation).Methods("DELETE")
	api.HandleFunc("/v6-reservations/{id}", a.handleUpdateV6Reservation).Methods("PUT")
	api.HandleFunc("/v6-reservations/{id}", a.handleDeleteV6Reservation).Methods("DELETE")

	// Reservation Groups
	api.HandleFunc("/reservation-groups", a.handleListReservationGroups).Methods("GET")
	api.HandleFunc("/reservation-groups", a.handleCreateReservationGroup).Methods("POST")
	api.HandleFunc("/reservation-groups/{id}", a.handleUpdateReservationGroup).Methods("PUT")
	api.HandleFunc("/reservation-groups/{id}", a.handleDeleteReservationGroup).Methods("DELETE")

	// Leases
	api.HandleFunc("/scopes/{id}/leases", a.handleListLeases).Methods("GET")
	api.HandleFunc("/leases/{id}/release", a.handleReleaseLease).Methods("POST")
	api.HandleFunc("/leases/{id}", a.handleDeleteLease).Methods("DELETE")
	api.HandleFunc("/v6-leases/{id}/release", a.handleReleaseV6Lease).Methods("POST")
	api.HandleFunc("/v6-leases/{id}", a.handleDeleteV6Lease).Methods("DELETE")
	api.HandleFunc("/leases/search", a.handleSearchLeases).Methods("GET")

	// Users
	api.HandleFunc("/users", a.handleListUsers).Methods("GET")
	api.HandleFunc("/users", a.handleCreateUser).Methods("POST")
	api.HandleFunc("/users/{id}", a.handleUpdateUser).Methods("PUT")
	api.HandleFunc("/users/{id}", a.handleDeleteUser).Methods("DELETE")
	api.HandleFunc("/users/change-password", a.handleChangePassword).Methods("POST")

	// Audit logs
	api.HandleFunc("/audit-logs", a.handleListAuditLogs).Methods("GET")

	// IP allocation logs
	api.HandleFunc("/ip-allocation-logs", a.handleListIPAllocationLogs).Methods("GET")

	// System logs
	api.HandleFunc("/system-logs", a.handleListSystemLogs).Methods("GET")

	// MAC Blacklist
	api.HandleFunc("/mac-blacklist", a.handleListMACBlacklist).Methods("GET")
	api.HandleFunc("/mac-blacklist", a.handleCreateMACBlacklist).Methods("POST")
	api.HandleFunc("/mac-blacklist/{id}", a.handleDeleteMACBlacklist).Methods("DELETE")

	// Cluster
	api.HandleFunc("/cluster/nodes", a.handleListClusterNodes).Methods("GET")

	// Static web UI (must be last to avoid shadowing API routes)
	a.router.PathPrefix("/").Handler(http.FileServer(http.FS(webFS)))
}

func (a *API) jsonError(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (a *API) jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func pagination(r *http.Request) (offset, limit, page, pageSize int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ = strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 {
		if l, _ := strconv.Atoi(r.URL.Query().Get("limit")); l > 0 {
			pageSize = l
		} else {
			pageSize = 20
		}
	}
	maxPageSize := 200
	if r.URL.Query().Has("limit") && pageSize > maxPageSize {
		maxPageSize = 10000
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	offset = (page - 1) * pageSize
	limit = pageSize
	return
}

func pageResponse(items interface{}, total int64, page, pageSize int) map[string]interface{} {
	return map[string]interface{}{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}
}

func (a *API) audit(r *http.Request, action, resource, resourceID string, details interface{}) {
	user, _ := r.Context().Value(auth.ContextUserKey).(*models.User)
	username := ""
	if user != nil {
		username = user.Username
	}
	b, _ := json.Marshal(details)
	_ = a.store.CreateAuditLog(r.Context(), &models.AuditLog{
		ID:         uuid.New().String(),
		Username:   username,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    b,
		CreatedAt:  time.Now().UTC(),
	})
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"status":     "ok",
		"node_id":    a.cfg.Cluster.NodeID,
		"cluster_id": a.cfg.Cluster.ClusterID,
		"listen":     a.cfg.Server.Listen,
		"v6_listen":  a.cfg.Server.V6Listen,
	}
	a.jsonOK(w, resp)
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	token, role, err := a.auth.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		a.audit(r, "login_failed", "user", "", map[string]string{"username": req.Username})
		a.jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}
	a.audit(r, "login", "user", "", map[string]string{"username": req.Username})
	a.jsonOK(w, map[string]string{"token": token, "role": role})
}

func (a *API) handleDashboard(w http.ResponseWriter, r *http.Request) {
	scopes, err := a.store.ListScopes(r.Context())
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	leases, err := a.store.ListActiveLeases(r.Context())
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.jsonOK(w, map[string]interface{}{
		"scopes":        scopes,
		"active_leases": leases,
		"lease_count":   len(leases),
	})
}

func (a *API) handleListScopes(w http.ResponseWriter, r *http.Request) {
	offset, limit, page, pageSize := pagination(r)
	var v6 *bool
	if r.URL.Query().Has("v6") {
		b := r.URL.Query().Get("v6") == "true"
		v6 = &b
	}
	scopes, total, err := a.store.ListScopesPaged(r.Context(), v6, offset, limit)
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.jsonOK(w, pageResponse(scopes, total, page, pageSize))
}

func (a *API) handleGetScope(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	scope, err := a.store.GetScopeByID(r.Context(), id)
	if err != nil {
		a.jsonError(w, http.StatusNotFound, "scope not found")
		return
	}
	a.jsonOK(w, scope)
}

func (a *API) handleCreateScope(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string                 `json:"name"`
		V6           bool                   `json:"v6"`
		Subnet       string                 `json:"subnet"`
		Prefix       string                 `json:"prefix"`
		StartIP      string                 `json:"start_ip"`
		EndIP        string                 `json:"end_ip"`
		Gateway      []string               `json:"gateway"`
		DNS          []string               `json:"dns"`
		ExcludedIPs  []string               `json:"excluded_ips"`
		DomainName   string                 `json:"domain_name"`
		LeaseTime    int                    `json:"lease_time"`
		MaxLeaseTime int                    `json:"max_lease_time"`
		Enabled      bool                   `json:"enabled"`
		Options      map[string]interface{} `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	scope, err := scopeFromRequest(&req)
	if err != nil {
		a.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	scope.ID = uuid.New().String()
	scope.CreatedAt = time.Now().UTC()
	scope.UpdatedAt = scope.CreatedAt
	if err := a.store.CreateScope(r.Context(), scope); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "create", "scope", scope.ID, scope)
	a.jsonOK(w, scope)
}

func (a *API) handleUpdateScope(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	existing, err := a.store.GetScopeByID(r.Context(), id)
	if err != nil {
		a.jsonError(w, http.StatusNotFound, "scope not found")
		return
	}
	var req struct {
		Name         string                 `json:"name"`
		V6           bool                   `json:"v6"`
		Subnet       string                 `json:"subnet"`
		Prefix       string                 `json:"prefix"`
		StartIP      string                 `json:"start_ip"`
		EndIP        string                 `json:"end_ip"`
		Gateway      []string               `json:"gateway"`
		DNS          []string               `json:"dns"`
		ExcludedIPs  []string               `json:"excluded_ips"`
		DomainName   string                 `json:"domain_name"`
		LeaseTime    int                    `json:"lease_time"`
		MaxLeaseTime int                    `json:"max_lease_time"`
		Enabled      bool                   `json:"enabled"`
		Options      map[string]interface{} `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	scope, err := scopeFromRequest(&req)
	if err != nil {
		a.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	scope.ID = existing.ID
	scope.CreatedAt = existing.CreatedAt
	scope.UpdatedAt = time.Now().UTC()
	if err := a.store.UpdateScope(r.Context(), scope); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "update", "scope", scope.ID, scope)
	a.jsonOK(w, scope)
}

func (a *API) handleDeleteScope(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := a.store.DeleteScope(r.Context(), id); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "delete", "scope", id, nil)
	a.jsonOK(w, map[string]string{"status": "deleted"})
}

func scopeFromRequest(req *struct {
	Name         string                 `json:"name"`
	V6           bool                   `json:"v6"`
	Subnet       string                 `json:"subnet"`
	Prefix       string                 `json:"prefix"`
	StartIP      string                 `json:"start_ip"`
	EndIP        string                 `json:"end_ip"`
	Gateway      []string               `json:"gateway"`
	DNS          []string               `json:"dns"`
	ExcludedIPs  []string               `json:"excluded_ips"`
	DomainName   string                 `json:"domain_name"`
	LeaseTime    int                    `json:"lease_time"`
	MaxLeaseTime int                    `json:"max_lease_time"`
	Enabled      bool                   `json:"enabled"`
	Options      map[string]interface{} `json:"options"`
}) (*models.Scope, error) {
	_, ipnet, err := net.ParseCIDR(req.Subnet)
	if err != nil {
		return nil, fmt.Errorf("invalid subnet: %w", err)
	}
	startIP := net.ParseIP(req.StartIP)
	endIP := net.ParseIP(req.EndIP)
	if startIP == nil || endIP == nil {
		return nil, fmt.Errorf("invalid start or end ip")
	}
	var gw, dns []net.IP
	for _, s := range req.Gateway {
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("invalid gateway: %s", s)
		}
		gw = append(gw, ip)
	}
	for _, s := range req.DNS {
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("invalid dns: %s", s)
		}
		dns = append(dns, ip)
	}
	var excluded []net.IP
	for _, s := range req.ExcludedIPs {
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("invalid excluded ip: %s", s)
		}
		excluded = append(excluded, ip)
	}
	if excluded == nil {
		excluded = []net.IP{}
	}
	if req.LeaseTime == 0 {
		req.LeaseTime = 3600
	}
	if req.MaxLeaseTime == 0 {
		req.MaxLeaseTime = 86400
	}
	var prefix *net.IPNet
	if req.Prefix != "" {
		_, pnet, err := net.ParseCIDR(req.Prefix)
		if err != nil {
			return nil, fmt.Errorf("invalid prefix: %w", err)
		}
		prefix = pnet
	}
	opts, _ := json.Marshal(req.Options)
	return &models.Scope{
		Name:         req.Name,
		V6:           req.V6,
		Subnet:       ipnet,
		Prefix:       prefix,
		StartIP:      startIP,
		EndIP:        endIP,
		Gateway:      gw,
		DNS:          dns,
		ExcludedIPs:  excluded,
		DomainName:   req.DomainName,
		LeaseTime:    req.LeaseTime,
		MaxLeaseTime: req.MaxLeaseTime,
		Enabled:      req.Enabled,
		Options:      opts,
	}, nil
}

func (a *API) handleListReservations(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	scope, err := a.store.GetScopeByID(r.Context(), id)
	if err != nil {
		a.jsonError(w, http.StatusNotFound, "scope not found")
		return
	}
	offset, limit, page, pageSize := pagination(r)
	if scope.V6 {
		res, total, err := a.store.ListV6ReservationsByScopePaged(r.Context(), id, offset, limit)
		if err != nil {
			a.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.jsonOK(w, pageResponse(res, total, page, pageSize))
		return
	}
	res, total, err := a.store.ListReservationsByScopePaged(r.Context(), id, offset, limit)
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.jsonOK(w, pageResponse(res, total, page, pageSize))
}

func ipKey(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func ipInRange(ip, start, end net.IP) bool {
	ipU := uint32(ip.To4()[0])<<24 | uint32(ip.To4()[1])<<16 | uint32(ip.To4()[2])<<8 | uint32(ip.To4()[3])
	startU := uint32(start.To4()[0])<<24 | uint32(start.To4()[1])<<16 | uint32(start.To4()[2])<<8 | uint32(start.To4()[3])
	endU := uint32(end.To4()[0])<<24 | uint32(end.To4()[1])<<16 | uint32(end.To4()[2])<<8 | uint32(end.To4()[3])
	return ipU >= startU && ipU <= endU
}

func (a *API) checkV4ReservationConflict(ctx context.Context, scope *models.Scope, mac string, ip net.IP, excludeID string) error {
	if existing, _ := a.store.GetReservationByIP(ctx, scope.ID, ip); existing != nil && existing.ID != excludeID {
		return fmt.Errorf("IP %s 已被 MAC %s 绑定", ip, existing.MACAddr)
	}
	if existing, _ := a.store.GetReservationByMAC(ctx, scope.ID, mac); existing != nil && existing.ID != excludeID {
		return fmt.Errorf("MAC %s 已绑定 IP %s", mac, existing.IPAddr)
	}
	if lease, _ := a.store.GetLeaseByIP(ctx, scope.ID, ip); lease != nil && (lease.State == models.LeaseActive || lease.State == models.LeaseOffered) {
		if lease.MACAddr != mac {
			return fmt.Errorf("IP %s 当前已被租约占用（MAC %s）", ip, lease.MACAddr)
		}
	}
	if !ipInRange(ip, scope.StartIP, scope.EndIP) {
		return fmt.Errorf("IP %s 不在作用域范围内", ip)
	}
	for _, ex := range scope.ExcludedIPs {
		if ipKey(ip) == ipKey(ex) {
			return fmt.Errorf("IP %s 在排除列表中", ip)
		}
	}
	return nil
}

func (a *API) checkV6ReservationConflict(ctx context.Context, scope *models.Scope, duid string, ip net.IP, excludeID string) error {
	if existing, _ := a.store.GetV6ReservationByIP(ctx, scope.ID, ip); existing != nil && existing.ID != excludeID {
		return fmt.Errorf("IP %s 已被 DUID %s 绑定", ip, existing.DUID)
	}
	if existing, _ := a.store.GetV6ReservationByDUID(ctx, scope.ID, duid); existing != nil && existing.ID != excludeID {
		return fmt.Errorf("DUID %s 已绑定 IP %s", duid, existing.IPAddr)
	}
	if lease, _ := a.store.GetV6LeaseByIP(ctx, scope.ID, ip); lease != nil && (lease.State == models.LeaseActive || lease.State == models.LeaseOffered) {
		if lease.DUID != duid {
			return fmt.Errorf("IP %s 当前已被 V6 租约占用（DUID %s）", ip, lease.DUID)
		}
	}
	return nil
}

func (a *API) handleCreateReservation(w http.ResponseWriter, r *http.Request) {
	scopeID := mux.Vars(r)["id"]
	scope, err := a.store.GetScopeByID(r.Context(), scopeID)
	if err != nil {
		a.jsonError(w, http.StatusNotFound, "scope not found")
		return
	}
	var req struct {
		MACAddr     string                 `json:"mac_addr"`
		DUID        string                 `json:"duid"`
		GroupID     string                 `json:"group_id"`
		IPAddr      string                 `json:"ip_addr"`
		Hostname    string                 `json:"hostname"`
		Description string                 `json:"description"`
		Options     map[string]interface{} `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ip := net.ParseIP(req.IPAddr)
	if ip == nil {
		a.jsonError(w, http.StatusBadRequest, "invalid ip")
		return
	}
	opts, _ := json.Marshal(req.Options)
	if scope.V6 {
		if req.DUID == "" {
			a.jsonError(w, http.StatusBadRequest, "invalid duid")
			return
		}
		if err := a.checkV6ReservationConflict(r.Context(), scope, req.DUID, ip, ""); err != nil {
			a.jsonError(w, http.StatusConflict, err.Error())
			return
		}
		res := &models.V6Reservation{
			ID:          uuid.New().String(),
			ScopeID:     scopeID,
			GroupID:     req.GroupID,
			DUID:        req.DUID,
			IPAddr:      ip,
			Hostname:    req.Hostname,
			Description: req.Description,
			Options:     opts,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		if err := a.store.CreateV6Reservation(r.Context(), res); err != nil {
			a.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.audit(r, "create", "v6_reservation", res.ID, res)
		a.jsonOK(w, res)
		return
	}
	hw, err := net.ParseMAC(req.MACAddr)
	if err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid mac")
		return
	}
	macStr := hw.String()
	if err := a.checkV4ReservationConflict(r.Context(), scope, macStr, ip, ""); err != nil {
		a.jsonError(w, http.StatusConflict, err.Error())
		return
	}
	res := &models.Reservation{
		ID:          uuid.New().String(),
		ScopeID:     scopeID,
		GroupID:     req.GroupID,
		MACAddr:     macStr,
		IPAddr:      ip,
		Hostname:    req.Hostname,
		Description: req.Description,
		Options:     opts,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := a.store.CreateReservation(r.Context(), res); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "create", "reservation", res.ID, res)
	a.jsonOK(w, res)
}

func (a *API) handleDeleteReservation(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := a.store.DeleteReservation(r.Context(), id); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "delete", "reservation", id, nil)
	a.jsonOK(w, map[string]string{"status": "deleted"})
}

func (a *API) handleUpdateReservation(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	existing, err := a.store.GetReservationByID(r.Context(), id)
	if err != nil {
		a.jsonError(w, http.StatusNotFound, "reservation not found")
		return
	}
	var req struct {
		MACAddr     string                 `json:"mac_addr"`
		GroupID     string                 `json:"group_id"`
		IPAddr      string                 `json:"ip_addr"`
		Hostname    string                 `json:"hostname"`
		Description string                 `json:"description"`
		Options     map[string]interface{} `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ip := net.ParseIP(req.IPAddr)
	if ip == nil {
		a.jsonError(w, http.StatusBadRequest, "invalid ip")
		return
	}
	hw, err := net.ParseMAC(req.MACAddr)
	if err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid mac")
		return
	}
	scope, err := a.store.GetScopeByID(r.Context(), existing.ScopeID)
	if err != nil {
		a.jsonError(w, http.StatusNotFound, "scope not found")
		return
	}
	macStr := hw.String()
	if err := a.checkV4ReservationConflict(r.Context(), scope, macStr, ip, id); err != nil {
		a.jsonError(w, http.StatusConflict, err.Error())
		return
	}
	opts, _ := json.Marshal(req.Options)
	res := &models.Reservation{
		ID:          id,
		MACAddr:     macStr,
		GroupID:     req.GroupID,
		IPAddr:      ip,
		Hostname:    req.Hostname,
		Description: req.Description,
		Options:     opts,
		UpdatedAt:   time.Now().UTC(),
	}
	if err := a.store.UpdateReservation(r.Context(), res); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := a.store.GetReservationByID(r.Context(), id)
	if updated == nil {
		updated = res
	}
	a.audit(r, "update", "reservation", id, updated)
	a.jsonOK(w, updated)
}

func (a *API) handleUpdateV6Reservation(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	existing, err := a.store.GetV6ReservationByID(r.Context(), id)
	if err != nil {
		a.jsonError(w, http.StatusNotFound, "reservation not found")
		return
	}
	var req struct {
		DUID        string                 `json:"duid"`
		GroupID     string                 `json:"group_id"`
		IPAddr      string                 `json:"ip_addr"`
		Hostname    string                 `json:"hostname"`
		Description string                 `json:"description"`
		Options     map[string]interface{} `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ip := net.ParseIP(req.IPAddr)
	if ip == nil {
		a.jsonError(w, http.StatusBadRequest, "invalid ip")
		return
	}
	if req.DUID == "" {
		a.jsonError(w, http.StatusBadRequest, "invalid duid")
		return
	}
	scope, err := a.store.GetScopeByID(r.Context(), existing.ScopeID)
	if err != nil {
		a.jsonError(w, http.StatusNotFound, "scope not found")
		return
	}
	if err := a.checkV6ReservationConflict(r.Context(), scope, req.DUID, ip, id); err != nil {
		a.jsonError(w, http.StatusConflict, err.Error())
		return
	}
	opts, _ := json.Marshal(req.Options)
	res := &models.V6Reservation{
		ID:          id,
		DUID:        req.DUID,
		GroupID:     req.GroupID,
		IPAddr:      ip,
		Hostname:    req.Hostname,
		Description: req.Description,
		Options:     opts,
		UpdatedAt:   time.Now().UTC(),
	}
	if err := a.store.UpdateV6Reservation(r.Context(), res); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := a.store.GetV6ReservationByID(r.Context(), id)
	if updated == nil {
		updated = res
	}
	a.audit(r, "update", "v6_reservation", id, updated)
	a.jsonOK(w, updated)
}

func (a *API) handleDeleteV6Reservation(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := a.store.DeleteV6Reservation(r.Context(), id); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "delete", "v6_reservation", id, nil)
	a.jsonOK(w, map[string]string{"status": "deleted"})
}

func (a *API) handleListReservationGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := a.store.ListReservationGroups(r.Context())
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.jsonOK(w, groups)
}

func (a *API) handleCreateReservationGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Options     map[string]interface{} `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" {
		a.jsonError(w, http.StatusBadRequest, "name is required")
		return
	}
	opts, _ := json.Marshal(req.Options)
	g := &models.ReservationGroup{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Options:     opts,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := a.store.CreateReservationGroup(r.Context(), g); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "create", "reservation_group", g.ID, g)
	a.jsonOK(w, g)
}

func (a *API) handleUpdateReservationGroup(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Options     map[string]interface{} `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" {
		a.jsonError(w, http.StatusBadRequest, "name is required")
		return
	}
	opts, _ := json.Marshal(req.Options)
	g := &models.ReservationGroup{
		ID:          id,
		Name:        req.Name,
		Description: req.Description,
		Options:     opts,
	}
	if err := a.store.UpdateReservationGroup(r.Context(), g); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := a.store.GetReservationGroupByID(r.Context(), id)
	if updated == nil {
		updated = g
	}
	a.audit(r, "update", "reservation_group", id, updated)
	a.jsonOK(w, updated)
}

func (a *API) handleDeleteReservationGroup(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := a.store.DeleteReservationGroup(r.Context(), id); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "delete", "reservation_group", id, nil)
	a.jsonOK(w, map[string]string{"status": "deleted"})
}

func (a *API) handleListLeases(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	scope, err := a.store.GetScopeByID(r.Context(), id)
	if err != nil {
		a.jsonError(w, http.StatusNotFound, "scope not found")
		return
	}
	offset, limit, page, pageSize := pagination(r)
	if scope.V6 {
		leases, total, err := a.store.ListV6LeasesByScopePaged(r.Context(), id, offset, limit)
		if err != nil {
			a.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.jsonOK(w, pageResponse(leases, total, page, pageSize))
		return
	}
	leases, total, err := a.store.ListLeasesByScopePaged(r.Context(), id, offset, limit)
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.jsonOK(w, pageResponse(leases, total, page, pageSize))
}

func (a *API) handleReleaseLease(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := a.store.UpdateLeaseState(r.Context(), id, models.LeaseReleased); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "release", "lease", id, nil)
	a.jsonOK(w, map[string]string{"status": "released"})
}

func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request) {
	offset, limit, page, pageSize := pagination(r)
	users, total, err := a.store.ListUsersPaged(r.Context(), offset, limit)
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.jsonOK(w, pageResponse(users, total, page, pageSize))
}

func (a *API) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Role == "" {
		req.Role = "admin"
	}
	user := &models.User{
		ID:           uuid.New().String(),
		Username:     req.Username,
		PasswordHash: hash,
		Role:         req.Role,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := a.store.CreateUser(r.Context(), user); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "create", "user", user.ID, map[string]string{"username": user.Username, "role": user.Role})
	user.PasswordHash = ""
	a.jsonOK(w, user)
}

func (a *API) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := a.store.DeleteUser(r.Context(), id); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "delete", "user", id, nil)
	a.jsonOK(w, map[string]string{"status": "deleted"})
}

func (a *API) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Role != "admin" && req.Role != "readonly" {
		a.jsonError(w, http.StatusBadRequest, "invalid role")
		return
	}
	user := &models.User{
		ID:        id,
		Username:  req.Username,
		Role:      req.Role,
		UpdatedAt: time.Now().UTC(),
	}
	if err := a.store.UpdateUser(r.Context(), user); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := a.store.GetUserByID(r.Context(), id)
	if updated == nil {
		updated = user
	}
	updated.PasswordHash = ""
	a.audit(r, "update", "user", id, map[string]string{"username": updated.Username, "role": updated.Role})
	a.jsonOK(w, updated)
}

func (a *API) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(auth.ContextUserKey).(*models.User)
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.NewPassword == "" {
		a.jsonError(w, http.StatusBadRequest, "new password required")
		return
	}
	// Verify old password
	existing, err := a.store.GetUserByUsername(r.Context(), user.Username)
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.auth.VerifyPassword(existing.PasswordHash, req.OldPassword); err != nil {
		a.jsonError(w, http.StatusUnauthorized, "old password incorrect")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.store.UpdateUserPassword(r.Context(), user.ID, hash); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "change_password", "user", user.ID, map[string]string{"username": user.Username})
	a.jsonOK(w, map[string]string{"status": "ok"})
}

func (a *API) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	offset, limit, page, pageSize := pagination(r)
	logs, total, err := a.store.ListAuditLogsPaged(r.Context(), offset, limit)
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.jsonOK(w, pageResponse(logs, total, page, pageSize))
}

func (a *API) handleListIPAllocationLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scopeID := q.Get("scope_id")
	nodeID := q.Get("node_id")
	mac := q.Get("mac")
	ip := q.Get("ip")
	action := q.Get("action")
	offset, limit, page, pageSize := pagination(r)
	logs, total, err := a.store.ListIPAllocationLogsPaged(r.Context(), scopeID, nodeID, mac, ip, action, offset, limit)
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.jsonOK(w, pageResponse(logs, total, page, pageSize))
}

func (a *API) handleListSystemLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	level := q.Get("level")
	nodeID := q.Get("node_id")
	offset, limit, page, pageSize := pagination(r)
	logs, total, err := a.store.ListSystemLogsPaged(r.Context(), level, nodeID, offset, limit)
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.jsonOK(w, pageResponse(logs, total, page, pageSize))
}

func (a *API) handleDeleteLease(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := a.store.DeleteLease(r.Context(), id); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "delete", "lease", id, nil)
	a.jsonOK(w, map[string]string{"status": "deleted"})
}

func (a *API) handleReleaseV6Lease(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := a.store.UpdateV6LeaseState(r.Context(), id, models.LeaseReleased); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "release", "v6_lease", id, nil)
	a.jsonOK(w, map[string]string{"status": "released"})
}

func (a *API) handleDeleteV6Lease(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := a.store.DeleteV6Lease(r.Context(), id); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "delete", "v6_lease", id, nil)
	a.jsonOK(w, map[string]string{"status": "deleted"})
}

func (a *API) handleSearchLeases(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mac := q.Get("mac")
	duid := q.Get("duid")
	if mac != "" {
		leases, err := a.store.SearchLeasesByMAC(r.Context(), mac)
		if err != nil {
			a.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.jsonOK(w, map[string]interface{}{"v4": leases, "v6": []*models.V6Lease{}})
		return
	}
	if duid != "" {
		leases, err := a.store.SearchV6LeasesByDUID(r.Context(), duid)
		if err != nil {
			a.jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.jsonOK(w, map[string]interface{}{"v4": []*models.Lease{}, "v6": leases})
		return
	}
	a.jsonError(w, http.StatusBadRequest, "mac or duid required")
}

func (a *API) handleListMACBlacklist(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.ListMACBlacklist(r.Context())
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.jsonOK(w, list)
}

func (a *API) handleCreateMACBlacklist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MACAddr string `json:"mac_addr"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.MACAddr == "" {
		a.jsonError(w, http.StatusBadRequest, "mac_addr required")
		return
	}
	hw, err := net.ParseMAC(req.MACAddr)
	if err != nil {
		a.jsonError(w, http.StatusBadRequest, "invalid mac")
		return
	}
	b := &models.MACBlacklist{
		ID:        uuid.New().String(),
		MACAddr:   hw.String(),
		Reason:    req.Reason,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := a.store.CreateMACBlacklist(r.Context(), b); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "create", "mac_blacklist", b.ID, map[string]string{"mac_addr": b.MACAddr})
	a.jsonOK(w, b)
}

func (a *API) handleDeleteMACBlacklist(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := a.store.DeleteMACBlacklist(r.Context(), id); err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r, "delete", "mac_blacklist", id, nil)
	a.jsonOK(w, map[string]string{"status": "deleted"})
}

func (a *API) handleListClusterNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := a.store.ListHANodesByCluster(r.Context(), a.cfg.Cluster.ClusterID)
	if err != nil {
		a.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.jsonOK(w, map[string]interface{}{
		"cluster_id": a.cfg.Cluster.ClusterID,
		"nodes":      nodes,
	})
}
