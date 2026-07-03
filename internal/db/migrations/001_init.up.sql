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
