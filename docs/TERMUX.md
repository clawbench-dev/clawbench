# 在 Termux（Android）上运行 ClawBench

ClawBench 可以运行在 Android 手机/平板的 **Termux** 终端模拟器里。Termux 提供完整的 Linux 用户态，ClawBench 的 Go 后端（`linux-arm64` 纯 Go 二进制，无 CGO）可以直接执行，配合内置的 Web 前端，手机上就能获得一个完整的 AI 工作站。

> **English Summary**: ClawBench can run inside Termux on Android. Termux provides a full
> Linux userland, so the pure-Go `linux-arm64` backend runs natively. Since Termux reports
> `process.platform === "android"` to Node, the npm launcher maps `android` → `linux` and
> auto-installs the `linux-arm64` platform package — no manual steps required.

## 前置条件

- 已安装 [Termux](https://f-droid.org/repo/com.termux.app.apk)（建议从 F-Droid 安装）
- Termux 内置的存储权限已开通（可选，用于持久化数据目录）
- 手机为 **arm64** 架构（绝大多数现代手机都满足；可用 `uname -m` 确认，返回 `aarch64` 即支持）

## 一、安装依赖

在 Termux 终端执行：

```bash
pkg update && pkg upgrade -y
pkg install -y nodejs-lts git
```

## 二、安装 ClawBench

```bash
npm install -g @xulongzhe/clawbench
```

npm 会自动识别环境并安装匹配的平台包（`android` 环境 → `linux-arm64` 二进制），无需手动指定。

> 如果在安装时看到 `postinstall` 脚本被 `allow-scripts` 阻止的警告，**可以忽略**——那是可选的
> 平台包兜底提示；真正的平台二进制是通过 `optionalDependencies` 安装的，不受该限制影响。

## 三、运行

```bash
clawbench
```

首次启动会在终端打印访问地址和默认密码，默认端口为 `20000`。保持终端在前台运行（或使用
`clawbench &` / tmux 等放到后台）。

### 访问方式

- **本机访问**：Termux 里打开任意浏览器访问 `http://localhost:20000`
- **局域网其他设备访问**：ClawBench 默认绑定 `0.0.0.0`，同一 WiFi 下用手机局域网 IP 访问，
  例如 `http://192.168.1.100:20000`

### 验证安装

```bash
clawbench --version
```

## 四、可选：从源码构建

如果想自行构建最新版（需要 Go ≥ 1.25）：

```bash
pkg install -y golang
git clone https://github.com/clawbench-dev/clawbench
cd clawbench
go build -o clawbench ./cmd/server
./clawbench
```

也可以复用仓库的交叉编译脚本在任意机器上构建后拷贝二进制到 Termux：

```bash
./build.sh --linux-arm64
```

## 注意事项

- **内存占用**：RAG 向量检索、会话摘要、系统监控等功能对内存较敏感，低配手机建议关闭
  不必要的模块（通过 Web 设置页或 `config.yaml`）以获得更流畅的体验。
- **AI CLI 后端**：ClawBench 需要调用 CodeBuddy、Claude Code 等 CLI 工具，这些需要你在
  Termux 里单独安装并配置各自的 API Key。
- **非 root 设备**：Termux 默认在 proot 环境运行，纯 Go 服务器功能正常；个别依赖系统级
  调用的功能可能受限。
- **网络**：手机局域网 IP 需与访问设备处于同一网络；如需公网访问，参见 [PUBLIC_ACCESS.md](PUBLIC_ACCESS.md)。
