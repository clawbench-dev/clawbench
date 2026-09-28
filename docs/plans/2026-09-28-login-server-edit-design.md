# 登录页服务器选择优化：编辑现有服务器 + 单一地址框 — 设计文档

日期：2026-09-28
状态：已实施
范围：`android/app/src/main/assets/`、`desktop/assets/`（两份登录页 + 共享解析器）

## 背景

安卓与 Electron 的登录页（`android/app/src/main/assets/login.html`、`desktop/assets/login.html`）
都让用户通过**协议单选（HTTPS/HTTP）+ 主机 + 端口**三个控件添加服务器，并只支持删除，
**不支持编辑**已保存的服务器。用户想改密码或换地址时，只能删掉重建。

本次做三件事：

1. 增加**编辑现有服务器**功能；
2. 删掉协议单选与端口输入，**完全依靠字符串解析**（一个地址框）；
3. placeholder 改为对应格式的**样例**，教会用户严格格式。

## 决策（已与用户确认）

| 问题 | 选择 |
|---|---|
| 编辑入口 | 每行加**铅笔按钮**（与删除按钮并排，`stopPropagation`） |
| 保存后行为 | **只保存，留在登录页**（不自动连接） |
| 地址变更时 | **视为同一台服务器**：迁移到新地址并保留密码（删旧条目、写新条目） |
| 无协议/无端口 | **必须显式写协议端口**，裸地址视为非法并提示 |
| 添加表单动作 | 改为「添加」**只保存**（与编辑统一；不再「添加并连接」） |
| 地址框 placeholder | `https://192.168.1.100:20000` |

## 接口

### `buildServerUrl(text)`（新增，两份 `url-utils.js` 逐字节相同）

```js
function buildServerUrl(text) {
  var parsed = parseServerInput(text);
  if (!parsed || !parsed.protocol || !parsed.port) return null;
  return parsed.protocol + '://' + parsed.host + ':' + parsed.port;
}
```

- 返回规范化 URL（协议小写、丢弃 path/query/fragment、去首尾空白）；
- **协议与端口缺一即返回 `null`** —— 这是「必须显式写协议端口」的实现，替代旧 UI
  的默认值（https / 20000）；
- 复用既有的宽松 `parseServerInput`（不重写正则），严格性只加在这一层。

### 表单契约

`#addServerForm` 同时承担添加与编辑，靠 **`data-edit-url`** 区分：

- `''`（或缺失）= 添加模式：标题隐藏、按钮「添加」、字段清空；
- 非空 = 编辑模式：标题「编辑服务器」、按钮「保存」、字段预填该条目的 URL 与密码。

提交（添加与编辑共用）：

1. `buildServerUrl(#addAddress.value)`；`null` → 提示 `enter_full_address`，不保存；
2. **重名守卫**：新 URL 若与**另一台**已保存服务器相同 → 提示 `duplicate_server`，
   拒绝保存（否则 `saveServer` 以 URL 为键会静默覆盖对方的密码）；
3. `saveServer(url, password)`；
4. **仅当 `editingUrl && editingUrl !== url`** 才 `removeServer(editingUrl)` ——
   地址未变时删除会把刚存的条目一起删掉，这是本功能最关键的边界；
5. 刷新列表（从原生重读，而非重渲染陈旧的内存数组）、留在登录页。

### 列表行

每行在删除按钮左侧加铅笔按钮，与删除按钮一起收进 `.server-item-actions`：

```js
html += '<button class="server-edit-btn" onclick="event.stopPropagation(); editServer(\'' + escJs(srv.url) + '\')" title="' + t('edit_server') + '">' + SVG_PENCIL + '</button>';
```

## 移除的东西

- 协议单选组（`name="addProtocol"`）及其 CSS（`.protocol-group*`）；
- 端口输入（`#addPort`）与 `.host-row` / `.colon` CSS；
- `#addHost` 改名 `#addAddress`（单框）；
- i18n 键 `protocol`、`add_connect`、`enter_address`（`enter_address` 已无引用）；
- 新增 i18n 键：`add`、`save`、`edit_server`、`enter_full_address`、`duplicate_server`。

`setConnecting(loading, true)` 的按钮文案恢复改为**按当前模式**取 `save` / `add`，
否则编辑保存中途失败后按钮会错误地显示「添加」。

## 平台差异（保持既有模式）

两份页面仍近乎重复，差异只在 bridge 包装：

- Android：`ClawBenchNative.saveServer/removeServer` 是**同步** `@JavascriptInterface`，
  提交后直接刷新列表；
- Electron：两者是 `ipcRenderer.invoke`，返回 Promise，须**串链**：
  `saveServer → (地址变了则 removeServer) → 刷新`，失败走 `.catch` 复位按钮。

blur 监听器两份**逐字节相同**（只调 `buildServerUrl` 与 DOM），由测试守卫。

## 测试

`web/src/__tests__/loginUrlUtils.test.ts`（91 用例）：

- `parseServerInput` 表（两份副本）**保持不动**；
- 新增 `buildServerUrl` 表：完整 URL 通过并规范化；缺协议/缺端口/垃圾/认证信息 → `null`；
- 静态接线：有 `#addAddress` 与样例 placeholder、**无** `addProtocol`/`addPort`/`addHost`、
  有铅笔按钮的**渲染点**（正则匹配 `class="server-edit-btn"` + onclick，避免被 CSS 规则
  假通过）、有 `editServer` 函数；
- **保存不连接**：用花括号配平切出提交处理器（不能用「第一个 `});`」——其体内有嵌套
  `some(function(){...})`），断言含 `buildServerUrl`/`saveServer`/`removeServer` 与
  `editingUrl !== url`，且**不含** `connectToServer`；
- blur 行为测试重写为单框版；两份副本逐字节比对与 Electron 打包断言保留。

变异验证（逐条确认测试会红）：保存路径加回 `connectToServer`；`buildServerUrl` 放宽端口；
放宽协议；桌面改铅笔按钮 class；桌面 blur 不再写回；重命名铅笔 onclick —— 均 1~4 个用例失败。

## 不做的事（YAGNI）

- 不合并两份 `login.html`；不改 Web 端（浏览器端没有这个登录页）；
- 不支持 IPv6 字面量与 URL 认证信息（沿用既有解析器限制）；
- 编辑态不允许改密码以外的「部分字段」——地址与密码整体提交。

## 人工验收

Robolectric 无法执行 JS，自动化只覆盖纯函数与静态接线，需人工确认：

1. 空列表 → 直接进入添加表单，placeholder 显示 `https://192.168.1.100:20000`；
2. 填 `192.168.1.100` 或 `https://192.168.1.100` → 提交报「请输入完整地址」；
3. 填 `https://192.168.1.100:20000` → 失焦后框内规范化，提交后列表出现该条且**未连接**；
4. 点某行铅笔 → 表单预填 URL 与密码、标题「编辑服务器」、按钮「保存」；
5. 只改密码 → 保存后该行仍在（地址未变，不应被删除）、密码已更新；
6. 改地址 → 保存后旧行消失、新行出现、密码保留；
7. 改成一个已存在的地址 → 报「该地址已存在」，不保存；
8. 点「连接」才真正连接。
