-- 003_scope_pd_enabled.up.sql
-- Explicit PD (Prefix Delegation) switch per scope: only scopes with
-- pd_enabled=true serve IA_PD requests. Previously PD was implicitly enabled
-- by configuring a prefix pool.
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS pd_enabled BOOLEAN NOT NULL DEFAULT FALSE;

-- Keep existing behavior for scopes that already have a prefix pool configured.
UPDATE scopes SET pd_enabled = TRUE WHERE prefix IS NOT NULL;
