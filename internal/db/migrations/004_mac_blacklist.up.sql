CREATE TABLE IF NOT EXISTS mac_blacklist (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    mac_addr TEXT NOT NULL UNIQUE,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mac_blacklist_mac ON mac_blacklist(mac_addr);
