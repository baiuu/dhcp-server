#!/bin/bash
# fix-database.sh
#
# 手动修复 dhcp-server 数据库：
#   1. 对齐 schema_migrations 版本
#      （迁移文件合并后，旧数据库的版本号如 6 已不存在，
#        会报 "no migration found for version N" 导致服务无法启动）
#   2. 回收僵尸租约（offered 超时未确认 / active 已过期但未翻转）
#   3. 释放被其他客户端错误占用的绑定（reservation）地址
#
# 用法:
#   sudo ./scripts/fix-database.sh [database_url]
#
# 默认连接: postgres://dhcp@localhost:5432/dhcpdb?sslmode=disable
# 需要密码时用环境变量: PGPASSWORD=xxx sudo -E ./scripts/fix-database.sh
# 或用 postgres 系统用户 peer 认证:
#   sudo -u postgres ./scripts/fix-database.sh "postgres:///dhcpdb?host=/var/run/postgresql"
#
# 脚本幂等，可重复执行；只改状态和版本号，不删除任何表。

set -euo pipefail

DB_URL="${1:-postgres://dhcp@localhost:5432/dhcpdb?sslmode=disable}"

if ! command -v psql >/dev/null 2>&1; then
    echo "ERROR: psql not found. Install postgresql-client first."
    exit 1
fi

echo "==> Database: ${DB_URL%%\?*}"
echo

psql -v ON_ERROR_STOP=1 "$DB_URL" <<'SQL'
\set ECHO queries
\echo '=== 1. 迁移版本状态（修复前）==='

SELECT version, dirty FROM schema_migrations;

-- 当前代码的迁移文件最大版本是 2（002_server_state）。
-- 旧数据库的版本号（001-014 时代）大于 2 时对齐到 1，让服务接着跑 002。
-- dirty 标记（上次迁移中断残留）一并清除。
UPDATE schema_migrations
SET version = 1, dirty = false
WHERE version > 2 OR dirty;

\echo ''
\echo '=== 2. 迁移版本状态（修复后）==='

SELECT version, dirty FROM schema_migrations;

\echo ''
\echo '=== 3. 僵尸租约统计（清理前）==='

SELECT 'leases(v4)' AS tbl, state, COUNT(*),
       COUNT(*) FILTER (WHERE state='offered' AND COALESCE(offered_at, starts_at) < NOW() - INTERVAL '120 seconds') AS stale_offered,
       COUNT(*) FILTER (WHERE state='active' AND ends_at < NOW()) AS expired_active
FROM leases GROUP BY state
UNION ALL
SELECT 'v6_leases', state, COUNT(*),
       COUNT(*) FILTER (WHERE state='offered' AND COALESCE(offered_at, starts_at) < NOW() - INTERVAL '120 seconds'),
       COUNT(*) FILTER (WHERE state='active' AND ends_at < NOW())
FROM v6_leases GROUP BY state
UNION ALL
SELECT 'v6_prefixes', state, COUNT(*),
       COUNT(*) FILTER (WHERE state='offered' AND starts_at < NOW() - INTERVAL '120 seconds'),
       COUNT(*) FILTER (WHERE state='active' AND ends_at < NOW())
FROM v6_prefixes GROUP BY state
ORDER BY 1, 2;

\echo ''
\echo '=== 4. 回收僵尸租约 ==='

-- offered 租约超过 120 秒未确认（客户端 Discover 后未 Request）
UPDATE leases SET state='expired', updated_at=NOW()
WHERE state='offered' AND COALESCE(offered_at, starts_at) < NOW() - INTERVAL '120 seconds';

-- active 租约已过期
UPDATE leases SET state='expired', updated_at=NOW()
WHERE state='active' AND ends_at < NOW();

UPDATE v6_leases SET state='expired', updated_at=NOW()
WHERE state='offered' AND COALESCE(offered_at, starts_at) < NOW() - INTERVAL '120 seconds';

UPDATE v6_leases SET state='expired', updated_at=NOW()
WHERE state='active' AND ends_at < NOW();

UPDATE v6_prefixes SET state='expired', updated_at=NOW()
WHERE state='offered' AND starts_at < NOW() - INTERVAL '120 seconds';

UPDATE v6_prefixes SET state='expired', updated_at=NOW()
WHERE state='active' AND ends_at < NOW();

\echo ''
\echo '=== 5. 绑定（reservation）地址被其他客户端占用（修复前）==='

SELECT l.mac_addr AS holder, l.ip_addr, r.mac_addr AS reserved_for, l.state
FROM leases l
JOIN reservations r ON r.scope_id = l.scope_id AND r.ip_addr = l.ip_addr
WHERE l.state IN ('active','offered') AND l.mac_addr <> r.mac_addr;

SELECT l.duid AS holder, l.ip_addr, r.duid AS reserved_for, l.state
FROM v6_leases l
JOIN v6_reservations r ON r.scope_id = l.scope_id AND r.ip_addr = l.ip_addr
WHERE l.state IN ('active','offered') AND l.duid <> r.duid;

\echo ''
\echo '=== 6. 释放被错误占用的绑定地址 ==='

-- 这些地址在绑定设备下次请求时会重新分配给正确的持有者
UPDATE leases l SET state='released', updated_at=NOW()
FROM reservations r
WHERE r.scope_id = l.scope_id AND r.ip_addr = l.ip_addr
  AND l.state IN ('active','offered') AND l.mac_addr <> r.mac_addr;

UPDATE v6_leases l SET state='released', updated_at=NOW()
FROM v6_reservations r
WHERE r.scope_id = l.scope_id AND r.ip_addr = l.ip_addr
  AND l.state IN ('active','offered') AND l.duid <> r.duid;

\echo ''
\echo '=== 7. 完成。现在可以启动服务: sudo systemctl restart dhcp-server ==='
SQL
