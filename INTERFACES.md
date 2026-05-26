# chawrtd 通信接口文档

> 本文档描述 chawrtd 与 openclaw-wrt（TypeScript 客户端）及 clawwrt（路由器端 Agent）之间的所有通信接口。

## 架构概览

```
┌──────────────────┐   HTTP REST + SSE    ┌──────────────┐   WebSocket   ┌──────────────┐
│  openclaw-wrt    │ ───────────────────→ │   chawrtd    │ ←──────────→  │   clawwrt    │
│  (TypeScript     │                      │   (Go daemon)│               │  (路由器端)   │
│   OpenClaw 插件)  │ ←────────────────── │              │               │              │
└──────────────────┘   JSON responses     └──────────────┘               └──────────────┘
```

**通信路径：**
- **openclaw-wrt → chawrtd：** HTTP REST API（命令/查询）+ SSE 事件流（设备事件推送）
- **chawrtd → clawwrt：** WebSocket 双向通信（命令下发 + 响应/事件上报）

---

## 1. 标准响应格式

chawrtd 所有 REST API 端点统一使用以下响应信封：

### 成功响应

```json
{
  "ok": true,
  "data": { ... }
}
```

### 错误响应

```json
{
  "ok": false,
  "error": "错误描述信息"
}
```

> **向后兼容：** 部分旧端点（如 VPS/WG/FRPS 操作）可能仍返回扁平格式。
> openclaw-wrt 客户端通过 `extractChawrtdData()` 统一处理两种格式。

---

## 2. openclaw-wrt → chawrtd REST API

### 2.1 设备管理

| 方法 | 端点 | 描述 |
|------|------|------|
| `GET` | `/v1/devices` | 列出所有已连接设备 |
| `GET` | `/v1/device/{deviceId}` | 获取单个设备信息 |
| `POST` | `/v1/device/{deviceId}/{operation}` | 向设备发送命令 |

### 2.2 诊断端点

| 方法 | 端点 | 描述 |
|------|------|------|
| `POST` | `/v1/device/{deviceId}/diagnose/dhcp` | DHCP 诊断 |
| `POST` | `/v1/device/{deviceId}/diagnose/dns` | DNS 诊断 |
| `POST` | `/v1/device/{deviceId}/diagnose/http` | HTTP 认证服务诊断 |
| `POST` | `/v1/device/{deviceId}/diagnose/https` | HTTPS 认证服务诊断 |

### 2.3 设备别名管理

| 方法 | 端点 | 描述 |
|------|------|------|
| `GET` | `/v1/devices/aliases` | 列出所有设备别名 |
| `POST` | `/v1/devices/alias/set` | 设置设备别名 |
| `POST` | `/v1/devices/alias/delete` | 删除设备别名 |

### 2.4 事件流

| 方法 | 端点 | 描述 |
|------|------|------|
| `GET` | `/v1/events/stream` | SSE 事件流（设备上下线、状态变更等） |

### 2.5 VPS 服务管理

| 方法 | 端点 | 描述 |
|------|------|------|
| `POST` | `/v1/frps/deploy` | 部署 FRPS 内网穿透服务端 |
| `GET` | `/v1/frps/status` | 获取 FRPS 状态 |
| `POST` | `/v1/frps/verify` | 验证 FRPS 端口监听 |
| `POST` | `/v1/frps/reset` | 重置 FRPS 配置 |
| `GET` | `/v1/vps/public-ip` | 获取 VPS 公网 IP |
| `POST` | `/v1/wg/deploy` | 部署 WireGuard 服务端 |
| `GET` | `/v1/wg/status` | 获取 WireGuard 状态 |
| `POST` | `/v1/wg/reset` | 重置 WireGuard 配置 |
| `POST` | `/v1/wg/verify` | 验证 WireGuard 连通性 |

### 2.6 健康检查

| 方法 | 端点 | 描述 |
|------|------|------|
| `GET` | `/healthz` | 服务健康检查 |

---

## 3. HTTP 请求头

### 标准头

| 头 | 值 | 描述 |
|----|-----|------|
| `Content-Type` | `application/json` | 请求/响应体格式 |

### 控制头

| 头 | 值 | 描述 |
|----|-----|------|
| `X-Expect-Response` | `true` / `false` | 控制 chawrtd 是否等待设备响应。默认 `true`。设为 `false` 时 chawrtd 立即返回 `accepted`，不等待设备回复。 |

> **向后兼容：** 旧版客户端可能在请求体中使用 `__expect_response` 字段。chawrtd 同时支持两种方式，优先读取 Header。

---

## 4. chawrtd → clawwrt WebSocket 协议

### 4.1 连接建立

clawwrt 通过 WebSocket 连接到 `ws://<chawrtd-host>:8001/ws/clawwrt`。

**连接消息（clawwrt → chawrtd）：**

```json
{
  "op": "connect",
  "device_id": "wifi1",
  "token": "<认证token>",
  "mode": 0,
  "gateway": { "gw_id": "..." },
  "data": {
    "device_info": { ... }
  }
}
```

**连接响应（chawrtd → clawwrt）：**

```json
{
  "req_id": "<原始req_id>",
  "data": { "ok": true }
}
```

### 4.2 命令/响应模式

**命令（chawrtd → clawwrt）：**

```json
{
  "op": "get_wifi_info",
  "req_id": "req-1234567890-1",
  "data": {}
}
```

**响应（clawwrt → chawrtd）：**

```json
{
  "req_id": "req-1234567890-1",
  "response": "200",
  "data": { ... }
}
```

**错误响应：**

```json
{
  "req_id": "req-1234567890-1",
  "error": "device not found"
}
```

### 4.3 设备事件推送

clawwrt 可主动推送事件（无需 chawrtd 请求）：

```json
{
  "op": "client_connected",
  "data": {
    "mac": "AA:BB:CC:DD:EE:FF",
    "ip": "192.168.1.100"
  }
}
```

**已知事件类型：**
- `client_connected` — 新客户端连接
- `client_disconnected` — 客户端断开
- `net_link_up` / `net_link_down` — 网络链路状态变更
- `usb_storage_attached` / `usb_storage_detached` — USB 存储事件

### 4.4 心跳

- chawrtd 每 30 秒发送 WebSocket Ping
- clawwrt 需在 60 秒内回复 Pong
- 超时未响应则断开连接

---

## 5. SSE 事件流协议

openclaw-wrt 通过 `GET /v1/events/stream` 订阅设备事件。

**连接后收到：**

```
: connected

```

**心跳（每 25 秒）：**

```
: heartbeat

```

**设备事件：**

```
event: device
data: {"op":"client_connected","device_id":"wifi1","alias":"WiFi1","data":{...},"time":1234567890}

```

---

## 6. TypeScript 客户端接口（openclaw-wrt）

### 6.1 ChawrtdClient 类

```typescript
class ChawrtdClient {
  // HTTP 通信
  call(params: {
    path: string;
    method?: "GET" | "POST";
    body?: unknown;
    headers?: Record<string, string>;
    timeoutMs?: number;
    signal?: AbortSignal;
  }): Promise<ChawrtdToolResult>;

  // 设备发现
  listDevices(): Promise<DeviceSnapshot[]>;
  getDevice(deviceId: string): Promise<DeviceSnapshot | null>;
  ensureDevice(deviceId: string): Promise<DeviceSnapshot>;

  // 设备操作
  callDeviceOp(params: {
    deviceId: string;
    op: string;
    payload?: JsonRecord;
    timeoutMs?: number;
    expectResponse?: boolean;
    signal?: AbortSignal;
  }): Promise<JsonRecord>;

  // 诊断
  callDeviceDiagnose(params: {
    deviceId: string;
    kind: "dhcp" | "dns" | "http" | "https";
    payload?: JsonRecord;
    timeoutMs?: number;
    signal?: AbortSignal;
  }): Promise<JsonRecord>;
}
```

### 6.2 ClawWRTBridge 接口

当 chawrtd 运行在同一进程内时，可通过 Bridge 直接调用，跳过 HTTP：

```typescript
interface ClawWRTBridge {
  listDevices?: () => Array<{ deviceId?: string } & Partial<DeviceSnapshot>>;
  getDevice?: (deviceId: string) => DeviceSnapshot | null;
  callDevice?: (input: {
    deviceId: string;
    op: string;
    payload?: JsonRecord;
    timeoutMs?: number;
    expectResponse?: boolean;
  }) => Promise<JsonRecord>;
  callDeviceDiagnose?: (input: {
    deviceId: string;
    kind: "dhcp" | "dns" | "http" | "https";
    payload?: JsonRecord;
    timeoutMs?: number;
  }) => Promise<JsonRecord>;
}
```

### 6.3 关键类型

```typescript
type DeviceSnapshot = {
  deviceId: string;
  connectedAtMs: number;
  lastSeenAtMs: number;
  remoteAddress?: string;
  gateway?: unknown;
  deviceInfo?: unknown;
  authMode?: number;
  alias?: string;
};

type ChawrtdToolResult = {
  ok?: boolean;
  summary?: string;
  output?: string;
  data?: JsonRecord;
  error?: string;
};

type JsonRecord = Record<string, unknown>;
```

---

## 7. 超时层级

| 层级 | 默认值 | 可配置 |
|------|--------|--------|
| openclaw-wrt `call()` | 180s | ✅ `timeoutMs` 参数 |
| chawrtd `defaultTimeout` | 120s | ✅ `CHAWRTD_DEFAULT_TIMEOUT_SECONDS` 环境变量 |
| WebSocket `requestTimeout` | 30s | ✅ `SetRequestTimeout()` |

**注意：** openclaw-wrt 的 timeout 控制 HTTP 请求超时，chawrtd 的 timeout 控制等待设备 WebSocket 响应超时。两者独立生效。

---

## 8. 认证

- **chawrtd ↔ clawwrt：** Token 认证（连接时在首条消息中携带）
- **openclaw-wrt → chawrtd：** 无认证（本地回环通信，仅限 127.0.0.1）
- **chawrtd HTTP API：** 无认证（可通过反向代理添加）

---

## 9. 环境变量

| 变量 | 默认值 | 描述 |
|------|--------|------|
| `CHAWRTD_ADDR` | `:8001` | 监听地址 |
| `CHAWRTD_DEFAULT_TIMEOUT_SECONDS` | `120` | 默认设备命令超时 |
| `CHAWRTD_TOKEN` | `clawwrt` | WebSocket 认证 Token |
| `CHAWRTD_CONFIG_FILE` | `./chawrtd.toml` | 配置文件路径 |
| `CHAWRTD_ALIAS_FILE` | `device-aliases.json` | 设备别名存储文件 |
| `CHAWRTD_TLS_CERT_FILE` | — | TLS 证书（需配合 KEY） |
| `CHAWRTD_TLS_KEY_FILE` | — | TLS 私钥（需配合 CERT） |

---

## 10. 设备操作（op）清单

以下是 chawrtd 透传给 clawwrt 的标准操作名：

### 设备管理
- `get_status` — 获取设备运行状态
- `get_sys_info` — 获取系统信息
- `get_device_info` — 获取设备元数据
- `update_device_info` — 更新设备元数据
- `get_firmware_info` — 获取固件信息
- `firmware_upgrade` — 触发固件升级
- `reboot_device` — 重启设备

### Wi-Fi
- `get_wifi_info` — 获取 WiFi 配置
- `set_wifi_info` — 设置 WiFi 配置
- `scan_wifi` — 扫描附近 WiFi
- `set_wifi_relay` — 设置 WiFi 中继
- `delete_wifi_relay` — 删除 WiFi 中继

### 客户端管理
- `get_clients` — 列出已认证客户端
- `get_client_info` — 获取单个客户端详情
- `auth_client` — 认证客户端
- `kickoff_client` — 踢下线客户端
- `tmp_pass_client` — 临时放行客户端

### 网络
- `get_network_interfaces` — 获取网络接口信息
- `get_br_lan` — 获取 LAN 网桥信息
- `set_br_lan` — 设置 LAN 网桥

### BPF 流量监控
- `bpf_add` — 添加监控目标
- `bpf_del` — 删除监控目标
- `bpf_flush` — 清空监控表
- `bpf_json` — 查询监控统计
- `bpf_update` — 更新限速规则
- `bpf_update_all` — 批量更新限速

### 认证与白名单
- `get_auth_serv` — 获取认证服务器配置
- `set_auth_serv` — 设置认证服务器
- `get_trusted_domains` — 获取可信域名白名单
- `sync_trusted_domains` — 同步可信域名
- `get_trusted_mac` — 获取可信 MAC 白名单
- `sync_trusted_mac` — 同步可信 MAC
- `get_trusted_wildcard_domains` — 获取通配域名白名单
- `sync_trusted_wildcard_domains` — 同步通配域名

### Portal 门户页
- `generate_portal_page` — 生成门户页 HTML
- `publish_portal_page` — 发布门户页到路由器

### MQTT
- `get_mqtt_serv` — 获取 MQTT 配置
- `set_mqtt_serv` — 设置 MQTT 配置

### WireGuard VPN
- `get_wireguard_vpn` — 获取 WireGuard 配置
- `set_wireguard_vpn` — 设置 WireGuard 配置
- `reset_wireguard_vpn` — 重置 WireGuard 配置
- `get_wireguard_vpn_status` — 获取 WireGuard 运行状态
- `generate_wireguard_keys` — 生成密钥对
- `set_vpn_routes` — 设置 VPN 路由
- `get_vpn_routes` — 获取 VPN 路由

### 内网穿透 (XFRPC)
- `get_xfrpc_common` — 获取 XFRPC 全局配置
- `set_xfrpc_common` — 设置 XFRPC 全局配置
- `get_xfrpc_tcp_service` — 获取 TCP 服务配置
- `add_xfrpc_tcp_service` — 添加 TCP 服务
- `del_xfrpc_tcp_service` — 删除 TCP 服务
- `disable_xfrpc_tcp_service` — 禁用 TCP 服务
- `restart_xfrpc` — 重启 XFRPC

### Shell（受限）
- `execute_shell` — 执行 Shell 命令（需用户显式授权）

---

*文档版本：2026-05-26*
