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
