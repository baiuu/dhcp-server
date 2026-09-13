#!/bin/bash
# rebuild-schema.sh
#
# 在保留全部数据的前提下，把数据库结构对齐到当前代码的最终 schema。
#
# 适用场景：数据库是很旧的版本（当时的迁移未跑全，或早期版本的 CREATE TABLE
# 缺少后期列，如 ha_nodes.cluster_id），直接启动新版服务报
#   "column xxx does not exist" / "no migration found for version N"
#
# 脚本做四件事，全部幂等，可重复执行：
#   1. CREATE TABLE IF NOT EXISTS  —— 补齐缺失的表
#   2. ALTER TABLE ADD COLUMN IF NOT EXISTS —— 逐列补齐已有表的缺失列
#   3. CREATE [UNIQUE] INDEX IF NOT EXISTS —— 补齐索引与唯一约束
#   4. 数据归一化、迁移版本对齐（version=2）、僵尸租约清理、绑定冲突释放
#
# 用法:
#   sudo ./scripts/rebuild-schema.sh [database_url]
# 默认: postgres://dhcp@localhost:5432/dhcpdb?sslmode=disable
# 免密方式: sudo -u postgres ./scripts/rebuild-schema.sh "postgres:///dhcpdb?host=/var/run/postgresql"
#
# 建议先备份: sudo -u postgres pg_dump dhcpdb > dhcpdb-backup-$(date +%F).sql

set -euo pipefail

DB_URL="${1:-postgres://dhcp@localhost:5432/dhcpdb?sslmode=disable}"

if ! command -v psql >/dev/null 2>&1; then
    echo "ERROR: psql not found. Install postgresql-client first."
    exit 1
fi

echo "==> Database: ${DB_URL%%\?*}"
echo "==> 正在对齐 schema（数据保留，不会被删除）..."
echo

psql -v ON_ERROR_STOP=1 "$DB_URL" <<'SQL'
\set ECHO none
\timing off

\echo '=== 1. 补齐表结构 ==='

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username VARCHAR(64) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(16) NOT NULL DEFAULT 'admin',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS scopes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(128) UNIQUE NOT NULL,
    subnet CIDR NOT NULL,
    start_ip INET NOT NULL,
    end_ip INET NOT NULL,
    gateway INET[] DEFAULT '{}',
    dns INET[] DEFAULT '{}',
    domain_name VARCHAR(255) DEFAULT '',
    lease_time INTEGER NOT NULL DEFAULT 3600,
    max_lease_time INTEGER NOT NULL DEFAULT 86400,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    options JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    v6 BOOLEAN NOT NULL DEFAULT FALSE,
    prefix CIDR DEFAULT NULL,
    excluded_ips INET[] DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS reservation_groups (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(128) UNIQUE NOT NULL,
    description TEXT DEFAULT '',
    options JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS reservations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    scope_id UUID NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    group_id UUID REFERENCES reservation_groups(id) ON DELETE SET NULL,
    mac_addr VARCHAR(17) NOT NULL,
    ip_addr INET NOT NULL,
    hostname VARCHAR(255) DEFAULT '',
    description TEXT DEFAULT '',
    options JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(scope_id, mac_addr),
    UNIQUE(scope_id, ip_addr)
);

CREATE TABLE IF NOT EXISTS leases (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    scope_id UUID NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    mac_addr VARCHAR(17) NOT NULL,
    ip_addr INET NOT NULL,
    hostname VARCHAR(255) DEFAULT '',
    client_id TEXT DEFAULT '',
    state VARCHAR(16) NOT NULL DEFAULT 'active',
    offered_at TIMESTAMPTZ,
    starts_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ends_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(scope_id, mac_addr),
    UNIQUE(scope_id, ip_addr)
);

CREATE TABLE IF NOT EXISTS ha_nodes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    cluster_id VARCHAR(64) NOT NULL DEFAULT 'default',
    node_id VARCHAR(64) UNIQUE NOT NULL,
    role VARCHAR(16) NOT NULL DEFAULT 'active',
    listen_addr VARCHAR(64),
    version VARCHAR(32),
    healthy BOOLEAN NOT NULL DEFAULT TRUE,
    last_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username VARCHAR(64),
    action VARCHAR(64) NOT NULL,
    resource VARCHAR(64) NOT NULL,
    resource_id UUID,
    details JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS v6_leases (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    scope_id UUID NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    duid TEXT NOT NULL,
    iaid TEXT NOT NULL,
    ip_addr INET NOT NULL,
    hostname VARCHAR(255) DEFAULT '',
    state VARCHAR(16) NOT NULL DEFAULT 'active',
    offered_at TIMESTAMPTZ,
    starts_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ends_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(scope_id, duid, iaid),
    UNIQUE(scope_id, ip_addr)
);

CREATE TABLE IF NOT EXISTS v6_prefixes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    scope_id UUID NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    duid TEXT NOT NULL,
    iaid TEXT NOT NULL,
    prefix CIDR NOT NULL,
    preferred_lifetime INTEGER NOT NULL DEFAULT 3600,
    valid_lifetime INTEGER NOT NULL DEFAULT 3600,
    state VARCHAR(16) NOT NULL DEFAULT 'active',
    starts_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ends_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(scope_id, duid, iaid),
    UNIQUE(scope_id, prefix)
);

CREATE TABLE IF NOT EXISTS v6_reservations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    scope_id UUID NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    group_id UUID REFERENCES reservation_groups(id) ON DELETE SET NULL,
    duid TEXT NOT NULL,
    ip_addr INET NOT NULL,
    hostname VARCHAR(255) DEFAULT '',
    description TEXT DEFAULT '',
    options JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(scope_id, duid),
    UNIQUE(scope_id, ip_addr)
);

CREATE TABLE IF NOT EXISTS mac_blacklist (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    mac_addr TEXT NOT NULL UNIQUE,
    reason TEXT DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ip_allocation_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    node_id VARCHAR(64) DEFAULT '',
    scope_id UUID REFERENCES scopes(id) ON DELETE SET NULL,
    scope_name VARCHAR(128) DEFAULT '',
    mac_addr VARCHAR(17) DEFAULT '',
    duid TEXT DEFAULT '',
    iaid TEXT DEFAULT '',
    ip_addr INET,
    prefix CIDR,
    action VARCHAR(16) NOT NULL,
    client_info JSONB DEFAULT '{}',
    relay_ip INET,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS system_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    node_id VARCHAR(64) DEFAULT '',
    level VARCHAR(16) NOT NULL,
    message TEXT NOT NULL,
    attrs JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS server_state (
    key VARCHAR(64) PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

\echo '=== 2. 补齐已有表的缺失列 ==='

-- users
ALTER TABLE users ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE users ADD COLUMN IF NOT EXISTS username VARCHAR(64);
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash VARCHAR(255);
ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(16) DEFAULT 'admin';
ALTER TABLE users ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE users ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

-- scopes
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS name VARCHAR(128);
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS subnet CIDR;
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS start_ip INET;
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS end_ip INET;
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS gateway INET[] DEFAULT '{}';
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS dns INET[] DEFAULT '{}';
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS domain_name VARCHAR(255) DEFAULT '';
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS lease_time INTEGER DEFAULT 3600;
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS max_lease_time INTEGER DEFAULT 86400;
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS enabled BOOLEAN DEFAULT TRUE;
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS options JSONB DEFAULT '{}';
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS v6 BOOLEAN DEFAULT FALSE;
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS prefix CIDR DEFAULT NULL;
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS excluded_ips INET[] DEFAULT '{}';

-- reservation_groups
ALTER TABLE reservation_groups ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE reservation_groups ADD COLUMN IF NOT EXISTS name VARCHAR(128);
ALTER TABLE reservation_groups ADD COLUMN IF NOT EXISTS description TEXT DEFAULT '';
ALTER TABLE reservation_groups ADD COLUMN IF NOT EXISTS options JSONB DEFAULT '{}';
ALTER TABLE reservation_groups ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE reservation_groups ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

-- reservations
ALTER TABLE reservations ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE reservations ADD COLUMN IF NOT EXISTS scope_id UUID REFERENCES scopes(id) ON DELETE CASCADE;
ALTER TABLE reservations ADD COLUMN IF NOT EXISTS group_id UUID REFERENCES reservation_groups(id) ON DELETE SET NULL;
ALTER TABLE reservations ADD COLUMN IF NOT EXISTS mac_addr VARCHAR(17);
ALTER TABLE reservations ADD COLUMN IF NOT EXISTS ip_addr INET;
ALTER TABLE reservations ADD COLUMN IF NOT EXISTS hostname VARCHAR(255) DEFAULT '';
ALTER TABLE reservations ADD COLUMN IF NOT EXISTS description TEXT DEFAULT '';
ALTER TABLE reservations ADD COLUMN IF NOT EXISTS options JSONB DEFAULT '{}';
ALTER TABLE reservations ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE reservations ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

-- leases
ALTER TABLE leases ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE leases ADD COLUMN IF NOT EXISTS scope_id UUID REFERENCES scopes(id) ON DELETE CASCADE;
ALTER TABLE leases ADD COLUMN IF NOT EXISTS mac_addr VARCHAR(17);
ALTER TABLE leases ADD COLUMN IF NOT EXISTS ip_addr INET;
ALTER TABLE leases ADD COLUMN IF NOT EXISTS hostname VARCHAR(255) DEFAULT '';
ALTER TABLE leases ADD COLUMN IF NOT EXISTS client_id TEXT DEFAULT '';
ALTER TABLE leases ADD COLUMN IF NOT EXISTS state VARCHAR(16) DEFAULT 'active';
ALTER TABLE leases ADD COLUMN IF NOT EXISTS offered_at TIMESTAMPTZ;
ALTER TABLE leases ADD COLUMN IF NOT EXISTS starts_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE leases ADD COLUMN IF NOT EXISTS ends_at TIMESTAMPTZ;
ALTER TABLE leases ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE leases ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

-- ha_nodes（旧库最常缺的表：cluster_id / listen_addr / version 等）
ALTER TABLE ha_nodes ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE ha_nodes ADD COLUMN IF NOT EXISTS cluster_id VARCHAR(64) DEFAULT 'default';
ALTER TABLE ha_nodes ADD COLUMN IF NOT EXISTS node_id VARCHAR(64);
ALTER TABLE ha_nodes ADD COLUMN IF NOT EXISTS role VARCHAR(16) DEFAULT 'active';
ALTER TABLE ha_nodes ADD COLUMN IF NOT EXISTS listen_addr VARCHAR(64) DEFAULT '';
ALTER TABLE ha_nodes ADD COLUMN IF NOT EXISTS version VARCHAR(32) DEFAULT '';
ALTER TABLE ha_nodes ADD COLUMN IF NOT EXISTS healthy BOOLEAN DEFAULT TRUE;
ALTER TABLE ha_nodes ADD COLUMN IF NOT EXISTS last_seen TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE ha_nodes ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE ha_nodes ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

-- audit_logs
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS username VARCHAR(64);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS action VARCHAR(64);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS resource VARCHAR(64);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS resource_id UUID;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS details JSONB DEFAULT '{}';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();

-- v6_leases
ALTER TABLE v6_leases ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE v6_leases ADD COLUMN IF NOT EXISTS scope_id UUID REFERENCES scopes(id) ON DELETE CASCADE;
ALTER TABLE v6_leases ADD COLUMN IF NOT EXISTS duid TEXT;
ALTER TABLE v6_leases ADD COLUMN IF NOT EXISTS iaid TEXT;
ALTER TABLE v6_leases ADD COLUMN IF NOT EXISTS ip_addr INET;
ALTER TABLE v6_leases ADD COLUMN IF NOT EXISTS hostname VARCHAR(255) DEFAULT '';
ALTER TABLE v6_leases ADD COLUMN IF NOT EXISTS state VARCHAR(16) DEFAULT 'active';
ALTER TABLE v6_leases ADD COLUMN IF NOT EXISTS offered_at TIMESTAMPTZ;
ALTER TABLE v6_leases ADD COLUMN IF NOT EXISTS starts_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE v6_leases ADD COLUMN IF NOT EXISTS ends_at TIMESTAMPTZ;
ALTER TABLE v6_leases ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE v6_leases ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

-- v6_prefixes
ALTER TABLE v6_prefixes ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE v6_prefixes ADD COLUMN IF NOT EXISTS scope_id UUID REFERENCES scopes(id) ON DELETE CASCADE;
ALTER TABLE v6_prefixes ADD COLUMN IF NOT EXISTS duid TEXT;
ALTER TABLE v6_prefixes ADD COLUMN IF NOT EXISTS iaid TEXT;
ALTER TABLE v6_prefixes ADD COLUMN IF NOT EXISTS prefix CIDR;
ALTER TABLE v6_prefixes ADD COLUMN IF NOT EXISTS preferred_lifetime INTEGER DEFAULT 3600;
ALTER TABLE v6_prefixes ADD COLUMN IF NOT EXISTS valid_lifetime INTEGER DEFAULT 3600;
ALTER TABLE v6_prefixes ADD COLUMN IF NOT EXISTS state VARCHAR(16) DEFAULT 'active';
ALTER TABLE v6_prefixes ADD COLUMN IF NOT EXISTS starts_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE v6_prefixes ADD COLUMN IF NOT EXISTS ends_at TIMESTAMPTZ;
ALTER TABLE v6_prefixes ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE v6_prefixes ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

-- v6_reservations
ALTER TABLE v6_reservations ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE v6_reservations ADD COLUMN IF NOT EXISTS scope_id UUID REFERENCES scopes(id) ON DELETE CASCADE;
ALTER TABLE v6_reservations ADD COLUMN IF NOT EXISTS group_id UUID REFERENCES reservation_groups(id) ON DELETE SET NULL;
ALTER TABLE v6_reservations ADD COLUMN IF NOT EXISTS duid TEXT;
ALTER TABLE v6_reservations ADD COLUMN IF NOT EXISTS ip_addr INET;
ALTER TABLE v6_reservations ADD COLUMN IF NOT EXISTS hostname VARCHAR(255) DEFAULT '';
ALTER TABLE v6_reservations ADD COLUMN IF NOT EXISTS description TEXT DEFAULT '';
ALTER TABLE v6_reservations ADD COLUMN IF NOT EXISTS options JSONB DEFAULT '{}';
ALTER TABLE v6_reservations ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE v6_reservations ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

-- mac_blacklist
ALTER TABLE mac_blacklist ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE mac_blacklist ADD COLUMN IF NOT EXISTS mac_addr TEXT;
ALTER TABLE mac_blacklist ADD COLUMN IF NOT EXISTS reason TEXT DEFAULT '';
ALTER TABLE mac_blacklist ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE mac_blacklist ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

-- ip_allocation_logs
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS node_id VARCHAR(64) DEFAULT '';
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS scope_id UUID REFERENCES scopes(id) ON DELETE SET NULL;
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS scope_name VARCHAR(128) DEFAULT '';
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS mac_addr VARCHAR(17) DEFAULT '';
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS duid TEXT DEFAULT '';
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS iaid TEXT DEFAULT '';
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS ip_addr INET;
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS prefix CIDR;
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS action VARCHAR(16);
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS client_info JSONB DEFAULT '{}';
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS relay_ip INET;
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();

-- system_logs
ALTER TABLE system_logs ADD COLUMN IF NOT EXISTS id UUID DEFAULT uuid_generate_v4();
ALTER TABLE system_logs ADD COLUMN IF NOT EXISTS node_id VARCHAR(64) DEFAULT '';
ALTER TABLE system_logs ADD COLUMN IF NOT EXISTS level VARCHAR(16);
ALTER TABLE system_logs ADD COLUMN IF NOT EXISTS message TEXT;
ALTER TABLE system_logs ADD COLUMN IF NOT EXISTS attrs JSONB DEFAULT '{}';
ALTER TABLE system_logs ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();

-- server_state
ALTER TABLE server_state ADD COLUMN IF NOT EXISTS value TEXT;
ALTER TABLE server_state ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

\echo '=== 3. 补齐索引与唯一约束 ==='

CREATE UNIQUE INDEX IF NOT EXISTS users_username_key ON users(username);
CREATE UNIQUE INDEX IF NOT EXISTS scopes_name_key ON scopes(name);
CREATE UNIQUE INDEX IF NOT EXISTS reservation_groups_name_key ON reservation_groups(name);
CREATE UNIQUE INDEX IF NOT EXISTS reservations_scope_id_mac_addr_key ON reservations(scope_id, mac_addr);
CREATE UNIQUE INDEX IF NOT EXISTS reservations_scope_id_ip_addr_key ON reservations(scope_id, ip_addr);
CREATE UNIQUE INDEX IF NOT EXISTS leases_scope_id_mac_addr_key ON leases(scope_id, mac_addr);
CREATE UNIQUE INDEX IF NOT EXISTS leases_scope_id_ip_addr_key ON leases(scope_id, ip_addr);
CREATE UNIQUE INDEX IF NOT EXISTS ha_nodes_node_id_key ON ha_nodes(node_id);
CREATE UNIQUE INDEX IF NOT EXISTS v6_leases_scope_id_duid_iaid_key ON v6_leases(scope_id, duid, iaid);
CREATE UNIQUE INDEX IF NOT EXISTS v6_leases_scope_id_ip_addr_key ON v6_leases(scope_id, ip_addr);
CREATE UNIQUE INDEX IF NOT EXISTS v6_prefixes_scope_id_duid_iaid_key ON v6_prefixes(scope_id, duid, iaid);
CREATE UNIQUE INDEX IF NOT EXISTS v6_prefixes_scope_id_prefix_key ON v6_prefixes(scope_id, prefix);
CREATE UNIQUE INDEX IF NOT EXISTS v6_reservations_scope_id_duid_key ON v6_reservations(scope_id, duid);
CREATE UNIQUE INDEX IF NOT EXISTS v6_reservations_scope_id_ip_addr_key ON v6_reservations(scope_id, ip_addr);
CREATE UNIQUE INDEX IF NOT EXISTS mac_blacklist_mac_addr_key ON mac_blacklist(mac_addr);

CREATE INDEX IF NOT EXISTS idx_leases_mac ON leases(mac_addr);
CREATE INDEX IF NOT EXISTS idx_leases_ip ON leases(ip_addr);
CREATE INDEX IF NOT EXISTS idx_leases_scope_state ON leases(scope_id, state);
CREATE INDEX IF NOT EXISTS idx_leases_ends_at ON leases(ends_at);
CREATE INDEX IF NOT EXISTS idx_ha_nodes_cluster ON ha_nodes(cluster_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_v6_leases_duid ON v6_leases(duid);
CREATE INDEX IF NOT EXISTS idx_v6_leases_ip ON v6_leases(ip_addr);
CREATE INDEX IF NOT EXISTS idx_v6_leases_scope_state ON v6_leases(scope_id, state);
CREATE INDEX IF NOT EXISTS idx_v6_prefixes_duid ON v6_prefixes(duid);
CREATE INDEX IF NOT EXISTS idx_v6_prefixes_prefix ON v6_prefixes(prefix);
CREATE INDEX IF NOT EXISTS idx_v6_prefixes_scope_state ON v6_prefixes(scope_id, state);
CREATE INDEX IF NOT EXISTS idx_mac_blacklist_mac ON mac_blacklist(mac_addr);
CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_created_at ON ip_allocation_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_scope_id ON ip_allocation_logs(scope_id);
CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_mac_addr ON ip_allocation_logs(mac_addr);
CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_ip_addr ON ip_allocation_logs(ip_addr);
CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_action ON ip_allocation_logs(action);
CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_node_id ON ip_allocation_logs(node_id);
CREATE INDEX IF NOT EXISTS idx_system_logs_created_at ON system_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_system_logs_level ON system_logs(level);
CREATE INDEX IF NOT EXISTS idx_system_logs_node_id ON system_logs(node_id);

\echo '=== 4. 数据修正（NULL 归一、IPv4 表示归一） ==='

-- NULL 字符串归一（对应迁移 007）
UPDATE reservations SET hostname = '' WHERE hostname IS NULL;
UPDATE reservations SET description = '' WHERE description IS NULL;
UPDATE v6_reservations SET hostname = '' WHERE hostname IS NULL;
UPDATE v6_reservations SET description = '' WHERE description IS NULL;
UPDATE leases SET hostname = '' WHERE hostname IS NULL;
UPDATE leases SET client_id = '' WHERE client_id IS NULL;
UPDATE v6_leases SET hostname = '' WHERE hostname IS NULL;
UPDATE mac_blacklist SET reason = '' WHERE reason IS NULL;
UPDATE reservation_groups SET description = '' WHERE description IS NULL;
UPDATE ha_nodes SET listen_addr = '' WHERE listen_addr IS NULL;
UPDATE ha_nodes SET version = '' WHERE version IS NULL;
UPDATE scopes SET domain_name = '' WHERE domain_name IS NULL;

ALTER TABLE reservations ALTER COLUMN hostname SET NOT NULL, ALTER COLUMN hostname SET DEFAULT '';
ALTER TABLE reservations ALTER COLUMN description SET NOT NULL, ALTER COLUMN description SET DEFAULT '';
ALTER TABLE v6_reservations ALTER COLUMN hostname SET NOT NULL, ALTER COLUMN hostname SET DEFAULT '';
ALTER TABLE v6_reservations ALTER COLUMN description SET NOT NULL, ALTER COLUMN description SET DEFAULT '';
ALTER TABLE leases ALTER COLUMN hostname SET NOT NULL, ALTER COLUMN hostname SET DEFAULT '';
ALTER TABLE leases ALTER COLUMN client_id SET NOT NULL, ALTER COLUMN client_id SET DEFAULT '';
ALTER TABLE v6_leases ALTER COLUMN hostname SET NOT NULL, ALTER COLUMN hostname SET DEFAULT '';
ALTER TABLE mac_blacklist ALTER COLUMN reason SET NOT NULL, ALTER COLUMN reason SET DEFAULT '';
ALTER TABLE reservation_groups ALTER COLUMN description SET NOT NULL, ALTER COLUMN description SET DEFAULT '';
ALTER TABLE ha_nodes ALTER COLUMN listen_addr SET NOT NULL, ALTER COLUMN listen_addr SET DEFAULT '';
ALTER TABLE ha_nodes ALTER COLUMN version SET NOT NULL, ALTER COLUMN version SET DEFAULT '';
ALTER TABLE scopes ALTER COLUMN domain_name SET NOT NULL, ALTER COLUMN domain_name SET DEFAULT '';

-- IPv4-mapped 地址归一（对应迁移 008-010）
UPDATE scopes SET start_ip = regexp_replace(host(start_ip), '^::ffff:', '')::inet WHERE text(start_ip) LIKE '::ffff:%';
UPDATE scopes SET end_ip   = regexp_replace(host(end_ip),   '^::ffff:', '')::inet WHERE text(end_ip)   LIKE '::ffff:%';
UPDATE scopes SET
    subnet = regexp_replace(text(subnet), '^::ffff:', '')::cidr,
    prefix = CASE WHEN prefix IS NULL THEN NULL ELSE regexp_replace(text(prefix), '^::ffff:', '')::cidr END,
    gateway = COALESCE((SELECT array_agg(regexp_replace(host(x), '^::ffff:', '')::inet) FROM unnest(gateway) x), '{}'::inet[]),
    dns = COALESCE((SELECT array_agg(regexp_replace(host(x), '^::ffff:', '')::inet) FROM unnest(dns) x), '{}'::inet[]),
    excluded_ips = COALESCE((SELECT array_agg(regexp_replace(host(x), '^::ffff:', '')::inet) FROM unnest(excluded_ips) x), '{}'::inet[])
WHERE text(subnet) LIKE '::ffff:%' OR text(prefix) LIKE '::ffff:%'
   OR EXISTS (SELECT 1 FROM unnest(gateway) x WHERE text(x) LIKE '::ffff:%')
   OR EXISTS (SELECT 1 FROM unnest(dns) x WHERE text(x) LIKE '::ffff:%')
   OR EXISTS (SELECT 1 FROM unnest(excluded_ips) x WHERE text(x) LIKE '::ffff:%');

UPDATE reservations    SET ip_addr = regexp_replace(host(ip_addr), '^::ffff:', '')::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE leases          SET ip_addr = regexp_replace(host(ip_addr), '^::ffff:', '')::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_reservations SET ip_addr = regexp_replace(host(ip_addr), '^::ffff:', '')::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_leases       SET ip_addr = regexp_replace(host(ip_addr), '^::ffff:', '')::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_prefixes     SET prefix  = regexp_replace(text(prefix), '^::ffff:', '')::cidr  WHERE text(prefix)  LIKE '::ffff:%';

\echo '=== 5. 迁移版本对齐 ==='

-- 数据库结构已对齐当前全部迁移（001 + 002），直接登记为最新版本
DELETE FROM schema_migrations;
INSERT INTO schema_migrations (version, dirty) VALUES (2, false);

SELECT version, dirty FROM schema_migrations;

\echo '=== 6. 僵尸租约清理 ==='

UPDATE leases SET state='expired', updated_at=NOW()
WHERE (state='offered' AND COALESCE(offered_at, starts_at) < NOW() - INTERVAL '120 seconds')
   OR (state='active' AND ends_at < NOW());

UPDATE v6_leases SET state='expired', updated_at=NOW()
WHERE (state='offered' AND COALESCE(offered_at, starts_at) < NOW() - INTERVAL '120 seconds')
   OR (state='active' AND ends_at < NOW());

UPDATE v6_prefixes SET state='expired', updated_at=NOW()
WHERE (state='offered' AND starts_at < NOW() - INTERVAL '120 seconds')
   OR (state='active' AND ends_at < NOW());

\echo '=== 7. 释放被错误占用的绑定地址 ==='

UPDATE leases l SET state='released', updated_at=NOW()
FROM reservations r
WHERE r.scope_id = l.scope_id AND r.ip_addr = l.ip_addr
  AND l.state IN ('active','offered') AND l.mac_addr <> r.mac_addr;

UPDATE v6_leases l SET state='released', updated_at=NOW()
FROM v6_reservations r
WHERE r.scope_id = l.scope_id AND r.ip_addr = l.ip_addr
  AND l.state IN ('active','offered') AND l.duid <> r.duid;

\echo '=== 8. 当前租约状态统计 ==='

SELECT 'leases(v4)' AS tbl, state, COUNT(*) FROM leases GROUP BY state
UNION ALL SELECT 'v6_leases', state, COUNT(*) FROM v6_leases GROUP BY state
UNION ALL SELECT 'v6_prefixes', state, COUNT(*) FROM v6_prefixes GROUP BY state
ORDER BY 1, 2;

\echo ''
\echo '=== 完成。启动服务: sudo systemctl restart dhcp-server ==='
SQL
