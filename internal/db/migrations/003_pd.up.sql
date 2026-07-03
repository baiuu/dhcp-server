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
