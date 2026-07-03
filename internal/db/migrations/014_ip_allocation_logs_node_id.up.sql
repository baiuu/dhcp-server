ALTER TABLE ip_allocation_logs ADD COLUMN IF NOT EXISTS node_id VARCHAR(64) DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_ip_allocation_logs_node_id ON ip_allocation_logs(node_id);
