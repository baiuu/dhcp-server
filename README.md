# Go DHCP 高可用数据库服务器

[![CI](https://github.com/baiuu/dhcp-server/actions/workflows/ci.yml/badge.svg)](https://github.com/baiuu/dhcp-server/actions/workflows/ci.yml)
[![Release](https://github.com/baiuu/dhcp-server/actions/workflows/release.yml/badge.svg)](https://github.com/baiuu/dhcp-server/actions/workflows/release.yml)

> ⚠️ **平台支持声明：本项目仅支持 Linux 服务器部署。**
>
> DHCP 服务需要绑定特权端口（UDP 67/547）、发送/接收原始套接字和广播包，并依赖 systemd 进行服务管理。因此**不支持 Windows 和 macOS 生产部署**，仅适配 Linux（推荐 Ubuntu / Debian / RHEL / CentOS 等服务器发行版）。
>
> 🤖 **本项目代码由 [Kimi Code](https://kimi.moonshot.cn/) 辅助编写。**

企业级 DHCPv4/v6 双栈服务器，基于 Go 开发，PostgreSQL 持久化，Active/Active DHCP Failover 多集群高可用，内嵌现代化 Web 管理后台，支持 Prometheus 监控与审计日志。

## 特性

- **DHCPv4 + DHCPv6 双栈**：完整 DHCPv4 状态机 + DHCPv6 Solicit/Advertise/Request/Reply/Renew/Rebind/Release/Decline/Information-Request
- **DHCPv6 PD（Prefix Delegation）**：支持从配置的前缀池分配 /64 子前缀
- **所有 DHCPv4/v6 Option 支持**：可三层覆盖（全局、作用域、保留 IP/Reservation）
- **PostgreSQL 持久化**：租约、保留 IP、PD 前缀、作用域配置、审计日志全部入库
- **现代化 Web 管理界面**：Dashboard 图表、作用域管理、保留 IP、租约详情/搜索、用户管理、Options 参考、审计日志
- **JWT 管理员鉴权**：支持 admin/readonly 角色与用户改密
- **Active/Active DHCP Failover + 多集群**：多节点同时服务，PostgreSQL 事务 + advisory lock 保证地址分配原子性
- **Prometheus 指标**：DHCP 包统计、活跃租约、HTTP 请求延迟等
- **审计日志**：记录登录、作用域/保留 IP/租约/用户操作
- **单二进制部署**

## 快速开始

### 1. 安装 PostgreSQL（两种方式都需要）

```bash
# Debian / Ubuntu
sudo apt update
sudo apt install -y postgresql postgresql-contrib

# RHEL / CentOS / Rocky
sudo dnf install -y postgresql-server postgresql-contrib
sudo postgresql-setup --initdb && sudo systemctl enable --now postgresql
```

### 2. 初始化数据库

创建数据库和用户（示例）：

```bash
sudo -u postgres psql -c "CREATE USER dhcp WITH PASSWORD 'your-db-password';"
sudo -u postgres psql -c "CREATE DATABASE dhcpdb OWNER dhcp;"
```

### 3. 部署程序

#### 方式一：预编译包（推荐）

从 [Releases](https://github.com/baiuu/dhcp-server/releases) 下载对应架构的压缩包（打 `v*` tag 时由 GitHub Actions 自动构建 linux/amd64 与 linux/arm64 静态二进制，无需任何编译环境）：

```bash
tar xzf dhcp-server-linux-amd64.tar.gz
sudo mkdir -p /opt/dhcp-server
sudo cp -r dhcp-server-linux-amd64/* /opt/dhcp-server/
```

#### 方式二：源码编译

环境要求：Go 1.26+、Node.js 22+、yarn（均可由 `make setup` 通过官方二进制包自动安装，不依赖 apt/deb）。

```bash
git clone https://github.com/baiuu/dhcp-server.git
cd dhcp-server

sudo make setup    # 安装 Go + Node.js + yarn 编译环境
make build         # 构建 Web UI 并编译后端二进制
sudo make install  # 安装到 /opt/dhcp-server 并配置 systemd
```

安装前缀可自定义：`sudo make install INSTALL_PREFIX=/usr/local/dhcp-server`

也可使用一键安装脚本 `sudo ./scripts/install-go-and-build.sh`。

### 4. 生成 JWT 密钥并配置

```bash
sudo /opt/dhcp-server/scripts/generate-jwt-keys.sh
sudo cp /opt/dhcp-server/configs/config.example.yaml /opt/dhcp-server/configs/config.yaml
sudo nano /opt/dhcp-server/configs/config.yaml
```

至少修改：

```yaml
database:
  url: "postgres://dhcp:your-db-password@localhost:5432/dhcpdb?sslmode=disable"

auth:
  default_admin_password: "your-strong-password"

server:
  interface: "eth0"   # 替换为实际网卡名
```

### 5. 启动服务并访问

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now dhcp-server
```

打开 http://服务器IP:8080，使用配置的管理员账号登录。

> 直接运行（开发/测试）：`sudo ./build/dhcp-server -config=configs/config.yaml`，需要 root 或 `CAP_NET_BIND_SERVICE` + `CAP_NET_RAW` 能力。

## 高可用多节点部署

本服务器采用 **Active/Active + 数据库协调** 的 DHCP Failover 方案，无需 Keepalived/VIP：

1. 在两台或多台 Linux 服务器上部署相同代码和配置。
2. 编辑各节点 `configs/config.yaml` 中的 `cluster` 段，确保 `cluster_id` 相同、`node_id` 唯一：

```yaml
cluster:
  enabled: true
  cluster_id: "cluster-1"
  node_id: "node1"      # 另一台节点改为 node2
  listen_addr: "192.168.1.11:67"
  heartbeat_interval: "5s"
  node_timeout: "30s"
```

3. 所有节点同时启动 DHCPv4/v6 服务，同时处理客户端请求。
4. 地址分配通过 PostgreSQL `pg_advisory_lock` + 事务保证原子性，避免多节点分配冲突。
5. 节点心跳写入 `ha_nodes` 表，可通过 Web UI 或 `GET /api/cluster/nodes` 查看集群状态。

> 注意：PostgreSQL 本身建议做主从或外部高可用（如 Patroni），数据库层不在本服务器改造范围内。

## 作用域 Options 示例

### DHCPv4 Options

在 Web UI 或 API 中，Options 字段为 JSON，key 为 option code：

```json
{
  "1":  {"type": "ip", "value": "255.255.255.0"},
  "3":  {"type": "ips", "value": ["192.168.100.1"]},
  "6":  {"type": "ips", "value": ["8.8.8.8", "8.8.4.4"]},
  "28": {"type": "ip", "value": "192.168.100.255"},
  "42": {"type": "ips", "value": ["192.168.100.2"]},
  "66": {"type": "string", "value": "tftp.example.com"},
  "67": {"type": "string", "value": "pxelinux.0"},
  "121": {"type": "routes", "value": [{"destination": "10.0.0.0", "mask": 8, "router": "192.168.1.1"}]}
}
```

### DHCPv6 Options

```json
{
  "23": {"type": "ips", "value": ["2001:db8::53"]},
  "24": {"type": "domains", "value": ["v6.example.com"]},
  "31": {"type": "ips", "value": ["2001:db8::123"]},
  "32": {"type": "uint32", "value": 3600}
}
```

支持的类型：`ip`、`ips`、`string`、`uint8`、`uint16`、`uint32`、`hex`、`bool`、`routes`、`domains`。

Web UI 中 "Options 参考" 页面提供常见 Options 的预设写法。

## API 概览

- `POST /api/auth/login` — 登录获取 JWT
- `POST /api/users/change-password` — 修改当前用户密码
- `GET  /api/dashboard` — 概览数据
- `GET/POST/PUT/DELETE /api/scopes` — 作用域管理（支持 IPv4/IPv6）
- `GET/POST /api/scopes/:id/reservations` — 保留 IP / V6 Reservation
- `GET /api/scopes/:id/leases` — 租约列表
- `POST /api/leases/:id/release` — 释放租约
- `DELETE /api/leases/:id` / `DELETE /api/v6-leases/:id` — 删除 released/expired 租约
- `GET /api/leases/search?mac=...` / `GET /api/leases/search?duid=...` — 按 MAC/DUID 搜索租约
- `GET/POST /api/users` — 用户管理
- `GET /api/audit-logs` — 审计日志
- `GET /metrics` — Prometheus 指标
- `GET /health` — 健康检查

## Prometheus 指标

| 指标名 | 说明 |
|--------|------|
| `dhcp_packets_total` | DHCP 包处理数，按 type/message_type 标签 |
| `dhcp_replies_total` | DHCP 回复数 |
| `dhcp_leases_active{version="v4/v6"}` | 活跃租约数 |
| `dhcp_leases_released_total` | 释放租约总数 |
| `dhcp_leases_declined_total` | 拒绝租约总数 |
| `dhcp_http_requests_total` | HTTP 请求数 |
| `dhcp_http_request_duration_seconds` | HTTP 请求延迟 |

## 开发

```bash
# 默认使用 dhcpdb_test 数据库，需先创建：
sudo -u postgres psql -c "CREATE DATABASE dhcpdb_test OWNER dhcp;"

make test   # 运行测试
make fmt    # 格式化代码
make clean  # 清理构建产物
```

## 注意事项

- **本项目仅支持 Linux 服务器部署**，请勿在 Windows / macOS 上运行生产服务。
- 生产环境请修改默认密码和 JWT Secret。
- PostgreSQL 自身建议做主从或外部高可用。
- 运行 DHCPv4 服务需要绑定 UDP 67 端口，确保无其他 DHCP 服务冲突。
- DHCPv6 服务绑定 UDP 547 端口，需要 IPv6 环境。
