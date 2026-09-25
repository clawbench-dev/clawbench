# 登录页粘贴完整 URL 自动解析协议与端口 — 设计文档

日期：2026-09-25
状态：已与用户确认，待实施
分支：`android-url-autofill`（worktree：`.worktrees/android-url-autofill`）

## 背景

安卓与 Electron 客户端的登录页（`android/app/src/main/assets/login.html`、
`desktop/assets/login.html`）都让用户分别填写**协议单选框**、**主机名**、**端口**三个控件。
用户常直接粘贴一个完整 URL（如 `https://192.168.1.100:8443`），目前没有任何解析逻辑——
粘贴进主机框会得到非法的主机值，连接必然失败。本设计只解决这一处体验：粘贴时自动拆出
协议与端口，其余行为完全不变。

## 当前实现事实（已实测验证）

- 两份 `login.html` 是近乎重复的兄弟文件：`diff -u` 共 **13 个 hunk / 115 行差异**
  （约 1750 行中的）。差异分四类，实测行数：
  | 类别 | hunk | 行数 |
  |---|---|---|
  | Electron 异步 bridge 包装 | 7,8,9,10,11 | 59 |
  | `onConnectError` 签名差异 | 12,13 | 19 |
  | i18n 文案 | 1,2,6 | 12 |
  | 注释 | 3,4,5 | 25 |
  | **合计** | | **115** |

  无任何平台 DOM/CSS 分支。
- **行偏移不是常量**。`desktop ≠ android + 11`：实测每个 hunk 的偏移为
  `+0, +0, +0, +4, +6, +11, +11, +13, +21, +25, +33, +34, +31`。
  两个关键锚点：脚本起始标签 android `1394` → desktop `1405`（+11）；
  但提交处理器 android `1456-1487` → desktop `1477-1512`（**+21**）。
  因此引用 desktop 行号**必须实测**，不能靠固定偏移推算。
- 提交处理器目前只做拼接：Android `login.html:1456-1487`，desktop `login.html:1477-1512`。
  逻辑为 `protocol + '://' + host + ':' + port`，端口仅在空时回落 443/80，末尾去 `/`。
- 协议单选框（`name="addProtocol"`，值 `https`/`http`，https 默认 checked，无 id）须用
  `document.querySelector('input[name="addProtocol"][value="http"]')` 选择。
- 主机框 `#addHost`、端口框 `#addPort`（`value="20000"`）**当前没有任何
  input/paste/blur 监听器**，插入点干净。两份文件均无 `paste` 监听（已 grep 确认）。
- 两份文件均**无 CSP**，`<script src="...">` 外部脚本可直接加载。
- 加载方式支持相对路径脚本：
  - Android：`MainActivity.LOGIN_HTML_URL = "file:///android_asset/login.html"`
    （`MainActivity.java:115,363`），相对 `src` 解析到同一 assets 目录。
  - Electron：`window.ts:15` 取 `path.join(process.resourcesPath, 'login.html')`，
    `loadFile(loginPagePath())`（`window.ts:112`），相对 `src` 解析到 `resourcesPath`。
- 打包：Android 的 AGP 默认整目录打包 `src/main/assets/**`（`android/app/build.gradle`
  无任何 `assets`/`sourceSets` 配置），新增文件自动生效；Electron 的
  `desktop/electron-builder.yml` `extraResources`（L7-15）**只复制具名文件**
  （当前仅 `assets/login.html`、`assets/logo.png`），新增文件必须显式加入，否则运行时 404。
- `hideError(id)` 在两份文件中均为顶层全局函数
  （android `login.html:1688`、desktop `login.html:1722`），引用安全。

## 设计决策（已与用户确认）

1. **触发方式**：仅在 `#addHost` 上监听 `paste` 事件。不做 blur/input 触发。
2. **解析范围**：丢弃路径与查询串，只取 scheme/host/port。
3. **端口缺省**：保持现有默认 `20000` 不变；只有 URL 显式带端口时才覆盖。
4. **无协议输入**（如 `192.168.1.100:8080`）也解析出端口，但协议单选框保持不变。
5. **两份文件各自复制一份 `url-utils.js`**（接受重复，不做构建期单一源合并）。
6. **测试**：一个 vitest 表驱动测试文件，同时加载两份 `url-utils.js`，同一组用例分别断言——
   既满足 AGENTS.md 的单测要求，又顺带守住两份副本不漂移。

### 为什么用正则而不是 `new URL()`

`new URL()` 无法统一处理无协议输入（实测）：

| 输入 | `new URL()` 行为 |
|---|---|
| `192.168.1.100:8080` | **抛 `Invalid URL`** |
| `example.com:8080` | 主机被当成协议：`protocol="example.com:"`、`hostname=""` |
| `localhost:20000` | 同上：`protocol="localhost:"`、`hostname=""` |

即点分四段形式直接抛异常，带点/裸标签形式则把主机误判为 scheme——两条路径都无法复用。
单个正则对「有无协议」两种形式行为一致，故采用正则。

## 行为表

| 粘贴内容 | 协议单选框 | 主机框 | 端口框 |
|---|---|---|---|
| `https://192.168.1.100:8443` | → https | `192.168.1.100` | `8443` |
| `http://example.com` | → http | `example.com` | 不变（保持 20000） |
| `https://example.com/chat?x=1` | → https | `example.com` | 不变（路径查询丢弃） |
| `192.168.1.100:8080` | 不变 | `192.168.1.100` | `8080` |
| `192.168.1.100` | 不变 | `192.168.1.100` | 不变（普通粘贴） |
| `not a url` | 不变 | 不拦截，走浏览器默认粘贴 | 不变 |

补充边界（测试表覆盖）：空串、纯空白、`http://host:80`（显式端口等于默认值也必须生效）、
大写协议 `HTTPS://Host:8443`、尾斜杠 `https://host:8443/`、垃圾串。

## 接口

`parseServerInput(text)` 返回：

```ts
{ protocol: 'http' | 'https' | null, host: string, port: string | null }
```

无法解析时返回 `null`，调用方不拦截默认粘贴。

纯函数，**不引用 `document` / `window` / `ClawBenchNative`**，因此可在裸 `vm` 沙箱中求值
（两份副本共用同一测试表的前提）。

## 不做的事（YAGNI）

- 不合并两份 `login.html`。
- 不改 Web 端 `LoginView.vue`。
- 不做 blur/input 触发。
- 不支持 IPv6 字面量与 URL 认证信息（`user:pass@`）。
- 不做构建期单一源。

## 风险

- **两份副本仍会漂移**：本设计的表驱动测试只覆盖 `url-utils.js`，不覆盖调用点
  （监听器代码、脚本标签）。缓解：测试里加静态断言，检查两份 `login.html` 都含
  `<script src="url-utils.js"></script>` 且存在引用 `parseServerInput` 的 paste 监听。
- **Electron 若忘记加 `extraResources` 条目，页面会静默 404 失去该功能**（无 CSP、
  无报错，只是脚本没加载）。实施计划里必须有一步验证。
- **Robolectric 无法执行 JS**（`ShadowWebView.evaluateJavascript` 是 no-op），
  因此本功能无法用 Android 单测端到端验证，只能静态断言接线 + 人工验收。
