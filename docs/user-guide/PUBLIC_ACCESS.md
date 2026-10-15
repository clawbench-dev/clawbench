# 公网访问 ClawBench

本文介绍两种在公网访问 ClawBench 的方式：**IPv6** 和 **EasyTier 去中心化组网**。

> **English Summary**: This guide covers two ways to access ClawBench from the public internet:
> 1. **IPv6 Direct Connection** — If your ISP provides public IPv6, connect directly with no relay. Fastest and simplest.
> 2. **EasyTier Decentralized Networking** — No VPS required. Install EasyTier on both server and phone, join the same network, and connect via virtual IP. Best for users without IPv6 or VPS.


```mermaid
flowchart TD
    Q1{有公网 IPv6?}
    Q1 -- 是 --> IPv6[IPv6 直连]
    Q1 -- 否 --> ET[EasyTier 组网]
```

> ⚠️ **历史选项已移除**：FRP 反向代理功能已于本版本移除。如果你使用过该功能,可手动部署独立 FRP 客户端（如 [fatedier/frp](https://github.com/fatedier/frp) 官方发行版），指向 ClawBench HTTP 端口——本仓库不再内置集成。

---

## 方式一：IPv6 公网直连

目前国内大部分家用宽带都已支持 IPv6，这是最简单、最稳定的公网访问方式——无需第三方中转，直连速度快、延迟低。

### 1. 确认 IPv6 支持

在服务端执行：

```bash
ip -6 addr show
```

如果出现 `inet6` 开头的地址，说明服务器已获得 IPv6 地址。例如：

```
2: eth0: <...> mtu 1500 state UP qlen 1000
    inet6 240e:390:1234:5678::123/64 scope global
    inet6 fe80::abcd:ef01:2345:6789/64 scope link
```

重点关注 `scope global` 的地址，那是公网 IPv6 地址。

### 2. 配置路由器

大多数家用路由器默认已开启 IPv6，但不同品牌配置入口不同：

| 品牌 | 典型设置路径 | 推荐模式 |
|------|-------------|---------|
| **TP-Link / 水星 / 迅捷** | 高级设置 → IPv6 → 开启 | `PPPoEv6` 或 `自动获取地址（SLAAC）` |
| **小米 / Redmi** | 常用设置 → 上网设置 → IPv6 开关 | `Native（原生）` 或 `DHCPv6` |
| **华为 / 荣耀路由** | 更多功能 → 网络设置 → IPv6 | `自动配置（SLAAC + DHCPv6）` |
| **华硕（ASUS）** | 高级设置 → IPv6 | `Native（PPPoE 拨号）` 或 `Passthrough` |
| **OpenWrt** | 网络 → 接口 → WAN → IPv6 设置 | `DHCPv6 Client` + 申请前缀 |
| **iKuai（爱快）** | 网络设置 → IPv6 → WAN 口 | `自动获取`或 `PPPoE`，LAN 口开启`RA 通告` |

**关键要点：**
- 确保路由器的 **IPv6 防火墙**（或称为 SPI 防火墙、IPv6 入站过滤）放行 ClawBench 端口（默认 `20000`）
- 如果光猫是路由模式，光猫也需要开启 IPv6 并放行端口
- 推荐将服务器设为 **固定 IPv6 地址**（DHCPv6 静态分配或在设备上手动配置）

### 3. 启动 ClawBench

```bash
clawbench
```

默认监听 `0.0.0.0:20000`，IPv6 也会自动生效。

如需修改端口：

```bash
clawbench -p 20080
```

### 4. 访问

从外网手机或电脑访问：

```
http://[240e:390:1234:5678::123]:20000
```

> ⚠️ IPv6 地址需要用 `[]` 包裹。

如果记不住地址，可使用 **DDNS**（动态域名解析，如 `dynv6.com`、`ddns-go`）将 IPv6 地址绑定到域名：

```
http://your-clawbench.dynv6.net:20000
```

> **注意**：部分手机热点或 4G/5G 网络可能未分配 IPv6 地址，此时无法通过 IPv6 访问。建议作为备选或双栈方案。

---
## 方式三：EasyTier 去中心化组网

EasyTier 是一款简单、安全、去中心化的内网穿透与异地组网方案，使用 Rust 语言和 Tokio 框架实现。与依赖 VPS 中转的传统方案不同，EasyTier 通过 P2P 打洞实现节点直连，无需公网 IP 即可组网。

### 特点

- **去中心化**：无需中心服务器，节点平等独立，可互相转发
- **无需公网 IP**：支持社区提供的免费公共节点辅助打洞，打洞成功后 P2P 直连
- **WireGuard 加密**：所有通信流量自动加密，保障安全
- **NAT 穿透**：支持 UDP/TCP 多协议打洞，应对复杂网络环境
- **子网代理**：可将可访问的网段暴露给虚拟网络，其他节点通过该节点访问局域网
- **智能路由**：多路径支持，自动切换健康链路，高丢包时自动降级
- **跨平台**：支持 Linux、macOS、Windows、Android
- **IPv6 支持**：支持 IPv6 组网

### 架构说明

```
Android 手机 (EasyTier App, 虚拟IP 10.10.10.2) ←→ 公共节点 (辅助打洞) ←→ ClawBench 服务器 (EasyTier 节点, 虚拟IP 10.10.10.1)
                                  ↑ P2P 打洞成功后直连，无需中转
```

### 步骤一：安装 EasyTier

前往 [EasyTier 官网](https://easytier.cn/guide/download.html) 或 [GitHub Releases](https://github.com/EasyTier/EasyTier/releases) 下载对应平台的命令行程序（CLI）。

**Linux（x86_64）**：

```bash
wget https://github.com/EasyTier/EasyTier/releases/latest/download/easytier-linux-x86_64.zip
unzip easytier-linux-x86_64.zip
chmod +x easytier-core easytier-cli
```

**Windows**：下载 `easytier-windows-x86_64.zip`，解压后获得 `easytier-core.exe` 和 `easytier-cli.exe`。

**macOS**：下载 `easytier-darwin-aarch64.zip`，解压后赋予执行权限。

> EasyTier 也提供 GUI 图形界面程序，适合不熟悉命令行的用户。

### 步骤二：在 ClawBench 服务器上启动 EasyTier

在运行 ClawBench 的服务器上，以管理员/root 权限执行：

```bash
easytier-core --network-name my-clawbench --network-secret my-secret-password --ipv4 10.10.10.1 -p tcp://public.easytier.top:11010
```

参数说明：

| 参数 | 说明 |
|------|------|
| `--network-name` | 虚拟网络名称，所有节点必须相同才能互相发现和通信 |
| `--network-secret` | 网络密码，用于身份认证和通信加密，所有节点必须相同 |
| `--ipv4` | 本节点的虚拟 IPv4 地址，每个节点必须不同（如 10.10.10.1、10.10.10.2 等） |
| `-p tcp://public.easytier.top:11010` | 公共节点地址，辅助 NAT 打洞和节点发现（官方免费提供） |

> `-p` 可指定多个公共节点以提高连接稳定性，也可以用自建节点地址替代。

如需开机自启，可创建 systemd 服务（Linux）：

```bash
# 创建服务文件
cat > /etc/systemd/system/easytier.service << 'EOF'
[Unit]
Description=EasyTier Service
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/easytier-core --network-name my-clawbench --network-secret my-secret-password --ipv4 10.10.10.1 -p tcp://public.easytier.top:11010
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# 启用并启动
systemctl daemon-reload
systemctl enable easytier --now
```

### 步骤三：在 Android 手机上启动 EasyTier

1. 从 [EasyTier 官网下载页](https://easytier.cn/guide/download.html) 或 [GitHub Releases](https://github.com/EasyTier/EasyTier/releases) 下载 Android APK（`app-universal-release.apk`），安装到手机上

2. 打开 EasyTier App，填写组网信息：

| 设置项 | 填写内容 |
|--------|----------|
| **虚拟 IPv4 地址** | `10.10.10.2`（每个节点不同，如手机用 10.10.10.2） |
| **网络名称** | 与服务端一致，如 `my-clawbench` |
| **网络密码** | 与服务端一致，如 `my-secret-password` |
| **公共服务器** | 选择默认的官方公共节点 `tcp://public.easytier.top:11010` |

3. 点击 **运行网络**，等待连接成功

> **注意**：`--network-name` 和 `--network-secret` 必须与服务端完全一致；虚拟 IPv4 地址不能与服务端重复。

> 如果手机同时使用 VPN（如 Clash），请在 EasyTier App 中开启 **无 TUN 模式**，并将 Socks5 端口设置为非冲突端口（如 `15555`），然后在 VPN 中将虚拟网段（`10.0.0.0/8`）路由到 EasyTier 的 Socks5 代理。

### 步骤四：验证组网状态

启动后，可使用 `easytier-cli` 查看组网状态：

```bash
easytier-cli peer list    # 查看已连接的对等节点
easytier-cli route list   # 查看虚拟网络路由
easytier-cli node list    # 查看节点信息
```

### 步骤五：访问 ClawBench

组网成功后，通过 ClawBench 服务器的虚拟 IP 地址访问：

```
http://10.10.10.1:20000
```

无需记忆地址，虚拟 IP 是固定的（只要配置不变）。

### 子网代理（可选）

如果你的 ClawBench 服务器在局域网中，想让远程设备也能访问局域网其他服务，可在启动 EasyTier 时添加子网代理：

```bash
easytier-core --network-name my-clawbench --network-secret my-secret-password \
  --ipv4 10.10.10.1 -p tcp://public.easytier.top:11010 \
  -n 192.168.1.0/24
```

`-n` 参数指定要代理的局域网网段，其他节点即可通过虚拟网络访问该局域网中的设备（如 NAS、打印机等）。

### 配置文件方式（推荐长期使用）

命令行参数适合调试，长期使用建议配置文件方式。先用命令行方式启动，然后导出配置文件：

```bash
easytier-cli node config > config.toml
```

后续使用配置文件启动：

```bash
easytier-core -c config.toml
```

---

## 总结

| 方式 | 优点 | 缺点 | 推荐场景 |
|------|------|------|---------|
| **IPv6 直连** | 无中转、低延迟、零成本 | 需要 IPv6 网络环境、需配置防火墙 | 家庭宽带支持 IPv6 时首选 |
| **FRP 隧道** | — | — | 已移除,功能参考 [FRP 官方文档](https://github.com/fatedier/frp),需独立部署 |
| **EasyTier 组网** | 无需公网 IP、P2P 直连低延迟、去中心化、加密安全 | 依赖公共节点辅助打洞、严格对称型 NAT 可能无法直连 | 多设备异地互联、无需 VPS 的场景 |

三种方式可同时部署互为备份，根据实际网络环境灵活切换。
