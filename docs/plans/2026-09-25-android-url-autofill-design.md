# 登录页粘贴完整 URL 自动解析协议与端口 — 设计文档

日期：2026-09-25
状态：已实施；触发方式经用户要求由 paste 改为 blur（见文末「变更记录」）
分支：`android-url-autofill`（worktree：`.worktrees/android-url-autofill`）

## 背景

安卓与 Electron 客户端的登录页（`android/app/src/main/assets/login.html`、
`desktop/assets/login.html`）都让用户分别填写**协议单选框**、**主机名**、**端口**三个控件。
用户常直接粘贴一个完整 URL（如 `https://192.168.1.100:8443`），目前没有任何解析逻辑——
粘贴进主机框会得到非法的主机值，连接必然失败。本设计只解决这一处体验：把完整 URL 归一化为
协议 + 端口，其余行为完全不变。

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
  > **历史基线（已被取代）**：以上是实施前的勘察结论。实施后 `#addHost` 上已挂 `blur`
  > 监听（见「变更记录」），因此「无任何监听器」不再成立；`url-utils.js` 的脚本标签也已加入。
  > 本节的**机制**（打包方式、加载路径、文件布局）仍然有效；但**行号是勘察时的快照，实施后
  > 已漂移**（如 `hideError` 现为 android `1711` / desktop `1745`，提交处理器现为 android
  > `1457-1488`），引用前必须重新实测，不能直接照搬本节数字。
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

1. **触发方式**：仅在 `#addHost` 上监听 `blur` 事件，不做 `input` 触发。字段只在用户离开后
   才被归一化，因此输入过程中不会被改写；且读取 `document.getElementById('addHost').value`
   而非剪贴板数据，顺带绕开 WebView 对剪贴板访问的限制。
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

| 主机框内容（粘贴或手输，blur 时解析） | 协议单选框 | 主机框 | 端口框 |
|---|---|---|---|
| `https://192.168.1.100:8443` | → https | `192.168.1.100` | `8443` |
| `http://example.com` | → http | `example.com` | 不变（保持 20000） |
| `https://example.com/chat?x=1` | → https | `example.com` | 不变（路径查询丢弃） |
| `192.168.1.100:8080` | 不变 | `192.168.1.100` | `8080` |
| `192.168.1.100` | 不变 | `192.168.1.100` | 不变（普通主机名） |
| `not a url` | 不变 | 不拦截，保持用户原样输入 | 不变 |

补充边界（测试表覆盖）：空串、纯空白、`http://host:80`（显式端口等于默认值也必须生效）、
大写协议 `HTTPS://Host:8443`、尾斜杠 `https://host:8443/`、垃圾串。

## 接口

`parseServerInput(text)` 返回：

```ts
{ protocol: 'http' | 'https' | null, host: string, port: string | null }
```

无法解析时返回 `null`，调用方不拦截、保持字段原样（blur 不可取消，本就无需拦截）。

纯函数，**不引用 `document` / `window` / `ClawBenchNative`**，因此可在裸 `vm` 沙箱中求值
（两份副本共用同一测试表的前提）。

## 不做的事（YAGNI）

- 不合并两份 `login.html`。
- 不改 Web 端 `LoginView.vue`。
- 不做 `input` 触发（blur 已是选定触发方式；`input` 会在每次击键后改写字段，仍属越界）。
- 不支持 IPv6 字面量与 URL 认证信息（`user:pass@`）。
- 不做构建期单一源。

## 风险

- **两份副本仍会漂移**：本设计的表驱动测试只覆盖 `url-utils.js`，不覆盖调用点
  （监听器代码、脚本标签）。缓解：测试里加静态断言，检查两份 `login.html` 都含
  `<script src="url-utils.js"></script>` 且存在引用 `parseServerInput` 的 blur 监听，
  并逐字节比对两份监听器文本。
- **Electron 若忘记加 `extraResources` 条目，页面会静默 404 失去该功能**（无 CSP、
  无报错，只是脚本没加载）。实施计划里必须有一步验证。
- **Robolectric 无法执行 JS**（`ShadowWebView.evaluateJavascript` 是 no-op），
  因此本功能无法用 Android 单测端到端验证，只能静态断言接线 + 人工验收。
- **（已消除）Android WebView 的 `clipboardData` 可用性**：早期 paste 方案需读取
  `e.clipboardData.getData('text')`，而 WebView 对剪贴板事件的暴露并不稳定。改用 blur
  并读取 `document.getElementById('addHost').value` 后，本风险不再存在。

## 已知限制（用户已确认接受，不修）

- **在 `#addHost` 中按 Enter 会拼出错误 URL。** `#addConnectBtn` 是 `#addServerForm`
  内 `type="submit"` 的按钮，因此在主机框里按 Enter 会触发表单的 `submit` 监听器；
  该监听器读取的是**未经解析**的主机值，于是拼出形如
  `https://https://192.168.1.100:8443:20000` 的错误地址。**点击**「连接」按钮不受影响，
  因为点击会先让主机框失焦（blur 先于 submit 触发），blur 监听器已完成归一化。
- 用户已明确审阅该取舍并选择**接受**，不额外加 Enter/keydown 守卫。本设计**不**声称此问题
  已修复，也**不**把加守卫当作必做项。

## 变更记录

- **2026-09-25（commit `c1aa60ee`，"refactor(login): 触发方式由 paste 改为 blur"）**：
  触发方式在初次实施后按用户要求由 `paste` 改为 `blur`。行为表**完全不变**（同样输入得到
  同样解析结果），仅触发时机不同。具体差异：
  - 监听器改为 `document.getElementById('addHost').addEventListener('blur', function() { ... })`，
    读取 `document.getElementById('addHost').value`（不再读 `e.clipboardData.getData('text')`），
    回调不再接收事件参数，也**不再调用 `e.preventDefault()`**（blur 本就不可取消）。
  - 开头注释改为 `// Event: blur on the host field -> normalize a full URL into protocol + port.`。
  - `url-utils.js` 的文档注释相应去掉 "paste" 措辞（`Parse a server address into protocol / host / port.`、
    `so the scheme-less form users actually type cannot go through it.`、
    `treat null as "leave the field alone".`）；函数逻辑未动，两份副本仍逐字节相同。
  - 测试由 51 个增至 **53 个**：paste 版 jsdom 功能测试改写为 blur 版，并移除全部
    `defaultPrevented` 断言。
  - 由此消除的风险与新增的已接受限制见上文「风险」「已知限制」两节。
