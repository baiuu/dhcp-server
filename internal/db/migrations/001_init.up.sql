-- 001_init.up.sql
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS reservations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    scope_id UUID NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
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

CREATE INDEX idx_leases_mac ON leases(mac_addr);
CREATE INDEX idx_leases_ip ON leases(ip_addr);
CREATE INDEX idx_leases_scope_state ON leases(scope_id, state);
CREATE INDEX idx_leases_ends_at ON leases(ends_at);

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

CREATE INDEX IF NOT EXISTS idx_ha_nodes_cluster ON ha_nodes(cluster_id);

CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username VARCHAR(64),
    action VARCHAR(64) NOT NULL,
    resource VARCHAR(64) NOT NULL,
    resource_id UUID,
    details JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);


-- 002_dhcpv6.up.sql
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS v6 BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS prefix CIDR DEFAULT NULL;

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

CREATE INDEX idx_v6_leases_duid ON v6_leases(duid);
CREATE INDEX idx_v6_leases_ip ON v6_leases(ip_addr);
CREATE INDEX idx_v6_leases_scope_state ON v6_leases(scope_id, state);

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

CREATE INDEX idx_v6_prefixes_duid ON v6_prefixes(duid);
CREATE INDEX idx_v6_prefixes_prefix ON v6_prefixes(prefix);
CREATE INDEX idx_v6_prefixes_scope_state ON v6_prefixes(scope_id, state);

CREATE TABLE IF NOT EXISTS v6_reservations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    scope_id UUID NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
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


-- 003_pd.up.sql
-- PD support was added in migration 002; this migration is kept for ordering.
-- If v6_prefixes does not exist yet (legacy systems), create it now.
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

CREATE INDEX IF NOT EXISTS idx_v6_prefixes_duid ON v6_prefixes(duid);
CREATE INDEX IF NOT EXISTS idx_v6_prefixes_prefix ON v6_prefixes(prefix);
CREATE INDEX IF NOT EXISTS idx_v6_prefixes_scope_state ON v6_prefixes(scope_id, state);


-- 004_mac_blacklist.up.sql
CREATE TABLE IF NOT EXISTS mac_blacklist (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    mac_addr TEXT NOT NULL UNIQUE,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mac_blacklist_mac ON mac_blacklist(mac_addr);


-- 005_scope_excluded.up.sql
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS excluded_ips INET[] DEFAULT '{}';


-- 006_reservation_groups.up.sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS reservation_groups (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(128) UNIQUE NOT NULL,
    description TEXT DEFAULT '',
    options JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE reservations
    ADD COLUMN IF NOT EXISTS group_id UUID REFERENCES reservation_groups(id) ON DELETE SET NULL;

ALTER TABLE v6_reservations
    ADD COLUMN IF NOT EXISTS group_id UUID REFERENCES reservation_groups(id) ON DELETE SET NULL;


-- 007_nullable_strings.up.sql
-- Ensure nullable string columns have non-NULL defaults so Go scans into
-- plain string fields never fail, especially after migrating data from
-- external DHCP servers.

UPDATE reservations SET hostname = COALESCE(hostname, '') WHERE hostname IS NULL;
ALTER TABLE reservations ALTER COLUMN hostname SET NOT NULL;
ALTER TABLE reservations ALTER COLUMN hostname SET DEFAULT '';

UPDATE reservations SET description = COALESCE(description, '') WHERE description IS NULL;
ALTER TABLE reservations ALTER COLUMN description SET NOT NULL;
ALTER TABLE reservations ALTER COLUMN description SET DEFAULT '';

UPDATE v6_reservations SET hostname = COALESCE(hostname, '') WHERE hostname IS NULL;
ALTER TABLE v6_reservations ALTER COLUMN hostname SET NOT NULL;
ALTER TABLE v6_reservations ALTER COLUMN hostname SET DEFAULT '';

UPDATE v6_reservations SET description = COALESCE(description, '') WHERE description IS NULL;
ALTER TABLE v6_reservations ALTER COLUMN description SET NOT NULL;
ALTER TABLE v6_reservations ALTER COLUMN description SET DEFAULT '';

UPDATE leases SET hostname = COALESCE(hostname, '') WHERE hostname IS NULL;
ALTER TABLE leases ALTER COLUMN hostname SET NOT NULL;
ALTER TABLE leases ALTER COLUMN hostname SET DEFAULT '';

UPDATE leases SET client_id = COALESCE(client_id, '') WHERE client_id IS NULL;
ALTER TABLE leases ALTER COLUMN client_id SET NOT NULL;
ALTER TABLE leases ALTER COLUMN client_id SET DEFAULT '';

UPDATE v6_leases SET hostname = COALESCE(hostname, '') WHERE hostname IS NULL;
ALTER TABLE v6_leases ALTER COLUMN hostname SET NOT NULL;
ALTER TABLE v6_leases ALTER COLUMN hostname SET DEFAULT '';

UPDATE mac_blacklist SET reason = COALESCE(reason, '') WHERE reason IS NULL;
ALTER TABLE mac_blacklist ALTER COLUMN reason SET NOT NULL;
ALTER TABLE mac_blacklist ALTER COLUMN reason SET DEFAULT '';

UPDATE reservation_groups SET description = COALESCE(description, '') WHERE description IS NULL;
ALTER TABLE reservation_groups ALTER COLUMN description SET NOT NULL;
ALTER TABLE reservation_groups ALTER COLUMN description SET DEFAULT '';

UPDATE ha_nodes SET listen_addr = COALESCE(listen_addr, '') WHERE listen_addr IS NULL;
ALTER TABLE ha_nodes ALTER COLUMN listen_addr SET NOT NULL;
ALTER TABLE ha_nodes ALTER COLUMN listen_addr SET DEFAULT '';

UPDATE ha_nodes SET version = COALESCE(version, '') WHERE version IS NULL;
ALTER TABLE ha_nodes ALTER COLUMN version SET NOT NULL;
ALTER TABLE ha_nodes ALTER COLUMN version SET DEFAULT '';

UPDATE scopes SET domain_name = COALESCE(domain_name, '') WHERE domain_name IS NULL;
ALTER TABLE scopes ALTER COLUMN domain_name SET NOT NULL;
ALTER TABLE scopes ALTER COLUMN domain_name SET DEFAULT '';


-- 008_normalize_ipv4_mapped.up.sql
-- Normalize IPv4 addresses stored in inet columns to a canonical form.
-- PostgreSQL may represent IPv4 addresses as IPv4-mapped IPv6 (::ffff:x.x.x.x)
-- depending on how they were inserted.  host() returns the plain address text,
-- and casting back to inet gives the canonical IPv4 form.
-- family() = 4 limits the update to IPv4 addresses so IPv6 prefixes are untouched.

UPDATE scopes SET start_ip = host(start_ip)::inet WHERE family(start_ip) = 4;
UPDATE scopes SET end_ip   = host(end_ip)::inet   WHERE family(end_ip)   = 4;

UPDATE reservations SET ip_addr = host(ip_addr)::inet WHERE family(ip_addr) = 4;
UPDATE v6_reservations SET ip_addr = host(ip_addr)::inet WHERE family(ip_addr) = 4;

UPDATE leases SET ip_addr = host(ip_addr)::inet WHERE family(ip_addr) = 4;
UPDATE v6_leases SET ip_addr = host(ip_addr)::inet WHERE family(ip_addr) = 4;

UPDATE v6_prefixes SET prefix = host(prefix)::inet WHERE family(prefix) = 4;


-- 009_normalize_ipv4_mapped_v2.up.sql
-- Normalize IPv4-mapped IPv6 addresses to plain IPv4.
-- These addresses are stored with an ::ffff: prefix in the inet column and must
-- be rewritten so that IPv4 comparisons and unique constraints work correctly.

UPDATE scopes SET start_ip = host(start_ip)::inet WHERE text(start_ip) LIKE '::ffff:%';
UPDATE scopes SET end_ip   = host(end_ip)::inet   WHERE text(end_ip)   LIKE '::ffff:%';

UPDATE reservations SET ip_addr = host(ip_addr)::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_reservations SET ip_addr = host(ip_addr)::inet WHERE text(ip_addr) LIKE '::ffff:%';

UPDATE leases SET ip_addr = host(ip_addr)::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_leases SET ip_addr = host(ip_addr)::inet WHERE text(ip_addr) LIKE '::ffff:%';

UPDATE v6_prefixes SET prefix = host(prefix)::inet WHERE text(prefix) LIKE '::ffff:%';


-- 010_normalize_ipv4_mapped_v3.up.sql
-- Normalize remaining IPv4-mapped IPv6 addresses (::ffff:...) across all
-- inet/cidr columns, including arrays. PostgreSQL's host() strips the netmask
-- but keeps the mapped representation, so for inet columns we use host() then
-- remove the ::ffff: prefix. For cidr columns we operate on text() which
-- preserves the mask.

UPDATE scopes SET
    subnet = regexp_replace(text(subnet), '^::ffff:', '')::cidr,
    prefix = CASE WHEN prefix IS NULL THEN NULL ELSE regexp_replace(text(prefix), '^::ffff:', '')::cidr END,
    start_ip = regexp_replace(host(start_ip), '^::ffff:', '')::inet,
    end_ip = regexp_replace(host(end_ip), '^::ffff:', '')::inet,
    gateway = COALESCE((
        SELECT array_agg(regexp_replace(host(x), '^::ffff:', '')::inet)
        FROM unnest(gateway) x
    ), '{}'::inet[]),
    dns = COALESCE((
        SELECT array_agg(regexp_replace(host(x), '^::ffff:', '')::inet)
        FROM unnest(dns) x
    ), '{}'::inet[]),
    excluded_ips = COALESCE((
        SELECT array_agg(regexp_replace(host(x), '^::ffff:', '')::inet)
        FROM unnest(excluded_ips) x
    ), '{}'::inet[])
WHERE text(subnet) LIKE '::ffff:%'
   OR text(prefix) LIKE '::ffff:%'
   OR text(start_ip) LIKE '::ffff:%'
   OR text(end_ip) LIKE '::ffff:%'
   OR EXISTS (SELECT 1 FROM unnest(gateway) x WHERE text(x) LIKE '::ffff:%')
   OR EXISTS (SELECT 1 FROM unnest(dns) x WHERE text(x) LIKE '::ffff:%')
   OR EXISTS (SELECT 1 FROM unnest(excluded_ips) x WHERE text(x) LIKE '::ffff:%');

UPDATE reservations    SET ip_addr = regexp_replace(host(ip_addr), '^::ffff:', '')::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE leases          SET ip_addr = regexp_replace(host(ip_addr), '^::ffff:', '')::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_reservations SET ip_addr = regexp_replace(host(ip_addr), '^::ffff:', '')::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_leases       SET ip_addr = regexp_replace(host(ip_addr), '^::ffff:', '')::inet WHERE text(ip_addr) LIKE '::ffff:%';
UPDATE v6_prefixes     SET prefix  = regexp_replace(text(prefix), '^::ffff:', '')::cidr  WHERE text(prefix)  LIKE '::ffff:%';


-- 011_ip_allocation_logs.up.sql
CREATE TABLE IF NOT EXISTS ip_allocation_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
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

CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_created_at ON ip_allocation_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_scope_id ON ip_allocation_logs(scope_id);
CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_mac_addr ON ip_allocation_logs(mac_addr);
CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_ip_addr ON ip_allocation_logs(ip_addr);
CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_action ON ip_allocation_logs(action);


-- 012_system_logs.up.sql
CREATE TABLE IF NOT EXISTS system_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    level VARCHAR(16) NOT NULL,
    message TEXT NOT NULL,
    attrs JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_system_logs_created_at ON system_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_system_logs_level ON system_logs(level);


-- 013_system_logs_node_id.up.sql
ALTER TABLE system_logs ADD COLUMN IF NOT EXISTS node_id VARCHAR(64) DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_system_logs_node_id ON system_logs(node_id);


-- 014_ip_allocation_logs_node_id.up.sql
ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS node_id VARCHAR(64) DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_node_id ON ip_allocation_logs(node_id);


