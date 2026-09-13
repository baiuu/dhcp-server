-- 002_server_state.up.sql
-- Key-value table for node-persistent server state (e.g. DHCPv6 server DUID).
CREATE TABLE IF NOT EXISTS server_state (
    key VARCHAR(64) PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
