ALTER TABLE system_logs ADD COLUMN IF NOT EXISTS node_id VARCHAR(64) DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_system_logs_node_id ON system_logs(node_id);
