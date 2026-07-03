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
