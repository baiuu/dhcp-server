package store

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/dhcp-server/dhcp-server/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func nullableUUID(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---------- Users ----------

func (s *Store) CreateUser(ctx context.Context, user *models.User) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, user.ID, user.Username, user.PasswordHash, user.Role, user.CreatedAt, user.UpdatedAt)
	return err
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, created_at, updated_at
		FROM users WHERE id=$1
	`, id)
	var u models.User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, created_at, updated_at
		FROM users WHERE username = $1
	`, username)
	var u models.User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) ListUsers(ctx context.Context) ([]*models.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, username, role, created_at, updated_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]*models.User, 0)
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}

func (s *Store) ListUsersPaged(ctx context.Context, offset, limit int) ([]*models.User, int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, username, role, created_at, updated_at FROM users ORDER BY username LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	users := make([]*models.User, 0)
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, 0, err
		}
		users = append(users, &u)
	}
	return users, total, rows.Err()
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	return err
}

func (s *Store) UpdateUserPassword(ctx context.Context, id, hash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET password_hash=$2, updated_at=$3 WHERE id=$1`, id, hash, time.Now().UTC())
	return err
}

func (s *Store) UpdateUser(ctx context.Context, user *models.User) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET username=$2, role=$3, updated_at=$4 WHERE id=$1
	`, user.ID, user.Username, user.Role, time.Now().UTC())
	return err
}

func (s *Store) CreateAuditLog(ctx context.Context, log *models.AuditLog) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_logs (id, username, action, resource, resource_id, details, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, log.ID, log.Username, log.Action, log.Resource, log.ResourceID, log.Details, log.CreatedAt)
	return err
}

func (s *Store) ListAuditLogs(ctx context.Context, limit int) ([]*models.AuditLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, username, action, resource, resource_id, details, created_at
		FROM audit_logs ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]*models.AuditLog, 0)
	for rows.Next() {
		var l models.AuditLog
		err := rows.Scan(&l.ID, &l.Username, &l.Action, &l.Resource, &l.ResourceID, &l.Details, &l.CreatedAt)
		if err != nil {
			return nil, err
		}
		logs = append(logs, &l)
	}
	return logs, rows.Err()
}

func (s *Store) ListAuditLogsPaged(ctx context.Context, offset, limit int) ([]*models.AuditLog, int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, username, action, resource, resource_id, details, created_at
		FROM audit_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	logs := make([]*models.AuditLog, 0)
	for rows.Next() {
		var l models.AuditLog
		err := rows.Scan(&l.ID, &l.Username, &l.Action, &l.Resource, &l.ResourceID, &l.Details, &l.CreatedAt)
		if err != nil {
			return nil, 0, err
		}
		logs = append(logs, &l)
	}
	return logs, total, rows.Err()
}

func (s *Store) CreateIPAllocationLog(ctx context.Context, log *models.IPAllocationLog) error {
	var ip, relay interface{}
	if log.IPAddr != nil {
		ip = log.IPAddr
	}
	if log.RelayIP != nil {
		relay = log.RelayIP
	}
	var prefix interface{}
	if log.Prefix != nil {
		prefix = log.Prefix
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ip_allocation_logs (id, node_id, scope_id, scope_name, mac_addr, duid, iaid, ip_addr, prefix, action, client_info, relay_ip, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, regexp_replace(host($8), '^::ffff:', '')::inet, regexp_replace(text($9), '^::ffff:', '')::cidr, $10, $11, regexp_replace(host($12), '^::ffff:', '')::inet, $13)
	`, log.ID, log.NodeID, log.ScopeID, log.ScopeName, log.MACAddr, log.DUID, log.IAID, ip, prefix, log.Action, log.ClientInfo, relay, log.CreatedAt)
	return err
}

func (s *Store) ListIPAllocationLogsPaged(ctx context.Context, scopeID, nodeID, mac, ip, action string, offset, limit int) ([]*models.IPAllocationLog, int64, error) {
	var total int64
	where := "WHERE 1=1"
	args := []interface{}{}
	argIdx := 1
	if scopeID != "" {
		where += fmt.Sprintf(" AND scope_id=$%d", argIdx)
		args = append(args, scopeID)
		argIdx++
	}
	if nodeID != "" {
		where += fmt.Sprintf(" AND node_id=$%d", argIdx)
		args = append(args, nodeID)
		argIdx++
	}
	if mac != "" {
		where += fmt.Sprintf(" AND (mac_addr=$%d OR duid=$%d)", argIdx, argIdx)
		args = append(args, mac)
		argIdx++
	}
	if ip != "" {
		where += fmt.Sprintf(" AND (ip_addr=regexp_replace(host($%d), '^::ffff:', '')::inet OR prefix=regexp_replace(text($%d), '^::ffff:', '')::cidr)", argIdx, argIdx)
		args = append(args, ip)
		argIdx++
	}
	if action != "" {
		where += fmt.Sprintf(" AND action=$%d", argIdx)
		args = append(args, action)
		argIdx++
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM ip_allocation_logs %s", where)
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`
		SELECT id, node_id, scope_id, scope_name, mac_addr, duid, iaid, ip_addr, prefix, action, client_info, relay_ip, created_at
		FROM ip_allocation_logs %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, limit, offset)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	logs := make([]*models.IPAllocationLog, 0)
	for rows.Next() {
		l, err := scanIPAllocationLog(rows)
		if err != nil {
			return nil, 0, err
		}
		logs = append(logs, l)
	}
	return logs, total, rows.Err()
}

func scanIPAllocationLog(row pgx.Row) (*models.IPAllocationLog, error) {
	var l models.IPAllocationLog
	var ip net.IP
	var relay net.IP
	var prefix *net.IPNet
	err := row.Scan(&l.ID, &l.NodeID, &l.ScopeID, &l.ScopeName, &l.MACAddr, &l.DUID, &l.IAID, &ip, &prefix, &l.Action, &l.ClientInfo, &relay, &l.CreatedAt)
	if err != nil {
		return nil, err
	}
	if ip != nil {
		l.IPAddr = to4IfPossible(ip)
	}
	if relay != nil {
		l.RelayIP = to4IfPossible(relay)
	}
	l.Prefix = prefix
	return &l, nil
}

func (s *Store) CreateSystemLog(ctx context.Context, log *models.SystemLog) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO system_logs (id, node_id, level, message, attrs, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, log.ID, log.NodeID, log.Level, log.Message, log.Attrs, log.CreatedAt)
	return err
}

func (s *Store) ListSystemLogsPaged(ctx context.Context, level, nodeID string, offset, limit int) ([]*models.SystemLog, int64, error) {
	var total int64
	where := "WHERE 1=1"
	args := []interface{}{}
	argIdx := 1
	if level != "" {
		where += fmt.Sprintf(" AND level=$%d", argIdx)
		args = append(args, level)
		argIdx++
	}
	if nodeID != "" {
		where += fmt.Sprintf(" AND node_id=$%d", argIdx)
		args = append(args, nodeID)
		argIdx++
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM system_logs %s", where)
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`
		SELECT id, node_id, level, message, attrs, created_at
		FROM system_logs %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, limit, offset)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	logs := make([]*models.SystemLog, 0)
	for rows.Next() {
		var l models.SystemLog
		if err := rows.Scan(&l.ID, &l.NodeID, &l.Level, &l.Message, &l.Attrs, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		logs = append(logs, &l)
	}
	return logs, total, rows.Err()
}

// ---------- Scopes ----------

func (s *Store) CreateScope(ctx context.Context, scope *models.Scope) error {
	var prefix interface{}
	if scope.Prefix != nil {
		prefix = scope.Prefix
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO scopes (id, name, v6, subnet, prefix, start_ip, end_ip, gateway, dns, excluded_ips, domain_name, lease_time, max_lease_time, enabled, options, created_at, updated_at)
		VALUES ($1, $2, $3,
			regexp_replace(text($4), '^::ffff:', '')::cidr,
			regexp_replace(text($5), '^::ffff:', '')::cidr,
			regexp_replace(host($6), '^::ffff:', '')::inet, regexp_replace(host($7), '^::ffff:', '')::inet,
			ARRAY(SELECT regexp_replace(host(x), '^::ffff:', '')::inet FROM unnest($8::inet[]) x),
			ARRAY(SELECT regexp_replace(host(x), '^::ffff:', '')::inet FROM unnest($9::inet[]) x),
			ARRAY(SELECT regexp_replace(host(x), '^::ffff:', '')::inet FROM unnest($10::inet[]) x),
			$11, $12, $13, $14, $15, $16, $17)
	`, scope.ID, scope.Name, scope.V6, scope.Subnet, prefix,
		scope.StartIP, scope.EndIP,
		pgtype.FlatArray[net.IP](scope.Gateway), pgtype.FlatArray[net.IP](scope.DNS), pgtype.FlatArray[net.IP](scope.ExcludedIPs),
		scope.DomainName, scope.LeaseTime, scope.MaxLeaseTime, scope.Enabled, scope.Options,
		scope.CreatedAt, scope.UpdatedAt)
	return err
}

func (s *Store) GetScopeByID(ctx context.Context, id string) (*models.Scope, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, name, v6, subnet, prefix, start_ip, end_ip, gateway, dns, excluded_ips, domain_name, lease_time, max_lease_time, enabled, options, created_at, updated_at
		FROM scopes WHERE id = $1
	`, id)
	return scanScope(row)
}

func (s *Store) GetScopeByName(ctx context.Context, name string) (*models.Scope, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, name, v6, subnet, prefix, start_ip, end_ip, gateway, dns, excluded_ips, domain_name, lease_time, max_lease_time, enabled, options, created_at, updated_at
		FROM scopes WHERE name = $1
	`, name)
	return scanScope(row)
}

func (s *Store) ListScopes(ctx context.Context) ([]*models.Scope, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, v6, subnet, prefix, start_ip, end_ip, gateway, dns, excluded_ips, domain_name, lease_time, max_lease_time, enabled, options, created_at, updated_at
		FROM scopes ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	scopes := make([]*models.Scope, 0)
	for rows.Next() {
		sc, err := scanScope(rows)
		if err != nil {
			return nil, err
		}
		scopes = append(scopes, sc)
	}
	return scopes, rows.Err()
}

func (s *Store) UpdateScope(ctx context.Context, scope *models.Scope) error {
	var prefix interface{}
	if scope.Prefix != nil {
		prefix = scope.Prefix
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE scopes SET name=$2, v6=$3,
			subnet=regexp_replace(text($4), '^::ffff:', '')::cidr,
			prefix=regexp_replace(text($5), '^::ffff:', '')::cidr,
			start_ip=regexp_replace(host($6), '^::ffff:', '')::inet, end_ip=regexp_replace(host($7), '^::ffff:', '')::inet,
			gateway=ARRAY(SELECT regexp_replace(host(x), '^::ffff:', '')::inet FROM unnest($8::inet[]) x),
			dns=ARRAY(SELECT regexp_replace(host(x), '^::ffff:', '')::inet FROM unnest($9::inet[]) x),
			excluded_ips=ARRAY(SELECT regexp_replace(host(x), '^::ffff:', '')::inet FROM unnest($10::inet[]) x),
			domain_name=$11,
			lease_time=$12, max_lease_time=$13, enabled=$14, options=$15, updated_at=$16
		WHERE id=$1
	`, scope.ID, scope.Name, scope.V6, scope.Subnet, prefix,
		scope.StartIP, scope.EndIP,
		pgtype.FlatArray[net.IP](scope.Gateway), pgtype.FlatArray[net.IP](scope.DNS), pgtype.FlatArray[net.IP](scope.ExcludedIPs),
		scope.DomainName, scope.LeaseTime, scope.MaxLeaseTime, scope.Enabled, scope.Options,
		scope.UpdatedAt)
	return err
}

func (s *Store) DeleteScope(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM scopes WHERE id=$1`, id)
	return err
}

func (s *Store) ListScopesPaged(ctx context.Context, v6 *bool, offset, limit int) ([]*models.Scope, int64, error) {
	var total int64
	where := ""
	args := []interface{}{}
	if v6 != nil {
		where = " WHERE v6 = $1"
		args = append(args, *v6)
	}
	countSQL := "SELECT COUNT(*) FROM scopes" + where
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append([]interface{}{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	paramOffset := len(args)
	limitSQL := fmt.Sprintf(" LIMIT $%d OFFSET $%d", paramOffset+1, paramOffset+2)
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, v6, subnet, prefix, start_ip, end_ip, gateway, dns, excluded_ips, domain_name, lease_time, max_lease_time, enabled, options, created_at, updated_at
		FROM scopes`+where+` ORDER BY name`+limitSQL,
		queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	scopes := make([]*models.Scope, 0)
	for rows.Next() {
		sc, err := scanScope(rows)
		if err != nil {
			return nil, 0, err
		}
		scopes = append(scopes, sc)
	}
	return scopes, total, rows.Err()
}

func scanScope(row pgx.Row) (*models.Scope, error) {
	var sc models.Scope
	var cidr net.IPNet
	var prefix *net.IPNet
	var startIP, endIP net.IP
	var gw, dns, excluded []net.IP
	err := row.Scan(&sc.ID, &sc.Name, &sc.V6, &cidr, &prefix, &startIP, &endIP, &gw, &dns, &excluded, &sc.DomainName,
		&sc.LeaseTime, &sc.MaxLeaseTime, &sc.Enabled, &sc.Options, &sc.CreatedAt, &sc.UpdatedAt)
	if err != nil {
		return nil, err
	}
	sc.Subnet = &cidr
	if sc.Subnet.IP != nil {
		sc.Subnet.IP = to4IfPossible(sc.Subnet.IP)
	}
	sc.Prefix = prefix
	if sc.Prefix != nil && sc.Prefix.IP != nil {
		sc.Prefix.IP = to4IfPossible(sc.Prefix.IP)
	}
	if startIP != nil {
		sc.StartIP = to4IfPossible(startIP)
	}
	if endIP != nil {
		sc.EndIP = to4IfPossible(endIP)
	}
	sc.Gateway = ipsTo4(gw)
	sc.DNS = ipsTo4(dns)
	sc.ExcludedIPs = ipsTo4(excluded)
	return &sc, nil
}

// ---------- Reservations ----------

func (s *Store) CreateReservation(ctx context.Context, r *models.Reservation) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO reservations (id, scope_id, group_id, mac_addr, ip_addr, hostname, description, options, created_at, updated_at)
		VALUES ($1, $2, $3, $4, regexp_replace(host($5), '^::ffff:', '')::inet, $6, $7, $8, $9, $10)
	`, r.ID, r.ScopeID, nullableUUID(r.GroupID), r.MACAddr,
		r.IPAddr,
		r.Hostname, r.Description, r.Options, r.CreatedAt, r.UpdatedAt)
	return err
}

func (s *Store) GetReservationByID(ctx context.Context, id string) (*models.Reservation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_id, group_id, mac_addr, ip_addr, hostname, description, options, created_at, updated_at
		FROM reservations WHERE id=$1
	`, id)
	return scanReservation(row)
}

func (s *Store) GetReservationByMAC(ctx context.Context, scopeID, mac string) (*models.Reservation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_id, group_id, mac_addr, ip_addr, hostname, description, options, created_at, updated_at
		FROM reservations WHERE scope_id=$1 AND mac_addr=$2
	`, scopeID, mac)
	return scanReservation(row)
}

func (s *Store) GetReservationByIP(ctx context.Context, scopeID string, ip net.IP) (*models.Reservation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_id, group_id, mac_addr, ip_addr, hostname, description, options, created_at, updated_at
		FROM reservations WHERE scope_id=$1 AND ip_addr=regexp_replace(host($2), '^::ffff:', '')::inet
	`, scopeID, ip)
	return scanReservation(row)
}

func (s *Store) ListReservationsByScope(ctx context.Context, scopeID string) ([]*models.Reservation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, group_id, mac_addr, ip_addr, hostname, description, options, created_at, updated_at
		FROM reservations WHERE scope_id=$1 ORDER BY ip_addr
	`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := make([]*models.Reservation, 0)
	for rows.Next() {
		r, err := scanReservation(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, rows.Err()
}

func (s *Store) DeleteReservation(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM reservations WHERE id=$1`, id)
	return err
}

func (s *Store) UpdateReservation(ctx context.Context, r *models.Reservation) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE reservations SET mac_addr=$2, ip_addr=regexp_replace(host($3), '^::ffff:', '')::inet, group_id=$4, hostname=$5, description=$6, options=$7, updated_at=$8
		WHERE id=$1
	`, r.ID, r.MACAddr, r.IPAddr, nullableUUID(r.GroupID), r.Hostname, r.Description, r.Options, time.Now().UTC())
	return err
}

func (s *Store) ListReservationsByScopePaged(ctx context.Context, scopeID string, offset, limit int) ([]*models.Reservation, int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM reservations WHERE scope_id=$1`, scopeID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, group_id, mac_addr, ip_addr, hostname, description, options, created_at, updated_at
		FROM reservations WHERE scope_id=$1 ORDER BY ip_addr LIMIT $2 OFFSET $3
	`, scopeID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	res := make([]*models.Reservation, 0)
	for rows.Next() {
		r, err := scanReservation(rows)
		if err != nil {
			return nil, 0, err
		}
		res = append(res, r)
	}
	return res, total, rows.Err()
}

func scanReservation(row pgx.Row) (*models.Reservation, error) {
	var r models.Reservation
	var ip net.IP
	var groupID, hostname, description *string
	err := row.Scan(&r.ID, &r.ScopeID, &groupID, &r.MACAddr, &ip, &hostname, &description, &r.Options, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if ip != nil {
		r.IPAddr = to4IfPossible(ip)
	}
	if groupID != nil {
		r.GroupID = *groupID
	}
	if hostname != nil {
		r.Hostname = *hostname
	}
	if description != nil {
		r.Description = *description
	}
	return &r, nil
}

// ---------- Leases ----------

func (s *Store) GetLeaseByMAC(ctx context.Context, scopeID, mac string) (*models.Lease, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_id, mac_addr, ip_addr, hostname, client_id, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM leases WHERE scope_id=$1 AND mac_addr=$2
	`, scopeID, mac)
	return scanLease(row)
}

func (s *Store) GetLeaseByIP(ctx context.Context, scopeID string, ip net.IP) (*models.Lease, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_id, mac_addr, ip_addr, hostname, client_id, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM leases WHERE scope_id=$1 AND ip_addr=regexp_replace(host($2), '^::ffff:', '')::inet
	`, scopeID, ip)
	return scanLease(row)
}

func (s *Store) CreateOrUpdateLease(ctx context.Context, lease *models.Lease) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO leases (id, scope_id, mac_addr, ip_addr, hostname, client_id, state, offered_at, starts_at, ends_at, created_at, updated_at)
		VALUES ($1, $2, $3, regexp_replace(host($4), '^::ffff:', '')::inet, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (scope_id, ip_addr) DO UPDATE SET
			mac_addr=EXCLUDED.mac_addr,
			hostname=EXCLUDED.hostname,
			client_id=EXCLUDED.client_id,
			state=EXCLUDED.state,
			offered_at=EXCLUDED.offered_at,
			starts_at=EXCLUDED.starts_at,
			ends_at=EXCLUDED.ends_at,
			updated_at=EXCLUDED.updated_at
	`, lease.ID, lease.ScopeID, lease.MACAddr,
		lease.IPAddr,
		lease.Hostname, lease.ClientID, string(lease.State), lease.OfferedAt, lease.StartsAt, lease.EndsAt,
		lease.CreatedAt, lease.UpdatedAt)
	return err
}

func (s *Store) UpdateLeaseState(ctx context.Context, id string, state models.LeaseState) error {
	_, err := s.pool.Exec(ctx, `UPDATE leases SET state=$2, updated_at=$3 WHERE id=$1`, id, string(state), time.Now().UTC())
	return err
}

func (s *Store) ListLeasesByScope(ctx context.Context, scopeID string) ([]*models.Lease, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, mac_addr, ip_addr, hostname, client_id, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM leases WHERE scope_id=$1 ORDER BY ip_addr
	`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	leases := make([]*models.Lease, 0)
	for rows.Next() {
		l, err := scanLease(rows)
		if err != nil {
			return nil, err
		}
		leases = append(leases, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = s.markReservedLeases(ctx, leases)
	return leases, nil
}

func (s *Store) ListLeasesByScopePaged(ctx context.Context, scopeID string, offset, limit int) ([]*models.Lease, int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM leases WHERE scope_id=$1`, scopeID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, mac_addr, ip_addr, hostname, client_id, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM leases WHERE scope_id=$1 ORDER BY ip_addr LIMIT $2 OFFSET $3
	`, scopeID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	leases := make([]*models.Lease, 0)
	for rows.Next() {
		l, err := scanLease(rows)
		if err != nil {
			return nil, 0, err
		}
		leases = append(leases, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	_ = s.markReservedLeases(ctx, leases)
	return leases, total, nil
}

func (s *Store) ListActiveLeases(ctx context.Context) ([]*models.Lease, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, mac_addr, ip_addr, hostname, client_id, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM leases WHERE state IN ('active', 'offered') ORDER BY ends_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	leases := make([]*models.Lease, 0)
	for rows.Next() {
		l, err := scanLease(rows)
		if err != nil {
			return nil, err
		}
		leases = append(leases, l)
	}
	return leases, rows.Err()
}

func (s *Store) ReleaseExpiredLeases(ctx context.Context, before time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE leases SET state='expired', updated_at=$2
		WHERE state='active' AND ends_at < $1
	`, before, time.Now().UTC())
	return err
}

func (s *Store) SearchLeasesByMAC(ctx context.Context, mac string) ([]*models.Lease, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, mac_addr, ip_addr, hostname, client_id, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM leases WHERE mac_addr ILIKE $1 ORDER BY ends_at DESC
	`, "%"+mac+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	leases := make([]*models.Lease, 0)
	for rows.Next() {
		l, err := scanLease(rows)
		if err != nil {
			return nil, err
		}
		leases = append(leases, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = s.markReservedLeases(ctx, leases)
	return leases, nil
}

func (s *Store) DeleteLease(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM leases WHERE id=$1`, id)
	return err
}

// CleanupExpiredLeases removes v4/v6 leases (and delegated prefixes) whose
// ends_at is older than the corresponding scope's max_lease_time (falling back
// to lease_time, then 1 day). This prevents the lease tables from growing
// indefinitely with stale entries.
func (s *Store) CleanupExpiredLeases(ctx context.Context) (int64, error) {
	leaseTag, err := s.pool.Exec(ctx, `
		DELETE FROM leases l
		USING scopes s
		WHERE l.scope_id = s.id
		  AND l.ends_at < NOW() - make_interval(secs => COALESCE(NULLIF(s.max_lease_time, 0), NULLIF(s.lease_time, 0), 86400))
	`)
	if err != nil {
		return 0, err
	}
	total := leaseTag.RowsAffected()

	v6Tag, err := s.pool.Exec(ctx, `
		DELETE FROM v6_leases l
		USING scopes s
		WHERE l.scope_id = s.id
		  AND l.ends_at < NOW() - make_interval(secs => COALESCE(NULLIF(s.max_lease_time, 0), NULLIF(s.lease_time, 0), 86400))
	`)
	if err != nil {
		return total, err
	}
	total += v6Tag.RowsAffected()

	pdTag, err := s.pool.Exec(ctx, `
		DELETE FROM v6_prefixes p
		USING scopes s
		WHERE p.scope_id = s.id
		  AND p.ends_at < NOW() - make_interval(secs => COALESCE(NULLIF(p.valid_lifetime, 0), 86400))
	`)
	if err != nil {
		return total, err
	}
	total += pdTag.RowsAffected()

	// Retain IP allocation history for 180 days.
	logTag, err := s.pool.Exec(ctx, `
		DELETE FROM ip_allocation_logs WHERE created_at < NOW() - INTERVAL '180 days'
	`)
	if err != nil {
		return total, err
	}
	total += logTag.RowsAffected()

	// Retain system logs for 30 days.
	sysTag, err := s.pool.Exec(ctx, `
		DELETE FROM system_logs WHERE created_at < NOW() - INTERVAL '30 days'
	`)
	if err != nil {
		return total, err
	}
	total += sysTag.RowsAffected()

	return total, nil
}

func scanLease(row pgx.Row) (*models.Lease, error) {
	var l models.Lease
	var ip net.IP
	var hostname, clientID *string
	err := row.Scan(&l.ID, &l.ScopeID, &l.MACAddr, &ip, &hostname, &clientID, &l.State, &l.OfferedAt, &l.StartsAt, &l.EndsAt, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if ip != nil {
		l.IPAddr = to4IfPossible(ip)
	}
	if hostname != nil {
		l.Hostname = *hostname
	}
	if clientID != nil {
		l.ClientID = *clientID
	}
	return &l, nil
}

func ipKey(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func (s *Store) markReservedLeases(ctx context.Context, leases []*models.Lease) error {
	if len(leases) == 0 {
		return nil
	}
	byScope := make(map[string][]*models.Lease)
	for _, l := range leases {
		byScope[l.ScopeID] = append(byScope[l.ScopeID], l)
	}
	for scopeID, list := range byScope {
		res, err := s.ListReservationsByScope(ctx, scopeID)
		if err != nil {
			return err
		}
		reserved := make(map[string]bool)
		for _, r := range res {
			reserved[ipKey(r.IPAddr)] = true
			reserved[r.MACAddr] = true
		}
		for _, l := range list {
			if reserved[ipKey(l.IPAddr)] || reserved[l.MACAddr] {
				l.Reserved = true
			}
		}
	}
	return nil
}

func (s *Store) markReservedV6Leases(ctx context.Context, leases []*models.V6Lease) error {
	if len(leases) == 0 {
		return nil
	}
	byScope := make(map[string][]*models.V6Lease)
	for _, l := range leases {
		byScope[l.ScopeID] = append(byScope[l.ScopeID], l)
	}
	for scopeID, list := range byScope {
		res, err := s.ListV6ReservationsByScope(ctx, scopeID)
		if err != nil {
			return err
		}
		reserved := make(map[string]bool)
		for _, r := range res {
			reserved[ipKey(r.IPAddr)] = true
			reserved[r.DUID] = true
		}
		for _, l := range list {
			if reserved[ipKey(l.IPAddr)] || reserved[l.DUID] {
				l.Reserved = true
			}
		}
	}
	return nil
}

// ---------- HA Nodes ----------

func (s *Store) UpsertHANode(ctx context.Context, node *models.HANode) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ha_nodes (id, cluster_id, node_id, role, listen_addr, version, healthy, last_seen, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (node_id) DO UPDATE SET
			cluster_id=EXCLUDED.cluster_id,
			role=EXCLUDED.role,
			listen_addr=EXCLUDED.listen_addr,
			version=EXCLUDED.version,
			healthy=EXCLUDED.healthy,
			last_seen=EXCLUDED.last_seen,
			updated_at=EXCLUDED.updated_at
	`, node.ID, node.ClusterID, node.NodeID, node.Role, node.ListenAddr, node.Version, node.Healthy, node.LastSeen, node.CreatedAt, node.UpdatedAt)
	return err
}

func (s *Store) GetHANode(ctx context.Context, nodeID string) (*models.HANode, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, cluster_id, node_id, role, listen_addr, version, healthy, last_seen, created_at, updated_at
		FROM ha_nodes WHERE node_id=$1
	`, nodeID)
	var n models.HANode
	err := row.Scan(&n.ID, &n.ClusterID, &n.NodeID, &n.Role, &n.ListenAddr, &n.Version, &n.Healthy, &n.LastSeen, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) ListHANodesByCluster(ctx context.Context, clusterID string) ([]*models.HANode, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, cluster_id, node_id, role, listen_addr, version, healthy, last_seen, created_at, updated_at
		FROM ha_nodes WHERE cluster_id=$1 ORDER BY node_id
	`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []*models.HANode
	for rows.Next() {
		var n models.HANode
		if err := rows.Scan(&n.ID, &n.ClusterID, &n.NodeID, &n.Role, &n.ListenAddr, &n.Version, &n.Healthy, &n.LastSeen, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		nodes = append(nodes, &n)
	}
	return nodes, rows.Err()
}

// ---------- Helpers ----------

func to4IfPossible(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

func ipsTo4(ips []net.IP) []net.IP {
	out := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		out = append(out, to4IfPossible(ip))
	}
	return out
}

func MustRawJSON(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ---------- V6 Leases ----------

func (s *Store) GetV6LeaseByDUID(ctx context.Context, scopeID, duid, iaid string) (*models.V6Lease, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_id, duid, iaid, ip_addr, hostname, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM v6_leases WHERE scope_id=$1 AND duid=$2 AND iaid=$3
	`, scopeID, duid, iaid)
	return scanV6Lease(row)
}

func (s *Store) GetV6LeaseByIP(ctx context.Context, scopeID string, ip net.IP) (*models.V6Lease, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_id, duid, iaid, ip_addr, hostname, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM v6_leases WHERE scope_id=$1 AND ip_addr=regexp_replace(host($2), '^::ffff:', '')::inet
	`, scopeID, ip)
	return scanV6Lease(row)
}

func (s *Store) CreateOrUpdateV6Lease(ctx context.Context, lease *models.V6Lease) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO v6_leases (id, scope_id, duid, iaid, ip_addr, hostname, state, offered_at, starts_at, ends_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, regexp_replace(host($5), '^::ffff:', '')::inet, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (scope_id, duid, iaid) DO UPDATE SET
			ip_addr=regexp_replace(host(EXCLUDED.ip_addr), '^::ffff:', '')::inet,
			hostname=EXCLUDED.hostname,
			state=EXCLUDED.state,
			offered_at=EXCLUDED.offered_at,
			starts_at=EXCLUDED.starts_at,
			ends_at=EXCLUDED.ends_at,
			updated_at=EXCLUDED.updated_at
	`, lease.ID, lease.ScopeID, lease.DUID, lease.IAID, lease.IPAddr, lease.Hostname, string(lease.State), lease.OfferedAt, lease.StartsAt, lease.EndsAt, lease.CreatedAt, lease.UpdatedAt)
	return err
}

func (s *Store) UpdateV6LeaseState(ctx context.Context, id string, state models.LeaseState) error {
	_, err := s.pool.Exec(ctx, `UPDATE v6_leases SET state=$2, updated_at=$3 WHERE id=$1`, id, string(state), time.Now().UTC())
	return err
}

func (s *Store) ListV6LeasesByScope(ctx context.Context, scopeID string) ([]*models.V6Lease, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, duid, iaid, ip_addr, hostname, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM v6_leases WHERE scope_id=$1 ORDER BY ip_addr
	`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	leases := make([]*models.V6Lease, 0)
	for rows.Next() {
		l, err := scanV6Lease(rows)
		if err != nil {
			return nil, err
		}
		leases = append(leases, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = s.markReservedV6Leases(ctx, leases)
	return leases, nil
}

func (s *Store) ListV6LeasesByScopePaged(ctx context.Context, scopeID string, offset, limit int) ([]*models.V6Lease, int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM v6_leases WHERE scope_id=$1`, scopeID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, duid, iaid, ip_addr, hostname, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM v6_leases WHERE scope_id=$1 ORDER BY ip_addr LIMIT $2 OFFSET $3
	`, scopeID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	leases := make([]*models.V6Lease, 0)
	for rows.Next() {
		l, err := scanV6Lease(rows)
		if err != nil {
			return nil, 0, err
		}
		leases = append(leases, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	_ = s.markReservedV6Leases(ctx, leases)
	return leases, total, nil
}

func (s *Store) ListActiveV6Leases(ctx context.Context) ([]*models.V6Lease, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, duid, iaid, ip_addr, hostname, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM v6_leases WHERE state IN ('active', 'offered') ORDER BY ends_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	leases := make([]*models.V6Lease, 0)
	for rows.Next() {
		l, err := scanV6Lease(rows)
		if err != nil {
			return nil, err
		}
		leases = append(leases, l)
	}
	return leases, rows.Err()
}

func (s *Store) ReleaseExpiredV6Leases(ctx context.Context, before time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE v6_leases SET state='expired', updated_at=$2
		WHERE state='active' AND ends_at < $1
	`, before, time.Now().UTC())
	return err
}

func (s *Store) SearchV6LeasesByDUID(ctx context.Context, duid string) ([]*models.V6Lease, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, duid, iaid, ip_addr, hostname, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM v6_leases WHERE duid ILIKE $1 ORDER BY ends_at DESC
	`, "%"+duid+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	leases := make([]*models.V6Lease, 0)
	for rows.Next() {
		l, err := scanV6Lease(rows)
		if err != nil {
			return nil, err
		}
		leases = append(leases, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = s.markReservedV6Leases(ctx, leases)
	return leases, nil
}

func (s *Store) DeleteV6Lease(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM v6_leases WHERE id=$1`, id)
	return err
}

func scanV6Lease(row pgx.Row) (*models.V6Lease, error) {
	var l models.V6Lease
	var hostname *string
	err := row.Scan(&l.ID, &l.ScopeID, &l.DUID, &l.IAID, &l.IPAddr, &hostname, &l.State, &l.OfferedAt, &l.StartsAt, &l.EndsAt, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return nil, err
	}
	l.IPAddr = to4IfPossible(l.IPAddr)
	if hostname != nil {
		l.Hostname = *hostname
	}
	return &l, nil
}

// ---------- V6 Reservations ----------

func (s *Store) CreateV6Reservation(ctx context.Context, r *models.V6Reservation) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO v6_reservations (id, scope_id, group_id, duid, ip_addr, hostname, description, options, created_at, updated_at)
		VALUES ($1, $2, $3, $4, regexp_replace(host($5), '^::ffff:', '')::inet, $6, $7, $8, $9, $10)
	`, r.ID, r.ScopeID, nullableUUID(r.GroupID), r.DUID, r.IPAddr, r.Hostname, r.Description, r.Options, r.CreatedAt, r.UpdatedAt)
	return err
}

func (s *Store) GetV6ReservationByID(ctx context.Context, id string) (*models.V6Reservation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_id, group_id, duid, ip_addr, hostname, description, options, created_at, updated_at
		FROM v6_reservations WHERE id=$1
	`, id)
	return scanV6Reservation(row)
}

func (s *Store) GetV6ReservationByDUID(ctx context.Context, scopeID, duid string) (*models.V6Reservation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_id, group_id, duid, ip_addr, hostname, description, options, created_at, updated_at
		FROM v6_reservations WHERE scope_id=$1 AND duid=$2
	`, scopeID, duid)
	return scanV6Reservation(row)
}

func (s *Store) GetV6ReservationByIP(ctx context.Context, scopeID string, ip net.IP) (*models.V6Reservation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_id, group_id, duid, ip_addr, hostname, description, options, created_at, updated_at
		FROM v6_reservations WHERE scope_id=$1 AND ip_addr=regexp_replace(host($2), '^::ffff:', '')::inet
	`, scopeID, ip)
	return scanV6Reservation(row)
}

func (s *Store) ListV6ReservationsByScope(ctx context.Context, scopeID string) ([]*models.V6Reservation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, group_id, duid, ip_addr, hostname, description, options, created_at, updated_at
		FROM v6_reservations WHERE scope_id=$1 ORDER BY ip_addr
	`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := make([]*models.V6Reservation, 0)
	for rows.Next() {
		r, err := scanV6Reservation(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, rows.Err()
}

func (s *Store) DeleteV6Reservation(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM v6_reservations WHERE id=$1`, id)
	return err
}

func (s *Store) UpdateV6Reservation(ctx context.Context, r *models.V6Reservation) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE v6_reservations SET duid=$2, ip_addr=regexp_replace(host($3), '^::ffff:', '')::inet, group_id=$4, hostname=$5, description=$6, options=$7, updated_at=$8
		WHERE id=$1
	`, r.ID, r.DUID, r.IPAddr, nullableUUID(r.GroupID), r.Hostname, r.Description, r.Options, time.Now().UTC())
	return err
}

func (s *Store) ListV6ReservationsByScopePaged(ctx context.Context, scopeID string, offset, limit int) ([]*models.V6Reservation, int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM v6_reservations WHERE scope_id=$1`, scopeID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, group_id, duid, ip_addr, hostname, description, options, created_at, updated_at
		FROM v6_reservations WHERE scope_id=$1 ORDER BY ip_addr LIMIT $2 OFFSET $3
	`, scopeID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	res := make([]*models.V6Reservation, 0)
	for rows.Next() {
		r, err := scanV6Reservation(rows)
		if err != nil {
			return nil, 0, err
		}
		res = append(res, r)
	}
	return res, total, rows.Err()
}

func scanV6Reservation(row pgx.Row) (*models.V6Reservation, error) {
	var r models.V6Reservation
	var groupID, hostname, description *string
	err := row.Scan(&r.ID, &r.ScopeID, &groupID, &r.DUID, &r.IPAddr, &hostname, &description, &r.Options, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.IPAddr = to4IfPossible(r.IPAddr)
	if groupID != nil {
		r.GroupID = *groupID
	}
	if hostname != nil {
		r.Hostname = *hostname
	}
	if description != nil {
		r.Description = *description
	}
	return &r, nil
}

// ---------- Reservation Groups ----------

func (s *Store) CreateReservationGroup(ctx context.Context, g *models.ReservationGroup) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO reservation_groups (id, name, description, options, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, g.ID, g.Name, g.Description, g.Options, g.CreatedAt, g.UpdatedAt)
	return err
}

func (s *Store) GetReservationGroupByID(ctx context.Context, id string) (*models.ReservationGroup, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, name, description, options, created_at, updated_at
		FROM reservation_groups WHERE id=$1
	`, id)
	return scanReservationGroup(row)
}

func (s *Store) GetReservationGroupByName(ctx context.Context, name string) (*models.ReservationGroup, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, name, description, options, created_at, updated_at
		FROM reservation_groups WHERE name=$1
	`, name)
	return scanReservationGroup(row)
}

func (s *Store) ListReservationGroups(ctx context.Context) ([]*models.ReservationGroup, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, description, options, created_at, updated_at
		FROM reservation_groups ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := make([]*models.ReservationGroup, 0)
	for rows.Next() {
		g, err := scanReservationGroup(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, g)
	}
	return res, rows.Err()
}

func (s *Store) UpdateReservationGroup(ctx context.Context, g *models.ReservationGroup) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE reservation_groups SET name=$2, description=$3, options=$4, updated_at=$5
		WHERE id=$1
	`, g.ID, g.Name, g.Description, g.Options, time.Now().UTC())
	return err
}

func (s *Store) DeleteReservationGroup(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM reservation_groups WHERE id=$1`, id)
	return err
}

func scanReservationGroup(row pgx.Row) (*models.ReservationGroup, error) {
	var g models.ReservationGroup
	var description *string
	err := row.Scan(&g.ID, &g.Name, &description, &g.Options, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if description != nil {
		g.Description = *description
	}
	return &g, nil
}

// ---------- V6 Prefixes (PD) ----------

func (s *Store) GetV6PrefixByDUID(ctx context.Context, scopeID, duid, iaid string) (*models.V6Prefix, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_id, duid, iaid, prefix, preferred_lifetime, valid_lifetime, state, starts_at, ends_at, created_at, updated_at
		FROM v6_prefixes WHERE scope_id=$1 AND duid=$2 AND iaid=$3
	`, scopeID, duid, iaid)
	return scanV6Prefix(row)
}

func (s *Store) GetV6PrefixByPrefix(ctx context.Context, scopeID string, prefix *net.IPNet) (*models.V6Prefix, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_id, duid, iaid, prefix, preferred_lifetime, valid_lifetime, state, starts_at, ends_at, created_at, updated_at
		FROM v6_prefixes WHERE scope_id=$1 AND prefix=regexp_replace(text($2), '^::ffff:', '')::cidr
	`, scopeID, prefix)
	return scanV6Prefix(row)
}

func (s *Store) CreateOrUpdateV6Prefix(ctx context.Context, p *models.V6Prefix) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO v6_prefixes (id, scope_id, duid, iaid, prefix, preferred_lifetime, valid_lifetime, state, starts_at, ends_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, regexp_replace(text($5), '^::ffff:', '')::cidr, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (scope_id, duid, iaid) DO UPDATE SET
			prefix=regexp_replace(text(EXCLUDED.prefix), '^::ffff:', '')::cidr,
			preferred_lifetime=EXCLUDED.preferred_lifetime,
			valid_lifetime=EXCLUDED.valid_lifetime,
			state=EXCLUDED.state,
			starts_at=EXCLUDED.starts_at,
			ends_at=EXCLUDED.ends_at,
			updated_at=EXCLUDED.updated_at
	`, p.ID, p.ScopeID, p.DUID, p.IAID, p.Prefix, p.PreferredLifetime, p.ValidLifetime, string(p.State), p.StartsAt, p.EndsAt, p.CreatedAt, p.UpdatedAt)
	return err
}

func (s *Store) UpdateV6PrefixState(ctx context.Context, id string, state models.LeaseState) error {
	_, err := s.pool.Exec(ctx, `UPDATE v6_prefixes SET state=$2, updated_at=$3 WHERE id=$1`, id, string(state), time.Now().UTC())
	return err
}

func (s *Store) DeleteV6Prefix(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM v6_prefixes WHERE id=$1`, id)
	return err
}

func (s *Store) ListV6PrefixesByScope(ctx context.Context, scopeID string) ([]*models.V6Prefix, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_id, duid, iaid, prefix, preferred_lifetime, valid_lifetime, state, starts_at, ends_at, created_at, updated_at
		FROM v6_prefixes WHERE scope_id=$1 ORDER BY prefix
	`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	prefixes := make([]*models.V6Prefix, 0)
	for rows.Next() {
		p, err := scanV6Prefix(rows)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, p)
	}
	return prefixes, rows.Err()
}

func scanV6Prefix(row pgx.Row) (*models.V6Prefix, error) {
	var p models.V6Prefix
	var prefix *net.IPNet
	err := row.Scan(&p.ID, &p.ScopeID, &p.DUID, &p.IAID, &prefix, &p.PreferredLifetime, &p.ValidLifetime, &p.State, &p.StartsAt, &p.EndsAt, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.Prefix = prefix
	if p.Prefix != nil && p.Prefix.IP != nil {
		p.Prefix.IP = to4IfPossible(p.Prefix.IP)
	}
	return &p, nil
}

// ---------- MAC Blacklist ----------

func (s *Store) CreateMACBlacklist(ctx context.Context, b *models.MACBlacklist) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mac_blacklist (id, mac_addr, reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`, b.ID, b.MACAddr, b.Reason, b.CreatedAt, b.UpdatedAt)
	return err
}

func (s *Store) ListMACBlacklist(ctx context.Context) ([]*models.MACBlacklist, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, mac_addr, reason, created_at, updated_at FROM mac_blacklist ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]*models.MACBlacklist, 0)
	for rows.Next() {
		var b models.MACBlacklist
		var reason *string
		if err := rows.Scan(&b.ID, &b.MACAddr, &reason, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		if reason != nil {
			b.Reason = *reason
		}
		list = append(list, &b)
	}
	return list, rows.Err()
}

func (s *Store) DeleteMACBlacklist(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM mac_blacklist WHERE id=$1`, id)
	return err
}

func (s *Store) IsMACBlacklisted(ctx context.Context, mac string) (bool, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM mac_blacklist WHERE mac_addr=$1`, mac).Scan(&count)
	return count > 0, err
}

// ---------- Cluster-Aware Lease Allocation ----------

// AllocateV4Lease reserves an IPv4 address for the given MAC/client inside a
// PostgreSQL transaction protected by a scope-level advisory lock. This makes
// the allocation atomic across multiple active DHCP nodes sharing the same
// database.
func (s *Store) AllocateV4Lease(ctx context.Context, scope *models.Scope, mac, clientID string, preferred net.IP, hostname string, leaseTime, maxLeaseTime int) (*models.Lease, error) {
	if leaseTime == 0 {
		leaseTime = 3600
	}
	if maxLeaseTime == 0 {
		maxLeaseTime = 86400
	}
	if leaseTime > maxLeaseTime {
		leaseTime = maxLeaseTime
	}

	var lease *models.Lease
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		// Scope-level distributed lock. hashtextextended gives a stable 64-bit hash.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", scope.ID); err != nil {
			return err
		}

		now := time.Now().UTC()
		endsAt := now.Add(time.Duration(leaseTime) * time.Second)

		// 1. Find reservation (always wins when present).
		res, err := getReservationByMACTx(ctx, tx, scope.ID, mac)
		if err != nil {
			return err
		}

		// 2. Handle any existing lease for this MAC.
		existing, err := getLeaseByMACTx(ctx, tx, scope.ID, mac)
		if err != nil {
			return err
		}
		if existing != nil {
			if (existing.State == models.LeaseActive || existing.State == models.LeaseOffered) && ipInRange(existing.IPAddr, scope.StartIP, scope.EndIP) {
				if res == nil || ipKey(existing.IPAddr) == ipKey(res.IPAddr) {
					existing.Hostname = hostname
					existing.ClientID = clientID
					existing.State = models.LeaseOffered
					existing.OfferedAt = &now
					existing.StartsAt = now
					existing.EndsAt = endsAt
					existing.UpdatedAt = now
					lease = existing
					return saveLeaseTx(ctx, tx, lease)
				}
				// Reservation exists but points to a different IP; drop the old lease.
			}
			// Stale or out-of-range lease for this MAC; remove all rows before assigning a new IP.
			if err := deleteLeasesByMACTx(ctx, tx, scope.ID, mac); err != nil {
				return err
			}
		}

		// 3. Reservation wins for new or corrected leases.
		if res != nil {
			if err := ensureIPAvailableTx(ctx, tx, scope.ID, res.IPAddr, mac); err != nil {
				return err
			}
			lease = &models.Lease{
				ID:        uuid.New().String(),
				ScopeID:   scope.ID,
				MACAddr:   mac,
				IPAddr:    res.IPAddr,
				Hostname:  hostname,
				ClientID:  clientID,
				State:     models.LeaseOffered,
				OfferedAt: &now,
				StartsAt:  now,
				EndsAt:    endsAt,
				CreatedAt: now,
				UpdatedAt: now,
			}
			return saveLeaseTx(ctx, tx, lease)
		}

		// 4. Honor a preferred IP if it is free or belongs to this MAC.
		// For migration scenarios we also allow IPs inside the subnet but outside
		// the configured start/end range, as long as they are not excluded/gateway/DNS.
		if preferred != nil && scope.Subnet != nil && scope.Subnet.Contains(preferred) {
			occupied, err := getLeaseByIPTx(ctx, tx, scope.ID, preferred)
			if err != nil {
				return err
			}
			usable := occupied == nil || occupied.MACAddr == mac || (occupied.State != models.LeaseActive && occupied.State != models.LeaseOffered)
			inRange := ipInRange(preferred, scope.StartIP, scope.EndIP)
			if usable && (inRange || !isExcludedIP(scope, preferred)) {
				if occupied != nil && occupied.MACAddr != mac {
					if err := deleteLeaseTx(ctx, tx, occupied.ID); err != nil {
						return err
					}
				}
				lease = &models.Lease{
					ID:        uuid.New().String(),
					ScopeID:   scope.ID,
					MACAddr:   mac,
					IPAddr:    preferred,
					Hostname:  hostname,
					ClientID:  clientID,
					State:     models.LeaseOffered,
					OfferedAt: &now,
					StartsAt:  now,
					EndsAt:    endsAt,
					CreatedAt: now,
					UpdatedAt: now,
				}
				return saveLeaseTx(ctx, tx, lease)
			}
		}

		// 4. Allocate from the pool.
		ip, err := allocateIPv4Tx(ctx, tx, scope)
		if err != nil {
			return err
		}
		lease = &models.Lease{
			ID:        uuid.New().String(),
			ScopeID:   scope.ID,
			MACAddr:   mac,
			IPAddr:    ip,
			Hostname:  hostname,
			ClientID:  clientID,
			State:     models.LeaseOffered,
			OfferedAt: &now,
			StartsAt:  now,
			EndsAt:    endsAt,
			CreatedAt: now,
			UpdatedAt: now,
		}
		return saveLeaseTx(ctx, tx, lease)
	})
	return lease, err
}

// AllocateV6Lease reserves an IPv6 address for the given DUID/IAID inside a
// PostgreSQL transaction protected by a scope-level advisory lock.
func (s *Store) AllocateV6Lease(ctx context.Context, scope *models.Scope, duid, iaid, hostname string, preferred net.IP, leaseTime, maxLeaseTime int) (*models.V6Lease, error) {
	if leaseTime == 0 {
		leaseTime = 3600
	}
	if maxLeaseTime == 0 {
		maxLeaseTime = 86400
	}
	if leaseTime > maxLeaseTime {
		leaseTime = maxLeaseTime
	}

	var lease *models.V6Lease
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", scope.ID); err != nil {
			return err
		}

		now := time.Now().UTC()
		endsAt := now.Add(time.Duration(leaseTime) * time.Second)

		// 1. Find reservation (always wins when present).
		res, err := getV6ReservationByDUIDTx(ctx, tx, scope.ID, duid)
		if err != nil {
			return err
		}

		// 2. Handle any existing lease for this DUID/IAID.
		existing, err := getV6LeaseByDUIDTx(ctx, tx, scope.ID, duid, iaid)
		if err != nil {
			return err
		}
		if existing != nil {
			if (existing.State == models.LeaseActive || existing.State == models.LeaseOffered) && ipInRange(existing.IPAddr, scope.StartIP, scope.EndIP) {
				if res == nil || ipKey(existing.IPAddr) == ipKey(res.IPAddr) {
					existing.Hostname = hostname
					existing.State = models.LeaseOffered
					existing.OfferedAt = &now
					existing.StartsAt = now
					existing.EndsAt = endsAt
					existing.UpdatedAt = now
					lease = existing
					return saveV6LeaseTx(ctx, tx, lease)
				}
				// Reservation exists but points to a different IP; drop the old lease.
			}
			// Stale or out-of-range lease for this DUID/IAID; remove all rows before assigning a new IP.
			if err := deleteV6LeasesByDUIDTx(ctx, tx, scope.ID, duid, iaid); err != nil {
				return err
			}
		}

		// 3. Reservation wins for new or corrected leases.
		if res != nil {
			if err := ensureV6IPAvailableTx(ctx, tx, scope.ID, res.IPAddr, duid); err != nil {
				return err
			}
			lease = &models.V6Lease{
				ID:        uuid.New().String(),
				ScopeID:   scope.ID,
				DUID:      duid,
				IAID:      iaid,
				IPAddr:    res.IPAddr,
				Hostname:  hostname,
				State:     models.LeaseOffered,
				OfferedAt: &now,
				StartsAt:  now,
				EndsAt:    endsAt,
				CreatedAt: now,
				UpdatedAt: now,
			}
			return saveV6LeaseTx(ctx, tx, lease)
		}

		// 4. Honor a preferred IP if it is free or belongs to this DUID.
		// For migration scenarios we also allow IPs inside the subnet but outside
		// the configured start/end range, as long as they are not excluded/gateway/DNS.
		if preferred != nil && scope.Subnet != nil && scope.Subnet.Contains(preferred) {
			occupied, err := getV6LeaseByIPTx(ctx, tx, scope.ID, preferred)
			if err != nil {
				return err
			}
			usable := occupied == nil || occupied.DUID == duid || (occupied.State != models.LeaseActive && occupied.State != models.LeaseOffered)
			inRange := ipInRange(preferred, scope.StartIP, scope.EndIP)
			if usable && (inRange || !isExcludedIP(scope, preferred)) {
				if occupied != nil && occupied.DUID != duid {
					if err := deleteV6LeaseTx(ctx, tx, occupied.ID); err != nil {
						return err
					}
				}
				lease = &models.V6Lease{
					ID:        uuid.New().String(),
					ScopeID:   scope.ID,
					DUID:      duid,
					IAID:      iaid,
					IPAddr:    preferred,
					Hostname:  hostname,
					State:     models.LeaseOffered,
					OfferedAt: &now,
					StartsAt:  now,
					EndsAt:    endsAt,
					CreatedAt: now,
					UpdatedAt: now,
				}
				return saveV6LeaseTx(ctx, tx, lease)
			}
		}

		// 4. Allocate from the pool.
		ip, err := allocateIPv6Tx(ctx, tx, scope)
		if err != nil {
			return err
		}
		lease = &models.V6Lease{
			ID:        uuid.New().String(),
			ScopeID:   scope.ID,
			DUID:      duid,
			IAID:      iaid,
			IPAddr:    ip,
			Hostname:  hostname,
			State:     models.LeaseOffered,
			OfferedAt: &now,
			StartsAt:  now,
			EndsAt:    endsAt,
			CreatedAt: now,
			UpdatedAt: now,
		}
		return saveV6LeaseTx(ctx, tx, lease)
	})
	return lease, err
}

// AllocateV6Prefix reserves a delegated prefix for the given DUID/IAID inside a
// PostgreSQL transaction protected by a scope-level advisory lock.
func (s *Store) AllocateV6Prefix(ctx context.Context, scope *models.Scope, duid, iaid string, leaseTime, maxLeaseTime int) (*models.V6Prefix, error) {
	if leaseTime == 0 {
		leaseTime = 3600
	}
	if maxLeaseTime == 0 {
		maxLeaseTime = 86400
	}
	if leaseTime > maxLeaseTime {
		leaseTime = maxLeaseTime
	}

	var prefix *models.V6Prefix
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", scope.ID); err != nil {
			return err
		}

		now := time.Now().UTC()
		endsAt := now.Add(time.Duration(leaseTime) * time.Second)

		// Reuse existing active/offered prefix for this DUID/IAID.
		existing, err := getV6PrefixByDUIDTx(ctx, tx, scope.ID, duid, iaid)
		if err != nil {
			return err
		}
		if existing != nil && (existing.State == models.LeaseActive || existing.State == models.LeaseOffered) {
			existing.State = models.LeaseOffered
			existing.PreferredLifetime = leaseTime
			existing.ValidLifetime = leaseTime
			existing.StartsAt = now
			existing.EndsAt = endsAt
			existing.UpdatedAt = now
			prefix = existing
			return saveV6PrefixTx(ctx, tx, prefix)
		}

		p, err := allocatePrefixTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		prefix = &models.V6Prefix{
			ID:                uuid.New().String(),
			ScopeID:           scope.ID,
			DUID:              duid,
			IAID:              iaid,
			Prefix:            p,
			PreferredLifetime: leaseTime,
			ValidLifetime:     leaseTime,
			State:             models.LeaseOffered,
			StartsAt:          now,
			EndsAt:            endsAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		return saveV6PrefixTx(ctx, tx, prefix)
	})
	return prefix, err
}

// ---------- Transaction helpers ----------

func getReservationByMACTx(ctx context.Context, tx pgx.Tx, scopeID, mac string) (*models.Reservation, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, scope_id, group_id, mac_addr, ip_addr, hostname, description, options, created_at, updated_at
		FROM reservations WHERE scope_id=$1 AND mac_addr=$2
	`, scopeID, mac)
	r, err := scanReservation(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func getLeaseByMACTx(ctx context.Context, tx pgx.Tx, scopeID, mac string) (*models.Lease, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, scope_id, mac_addr, ip_addr, hostname, client_id, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM leases WHERE scope_id=$1 AND mac_addr=$2
	`, scopeID, mac)
	l, err := scanLease(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func getLeaseByIPTx(ctx context.Context, tx pgx.Tx, scopeID string, ip net.IP) (*models.Lease, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, scope_id, mac_addr, ip_addr, hostname, client_id, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM leases WHERE scope_id=$1 AND ip_addr=regexp_replace(host($2), '^::ffff:', '')::inet
	`, scopeID, ip)
	l, err := scanLease(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func saveLeaseTx(ctx context.Context, tx pgx.Tx, lease *models.Lease) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO leases (id, scope_id, mac_addr, ip_addr, hostname, client_id, state, offered_at, starts_at, ends_at, created_at, updated_at)
		VALUES ($1, $2, $3, regexp_replace(host($4), '^::ffff:', '')::inet, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (scope_id, ip_addr) DO UPDATE SET
			mac_addr=EXCLUDED.mac_addr,
			hostname=EXCLUDED.hostname,
			client_id=EXCLUDED.client_id,
			state=EXCLUDED.state,
			offered_at=EXCLUDED.offered_at,
			starts_at=EXCLUDED.starts_at,
			ends_at=EXCLUDED.ends_at,
			updated_at=EXCLUDED.updated_at
	`, lease.ID, lease.ScopeID, lease.MACAddr, lease.IPAddr, lease.Hostname, lease.ClientID, string(lease.State), lease.OfferedAt, lease.StartsAt, lease.EndsAt, lease.CreatedAt, lease.UpdatedAt)
	return err
}

func deleteLeaseTx(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `DELETE FROM leases WHERE id=$1`, id)
	return err
}

func deleteLeasesByMACTx(ctx context.Context, tx pgx.Tx, scopeID, mac string) error {
	_, err := tx.Exec(ctx, `DELETE FROM leases WHERE scope_id=$1 AND mac_addr=$2`, scopeID, mac)
	return err
}

func ensureIPAvailableTx(ctx context.Context, tx pgx.Tx, scopeID string, ip net.IP, mac string) error {
	existing, err := getLeaseByIPTx(ctx, tx, scopeID, ip)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}
	if existing.MACAddr == mac {
		return nil
	}
	if existing.State == models.LeaseActive || existing.State == models.LeaseOffered {
		return fmt.Errorf("reservation ip %s in use by %s (state %s)", ip, existing.MACAddr, existing.State)
	}
	return deleteLeaseTx(ctx, tx, existing.ID)
}

func allocateIPv4Tx(ctx context.Context, tx pgx.Tx, scope *models.Scope) (net.IP, error) {
	rows, err := tx.Query(ctx, `
		SELECT ip_addr, state FROM leases WHERE scope_id=$1
	`, scope.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := make(map[string]bool)
	for rows.Next() {
		var ip net.IP
		var state string
		if err := rows.Scan(&ip, &state); err != nil {
			return nil, err
		}
		if state == string(models.LeaseActive) || state == string(models.LeaseOffered) {
			used[ipKey(ip)] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	resRows, err := tx.Query(ctx, `SELECT ip_addr FROM reservations WHERE scope_id=$1`, scope.ID)
	if err != nil {
		return nil, err
	}
	defer resRows.Close()
	for resRows.Next() {
		var ip net.IP
		if err := resRows.Scan(&ip); err != nil {
			return nil, err
		}
		used[ipKey(ip)] = true
	}
	if err := resRows.Err(); err != nil {
		return nil, err
	}

	for _, ip := range scope.ExcludedIPs {
		used[ipKey(ip)] = true
	}

	start := ipToUint32(scope.StartIP.To4())
	end := ipToUint32(scope.EndIP.To4())
	for ip := start; ip <= end; ip++ {
		candidate := uint32ToIP(ip)
		if used[ipKey(candidate)] {
			continue
		}
		if candidate.Equal(scope.StartIP) || candidate.Equal(scope.EndIP) {
			continue
		}
		return candidate, nil
	}
	return nil, fmt.Errorf("no available ip in scope")
}

func getV6ReservationByDUIDTx(ctx context.Context, tx pgx.Tx, scopeID, duid string) (*models.V6Reservation, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, scope_id, group_id, duid, ip_addr, hostname, description, options, created_at, updated_at
		FROM v6_reservations WHERE scope_id=$1 AND duid=$2
	`, scopeID, duid)
	r, err := scanV6Reservation(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func getV6LeaseByDUIDTx(ctx context.Context, tx pgx.Tx, scopeID, duid, iaid string) (*models.V6Lease, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, scope_id, duid, iaid, ip_addr, hostname, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM v6_leases WHERE scope_id=$1 AND duid=$2 AND iaid=$3
	`, scopeID, duid, iaid)
	l, err := scanV6Lease(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func getV6LeaseByIPTx(ctx context.Context, tx pgx.Tx, scopeID string, ip net.IP) (*models.V6Lease, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, scope_id, duid, iaid, ip_addr, hostname, state, offered_at, starts_at, ends_at, created_at, updated_at
		FROM v6_leases WHERE scope_id=$1 AND ip_addr=regexp_replace(host($2), '^::ffff:', '')::inet
	`, scopeID, ip)
	l, err := scanV6Lease(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func saveV6LeaseTx(ctx context.Context, tx pgx.Tx, lease *models.V6Lease) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO v6_leases (id, scope_id, duid, iaid, ip_addr, hostname, state, offered_at, starts_at, ends_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, regexp_replace(host($5), '^::ffff:', '')::inet, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (scope_id, ip_addr) DO UPDATE SET
			duid=EXCLUDED.duid,
			iaid=EXCLUDED.iaid,
			hostname=EXCLUDED.hostname,
			state=EXCLUDED.state,
			offered_at=EXCLUDED.offered_at,
			starts_at=EXCLUDED.starts_at,
			ends_at=EXCLUDED.ends_at,
			updated_at=EXCLUDED.updated_at
	`, lease.ID, lease.ScopeID, lease.DUID, lease.IAID, lease.IPAddr, lease.Hostname, string(lease.State), lease.OfferedAt, lease.StartsAt, lease.EndsAt, lease.CreatedAt, lease.UpdatedAt)
	return err
}

func deleteV6LeaseTx(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `DELETE FROM v6_leases WHERE id=$1`, id)
	return err
}

func deleteV6LeasesByDUIDTx(ctx context.Context, tx pgx.Tx, scopeID, duid, iaid string) error {
	_, err := tx.Exec(ctx, `DELETE FROM v6_leases WHERE scope_id=$1 AND duid=$2 AND iaid=$3`, scopeID, duid, iaid)
	return err
}

func ensureV6IPAvailableTx(ctx context.Context, tx pgx.Tx, scopeID string, ip net.IP, duid string) error {
	existing, err := getV6LeaseByIPTx(ctx, tx, scopeID, ip)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}
	if existing.DUID == duid {
		return nil
	}
	if existing.State == models.LeaseActive || existing.State == models.LeaseOffered {
		return fmt.Errorf("reservation ip %s in use", ip)
	}
	return deleteV6LeaseTx(ctx, tx, existing.ID)
}

func allocateIPv6Tx(ctx context.Context, tx pgx.Tx, scope *models.Scope) (net.IP, error) {
	rows, err := tx.Query(ctx, `SELECT ip_addr, state FROM v6_leases WHERE scope_id=$1`, scope.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := make(map[string]bool)
	for rows.Next() {
		var ip net.IP
		var state string
		if err := rows.Scan(&ip, &state); err != nil {
			return nil, err
		}
		if state == string(models.LeaseActive) || state == string(models.LeaseOffered) {
			used[ipKey(ip)] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	resRows, err := tx.Query(ctx, `SELECT ip_addr FROM v6_reservations WHERE scope_id=$1`, scope.ID)
	if err != nil {
		return nil, err
	}
	defer resRows.Close()
	for resRows.Next() {
		var ip net.IP
		if err := resRows.Scan(&ip); err != nil {
			return nil, err
		}
		used[ipKey(ip)] = true
	}
	if err := resRows.Err(); err != nil {
		return nil, err
	}

	start := ipToUint128(scope.StartIP.To16())
	end := ipToUint128(scope.EndIP.To16())
	for ip := start; ip.Cmp(end) <= 0; ip = ipAddOne(ip) {
		candidate := uint128ToIP(ip)
		if used[ipKey(candidate)] {
			continue
		}
		if candidate.Equal(scope.StartIP) || candidate.Equal(scope.EndIP) {
			continue
		}
		return candidate, nil
	}
	return nil, fmt.Errorf("no available v6 ip in scope")
}

func getV6PrefixByDUIDTx(ctx context.Context, tx pgx.Tx, scopeID, duid, iaid string) (*models.V6Prefix, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, scope_id, duid, iaid, prefix, preferred_lifetime, valid_lifetime, state, starts_at, ends_at, created_at, updated_at
		FROM v6_prefixes WHERE scope_id=$1 AND duid=$2 AND iaid=$3
	`, scopeID, duid, iaid)
	p, err := scanV6Prefix(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func saveV6PrefixTx(ctx context.Context, tx pgx.Tx, p *models.V6Prefix) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO v6_prefixes (id, scope_id, duid, iaid, prefix, preferred_lifetime, valid_lifetime, state, starts_at, ends_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, regexp_replace(text($5), '^::ffff:', '')::cidr, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (scope_id, duid, iaid) DO UPDATE SET
			prefix=regexp_replace(text(EXCLUDED.prefix), '^::ffff:', '')::cidr,
			preferred_lifetime=EXCLUDED.preferred_lifetime,
			valid_lifetime=EXCLUDED.valid_lifetime,
			state=EXCLUDED.state,
			starts_at=EXCLUDED.starts_at,
			ends_at=EXCLUDED.ends_at,
			updated_at=EXCLUDED.updated_at
	`, p.ID, p.ScopeID, p.DUID, p.IAID, p.Prefix, p.PreferredLifetime, p.ValidLifetime, string(p.State), p.StartsAt, p.EndsAt, p.CreatedAt, p.UpdatedAt)
	return err
}

func allocatePrefixTx(ctx context.Context, tx pgx.Tx, scope *models.Scope) (*net.IPNet, error) {
	if scope.Prefix == nil {
		return nil, fmt.Errorf("scope has no prefix configured")
	}
	delegatedLen := 64
	parentLen, _ := scope.Prefix.Mask.Size()
	if delegatedLen <= parentLen {
		return nil, fmt.Errorf("delegated prefix length must be larger than parent")
	}
	count := 1 << (delegatedLen - parentLen)

	rows, err := tx.Query(ctx, `SELECT prefix, state FROM v6_prefixes WHERE scope_id=$1`, scope.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := make(map[string]bool)
	for rows.Next() {
		var prefix *net.IPNet
		var state string
		if err := rows.Scan(&prefix, &state); err != nil {
			return nil, err
		}
		if state == string(models.LeaseActive) || state == string(models.LeaseOffered) {
			used[prefix.String()] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	base := ipToUint128(scope.Prefix.IP.To16())
	step := uint64(1) << (delegatedLen - parentLen)
	for i := 0; i < count; i++ {
		ip := uint128ToIP(ipAddN(base, uint64(i)*step))
		ip[len(ip)-1] = 0
		candidate := &net.IPNet{IP: ip, Mask: net.CIDRMask(delegatedLen, 128)}
		if used[candidate.String()] {
			continue
		}
		return candidate, nil
	}
	return nil, fmt.Errorf("no available prefix")
}

// ---------- IP helpers duplicated from dhcp packages for store use ----------

func ipInRange(ip, start, end net.IP) bool {
	ipU := ipToUint128(ip.To16())
	startU := ipToUint128(start.To16())
	endU := ipToUint128(end.To16())
	return ipU.Cmp(startU) >= 0 && ipU.Cmp(endU) <= 0
}

// isExcludedIP reports whether ip is reserved for scope infrastructure
// (gateway, dns) or explicitly excluded.
func isExcludedIP(scope *models.Scope, ip net.IP) bool {
	key := ipKey(ip)
	for _, ex := range scope.ExcludedIPs {
		if ipKey(ex) == key {
			return true
		}
	}
	for _, gw := range scope.Gateway {
		if ipKey(gw) == key {
			return true
		}
	}
	for _, dns := range scope.DNS {
		if ipKey(dns) == key {
			return true
		}
	}
	return false
}

func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32ToIP(v uint32) net.IP {
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func ipToUint128(ip net.IP) *bigInt128 {
	ip = ip.To16()
	return &bigInt128{hi: binary.BigEndian.Uint64(ip[0:8]), lo: binary.BigEndian.Uint64(ip[8:16])}
}

func uint128ToIP(v *bigInt128) net.IP {
	ip := make([]byte, 16)
	binary.BigEndian.PutUint64(ip[0:8], v.hi)
	binary.BigEndian.PutUint64(ip[8:16], v.lo)
	return net.IP(ip)
}

type bigInt128 struct {
	hi, lo uint64
}

func (b *bigInt128) Cmp(other *bigInt128) int {
	if b.hi < other.hi {
		return -1
	}
	if b.hi > other.hi {
		return 1
	}
	if b.lo < other.lo {
		return -1
	}
	if b.lo > other.lo {
		return 1
	}
	return 0
}

func ipAddOne(v *bigInt128) *bigInt128 {
	out := &bigInt128{hi: v.hi, lo: v.lo}
	if out.lo == ^uint64(0) {
		out.lo = 0
		out.hi++
	} else {
		out.lo++
	}
	return out
}

func ipAddN(v *bigInt128, n uint64) *bigInt128 {
	out := &bigInt128{hi: v.hi, lo: v.lo}
	out.lo += n
	if out.lo < v.lo {
		out.hi++
	}
	return out
}
