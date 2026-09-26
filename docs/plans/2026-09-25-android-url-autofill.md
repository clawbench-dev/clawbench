# 登录页粘贴 URL 自动解析协议与端口 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 让安卓与 Electron 登录页在用户把完整 URL 填入主机框后（粘贴或手输，离开字段时），自动解析出协议并填充协议单选框与端口框。

**Architecture:** 把纯解析逻辑抽到一个无 DOM 依赖的全局函数 `parseServerInput(text)`，放在两份 `login.html` 各自的同级 `url-utils.js` 中；`login.html` 通过 `<script src="url-utils.js">` 加载，并只在 `#addHost` 上挂 `blur` 监听调用它。两份副本各自独立，用一个表驱动 vitest 同时断言两份以保证不漂移。

**Tech Stack:** 原生浏览器 JS（无框架）、vitest（仓库根 `npm test`，jsdom 环境）、Android Gradle assets、electron-builder extraResources

---

> **修订说明（务必先读）**：本计划最初以 **`paste`** 为触发方式编写并实施；此后按用户要求
> 在 commit **`c1aa60ee`**（"refactor(login): 触发方式由 paste 改为 blur"）改为 **`blur`**。
> 行为表**完全不变**（同样输入得到同样解析结果），仅触发时机不同。本次变更的要点：
> 监听器改为 `addEventListener('blur', function() {...})`，读取
> `document.getElementById('addHost').value`（不再读 `e.clipboardData.getData('text')`），
> 回调不再接收事件参数、**不再调用 `e.preventDefault()`**（blur 不可取消）；开头注释改为
> `// Event: blur on the host field -> normalize a full URL into protocol + port.`；
> paste 版 jsdom 功能测试改写为 blur 版并移除全部 `defaultPrevented` 断言，测试数由 51 增至 **53**。
> 正文各 Task 的「预期输出」数字（28 / 30）是**当时中间态**的实测值，当前测试文件为 **53 个用例**
> （已实测 `Tests 53 passed (53)`）。
> 另有一项**用户已确认接受**的限制：`#addConnectBtn` 是 `type="submit"`，在 `#addHost` 里按 Enter
> 会走表单 `submit` 监听器并拼出错误 URL（点击连接按钮因先触发 blur 而不受影响）；用户选择接受，
> 不加 Enter/keydown 守卫。设计文档
> `docs/plans/2026-09-25-android-url-autofill-design.md` 的「变更记录」「已知限制」两节为权威记录。
>
> 下文正文中的行号为**实施前的勘察值**（会随插入内容而漂移，已就地标注当前实测位置）。
> 文中的内联代码块（监听器、静态断言、验证 grep）**已整体替换为 blur 版**，以免读者照抄一份
> 已废弃的实现；仅「修订说明」及历史叙述中保留 `paste` 字样。当前权威实现以已提交的
> `login.html` / `url-utils.js` 为准。

---

**工作目录（所有命令都在这里执行）：** `/root/code/clawbench/.worktrees/android-url-autofill`

设计文档：`docs/plans/2026-09-25-android-url-autofill-design.md`（本计划的行号引用均来自该分支实测）。

## 环境前置：node_modules

该 worktree **没有 `node_modules`**，`npx vitest` 会 `Failed to resolve import "vue"`。

已核实：`vitest` 是**仓库根** `package.json` 的 devDependency（`"vitest": "^4.1.8"`），
且 `vitest.config.ts` 明确支持「worktree + symlink node_modules」布局
（`webNodeModulesReal` 的 `fs.allow` 逻辑，注释见该文件 L6-17）。

仓库既有 worktree 的做法是**符号链接**而非 `npm install`（`.worktrees/multi-server-android`
即如此，且 `.worktrees/` 在 `.gitignore` L58 被忽略，链接不会被提交）。在 Task 1 之前执行一次：

```bash
cd /root/code/clawbench/.worktrees/android-url-autofill
ln -s /root/code/clawbench/web/node_modules web/node_modules
mkdir -p node_modules && cd node_modules
for d in /root/code/clawbench/node_modules/* /root/code/clawbench/node_modules/.[!.]*; do
  b=$(basename "$d"); [ -e "$b" ] || ln -s "$d" "$b"
done
```

（第二条 glob 覆盖 `.bin`、`.vite` 等点开头目录；漏掉 `.bin` 会导致 `npx vitest` 找不到可执行文件。）

已实测：链接后从 worktree 根运行
`npx vitest run web/src/__tests__/coverageScriptPaths.test.ts` → `5 passed`。

> **注意 `vitest.config.ts` 的 `exclude`**：它含 `'**/.worktrees/**'`（L91）。这意味着
> **必须从 worktree 根运行**（cwd = `.worktrees/android-url-autofill`，相对路径不含
> `.worktrees/`，不会命中排除）。若从主 checkout `/root/code/clawbench` 运行并传
> `.worktrees/...` 路径，测试文件会被静默排除、报 `No test files found`。
>
> 若符号链接不可行（例如 CI 全新 checkout），退路是在 worktree 根 `npm install`——
> 但那会完整装一遍依赖，较慢；优先用上面的链接方案。

---

## Task 1: 建立测试基建并写失败测试

**Files:**
- Create: `web/src/__tests__/loginUrlUtils.test.ts`

**为什么放这里：** `web/src/__tests__/` 是「读磁盘上仓库文件」的既有测试位置，先例
`web/src/__tests__/coverageScriptPaths.test.ts`（用 `node:fs` + `node:child_process`）。

**先例的路径解析写法（已读 `coverageScriptPaths.test.ts` L25-34，照抄这个 idiom）：**

```ts
function readScript(): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    try {
      return readFileSync(resolve(base, 'scripts/check-frontend-coverage.sh'), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`check-frontend-coverage.sh not found from cwd: ${process.cwd()}`)
}
```

要点：它**不用 `import.meta.url` / `__dirname`**，而是拿 `process.cwd()` 及其父目录逐个
试读——因为 vitest 的 cwd 是仓库根（这里是 worktree 根），相对路径即仓库根相对路径。
新测试照此实现一个 `readRepoFile(rel)`，把先例里的固定文件名换成参数。

**Step 1: 写测试文件（内容如下，完整照抄）**

```ts
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { createContext, runInContext } from 'node:vm'

const ANDROID_UTILS = 'android/app/src/main/assets/url-utils.js'
const DESKTOP_UTILS = 'desktop/assets/url-utils.js'

/**
 * Locate a repo file from the vitest cwd. Mirrors the idiom in
 * coverageScriptPaths.test.ts: try the cwd, then its parent. No import.meta.url
 * — vitest runs with the repo root as cwd, so relative paths are repo-relative.
 */
function readRepoFile(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    try {
      return readFileSync(resolve(base, rel), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

interface Parsed {
  protocol: 'http' | 'https' | null
  host: string
  port: string | null
}

/**
 * Evaluate a url-utils.js in a bare vm sandbox and return its global
 * parseServerInput. A bare sandbox (no DOM, no Node globals) is deliberate:
 * the file must not touch document/window/ClawBenchNative, and this fails
 * loudly if it does.
 */
function parserFor(rel: string): (text: string) => Parsed | null {
  const sandbox: Record<string, unknown> = {}
  createContext(sandbox)
  runInContext(readRepoFile(rel), sandbox, { filename: rel })
  const fn = sandbox.parseServerInput
  if (typeof fn !== 'function') throw new Error(`${rel} did not define parseServerInput`)
  return fn as (text: string) => Parsed | null
}

/**
 * One table, asserted against both copies. This is what keeps the two
 * url-utils.js files from drifting: a change to either alone fails here.
 */
const CASES: Array<[string, Parsed | null]> = [
  // Behavior table from the design doc.
  ['https://192.168.1.100:8443', { protocol: 'https', host: '192.168.1.100', port: '8443' }],
  ['http://example.com', { protocol: 'http', host: 'example.com', port: null }],
  ['https://example.com/chat?x=1', { protocol: 'https', host: 'example.com', port: null }],
  ['192.168.1.100:8080', { protocol: null, host: '192.168.1.100', port: '8080' }],
  ['192.168.1.100', { protocol: null, host: '192.168.1.100', port: null }],
  ['not a url', null],
  // Extra edges.
  ['', null],
  ['   ', null],
  // An explicit port equal to the scheme default is still explicit.
  ['http://host:80', { protocol: 'http', host: 'host', port: '80' }],
  ['HTTPS://Host:8443', { protocol: 'https', host: 'Host', port: '8443' }],
  ['https://host:8443/', { protocol: 'https', host: 'host', port: '8443' }],
  // Garbage / unsupported forms.
  ['http://', null],
  ['ftp://host', null],
  ['https://user:pass@host:8443', null],
]

describe.each([
  ['android', ANDROID_UTILS],
  ['desktop', DESKTOP_UTILS],
])('parseServerInput (%s copy)', (_label, rel) => {
  for (const [input, expected] of CASES) {
    it(`${JSON.stringify(input)} -> ${JSON.stringify(expected)}`, () => {
      expect(parserFor(rel)(input)).toEqual(expected)
    })
  }
})
```

**Step 2: 运行测试，确认失败**

```bash
cd /root/code/clawbench/.worktrees/android-url-autofill
npx vitest run web/src/__tests__/loginUrlUtils.test.ts
```

**预期输出：** 28 个用例全部失败（14 用例 × 2 份副本），退出码 1，失败原因是
`android/app/src/main/assets/url-utils.js not found from cwd: ...`（文件还不存在）。
已实测该形态：`Test Files 1 failed (1)` / `Tests 28 failed (28)` / exit 1。

---

## Task 2: 实现 Android 的 url-utils.js

**Files:**
- Create: `android/app/src/main/assets/url-utils.js`

**要求：** 纯全局函数，**不得**引用 `document` / `window` / `ClawBenchNative`
（Task 1 的裸 `vm` 沙箱会因此失败）。

**Step 1: 写文件（内容如下，完整照抄）**

```js
/**
 * Parse a server address into protocol / host / port.
 *
 * Pure and dependency-free on purpose: it must be evaluable in a bare sandbox
 * (no DOM, no ClawBenchNative). Deliberately NOT built on `new URL()`:
 *   new URL('192.168.1.100:8080')  -> throws Invalid URL
 *   new URL('example.com:8080')    -> protocol "example.com:", hostname ""
 * so the scheme-less form users actually type cannot go through it. A single
 * regex treats both "with scheme" and "without scheme" uniformly.
 *
 * Returns { protocol: 'http'|'https'|null, host: string, port: string|null },
 * or null when the input is not a server address we understand. Callers must
 * treat null as "leave the field alone".
 */
function parseServerInput(text) {
  if (typeof text !== 'string') return null;
  var s = text.trim();
  if (!s) return null;

  var protocol = null;
  var rest = s;

  // Optional scheme. Anything other than http/https is not ours to interpret.
  var schemeMatch = rest.match(/^([A-Za-z][A-Za-z0-9+.-]*):\/\//);
  if (schemeMatch) {
    var scheme = schemeMatch[1].toLowerCase();
    if (scheme !== 'http' && scheme !== 'https') return null;
    protocol = scheme;
    rest = rest.slice(schemeMatch[0].length);
  }

  // Drop path, query and fragment — only scheme/host/port are meaningful here.
  rest = rest.split(/[/?#]/)[0];

  var host = rest;
  var port = null;
  var portMatch = rest.match(/^(.*):(\d+)$/);
  if (portMatch) {
    host = portMatch[1];
    port = portMatch[2];
  }

  // Host must look like a hostname or IPv4 literal. This is also what rejects
  // 'not a url', 'http://' (empty host) and 'user:pass@host' (contains '@').
  if (!/^[A-Za-z0-9._-]+$/.test(host)) return null;

  return { protocol: protocol, host: host, port: port };
}
```

**Step 2: 运行测试，确认 Android 半边通过**

```bash
npx vitest run web/src/__tests__/loginUrlUtils.test.ts
```

**预期输出：** `parseServerInput (android copy)` 的 14 个用例**通过**；
`parseServerInput (desktop copy)` 的 14 个仍失败（`desktop/assets/url-utils.js not found`）。
整体仍是失败状态（`Tests 14 failed | 14 passed`）。

---

## Task 3: 复制到 desktop 并加入打包配置

**Files:**
- Create: `desktop/assets/url-utils.js`（与 Android 版**逐字节相同**）
- Modify: `desktop/electron-builder.yml`

**Step 1: 复制（不要手抄，避免引入差异）**

```bash
cp android/app/src/main/assets/url-utils.js desktop/assets/url-utils.js
cmp android/app/src/main/assets/url-utils.js desktop/assets/url-utils.js && echo IDENTICAL
```

**预期输出：** `IDENTICAL`

**Step 2: 加入 extraResources 条目**

先读当前文件确认布局。`desktop/electron-builder.yml` L7-15 现状（已实测）：

```yaml
extraResources:
  - from: assets/login.html
    to: login.html
  # The first-run login page renders <img src="logo.png"> next to login.html, so
  # the logo must ship as a sibling resource. Without it the image is broken and
  # the browser falls back to the alt text, which the decorative ring then
  # clips — that is why the page showed a cropped "ClawBenc".
  - from: assets/logo.png
    to: logo.png
```

这是**扁平布局**：所有资源都直接落在 `resources/` 下（`to:` 不带目录），因为
`desktop/src/main/window.ts:15` 读的是 `path.join(process.resourcesPath, 'login.html')`，
而 `login.html` 里用的是相对路径 `src="url-utils.js"`。因此新条目必须同样扁平。

在 `logo.png` 条目之后追加两行：

```yaml
  - from: assets/url-utils.js
    to: url-utils.js
```

改完后 L7 起应为：

```yaml
extraResources:
  - from: assets/login.html
    to: login.html
  # The first-run login page renders <img src="logo.png"> next to login.html, so
  # the logo must ship as a sibling resource. Without it the image is broken and
  # the browser falls back to the alt text, which the decorative ring then
  # clips — that is why the page showed a cropped "ClawBenc".
  - from: assets/logo.png
    to: logo.png
  - from: assets/url-utils.js
    to: url-utils.js
```

**注意 `to:` 必须是 `url-utils.js`（不带 `assets/`）**：`login.html` 里写的是
`<script src="url-utils.js">`，与 `login.html` 同级。写成 `assets/url-utils.js` 会
404，且因为无 CSP、无报错，故障是静默的。

**Step 3: 运行测试，确认两半都通过**

```bash
npx vitest run web/src/__tests__/loginUrlUtils.test.ts
```

**预期输出：** `Test Files 1 passed (1)` / `Tests 28 passed (28)`，exit 0。

---

## Task 4: 接入 Android login.html

**Files:**
- Modify: `android/app/src/main/assets/login.html`

**Step 1: 插入脚本标签**

已实测：主 `<script>` 起始标签在 `login.html:1394`，其前一行 `1393` 是空行，
再往前是确认对话框的收尾 `1392:  </div>`。在 `1394` 之前插入一行：

```html
<script src="url-utils.js"></script>
```

即改为（L1392 起）：

```html
  </div>

<script src="url-utils.js"></script>
<script>
  var connecting = false;
```

**Step 2: 加 blur 监听**

> 下述代码块已更新为**当前 blur 版**（原计划为 paste 版，见文首「修订说明」）。
> 行号为实施前勘察值；当前 android 的监听器（含开头注释）位于 `login.html:1490-1510`。

已实测：提交处理器在 `login.html:1456-1487`（`document.getElementById('addServerForm').addEventListener('submit', ...)`），
紧接其后的 `1489` 是 `// Event: show add form` 注释。把新监听插在
**提交处理器之后、`// Event: show add form` 之前**（即原 `1488` 空行处）。

已核实 `hideError` 是顶层全局函数（`login.html:1688`），可直接调用。

插入的完整代码：

```js
    // Event: blur on the host field -> normalize a full URL into protocol + port.
    // Blur, not paste/input: the field is only rewritten after the user leaves it,
    // so typing is never disturbed mid-keystroke. Reading .value (rather than
    // clipboard data) also sidesteps WebView clipboard restrictions.
    document.getElementById('addHost').addEventListener('blur', function() {
      var parsed = parseServerInput(document.getElementById('addHost').value);
      // Not a URL we understand -> leave the field exactly as the user typed it.
      if (!parsed) return;

      hideError('addErrorMsg');
      document.getElementById('addHost').value = parsed.host;
      // Only override the port when the URL carried one explicitly; the field
      // keeps its default (20000) otherwise.
      if (parsed.port !== null) {
        document.getElementById('addPort').value = parsed.port;
      }
      // No scheme in the input -> leave the radio untouched.
      if (parsed.protocol !== null) {
        document.querySelector('input[name="addProtocol"][value="' + parsed.protocol + '"]').checked = true;
      }
    });
```

插入后该区域应为：

```js
    });

    // Event: blur on the host field -> normalize a full URL into protocol + port.
    // ...（如上）
    });

    // Event: show add form
    document.getElementById('addServerBtn').addEventListener('click', function() {
```

**Step 3: 加静态接线断言到同一个测试文件**

Robolectric **无法执行 JS**（`ShadowWebView.evaluateJavascript` 是 no-op），
Android 单测无法端到端验证本功能。因此在 `web/src/__tests__/loginUrlUtils.test.ts`
**末尾追加**以下 describe（同时覆盖 desktop，Task 5 后一并变绿）：

```ts
const ANDROID_LOGIN = 'android/app/src/main/assets/login.html'
const DESKTOP_LOGIN = 'desktop/assets/login.html'

/**
 * Static wiring guard. Robolectric cannot execute JS (ShadowWebView
 * .evaluateJavascript is a no-op), so the blur handler cannot be exercised
 * end-to-end in a unit test. These assertions catch the wiring being forgotten
 * — a missing <script> tag or an unwired listener — which would otherwise ship
 * silently (no CSP, no error, the feature just does nothing).
 */
describe('login.html wiring', () => {
  for (const [label, rel] of [
    ['android', ANDROID_LOGIN],
    ['desktop', DESKTOP_LOGIN],
  ] as const) {
    it(`${label} loads url-utils.js and wires a blur listener on #addHost`, () => {
      const html = readRepoFile(rel)
      expect(html).toContain('<script src="url-utils.js"></script>')
      expect(html).toMatch(/getElementById\('addHost'\)\.addEventListener\('blur'/)
      expect(html).toContain('parseServerInput')
    })
  }
})
```

**Step 4: 运行测试**

```bash
npx vitest run web/src/__tests__/loginUrlUtils.test.ts
```

**预期输出：** 28 个解析用例仍通过；`login.html wiring > android ...` **通过**；
`login.html wiring > desktop ...` **失败**（desktop 尚未接线）。
`Tests 1 failed | 29 passed`。

**Step 5: 人工验收（JS 无法单测，必须人工确认）**

按 Task 6 的验证清单构建并运行 App 实测 blur 归一化行为（粘贴或手输后离开字段触发）。

---

## Task 5: 接入 desktop login.html

**Files:**
- Modify: `desktop/assets/login.html`

**Step 1: 插入脚本标签**

已实测：主 `<script>` 起始标签在 desktop `login.html:1405`（android 1394，偏移 +11）。
在其前插入同一行：

```html
<script src="url-utils.js"></script>
```

**Step 2: 加 blur 监听**

> 下述代码块已更新为**当前 blur 版**（原计划为 paste 版，见文首「修订说明」）。
> 当前 desktop 的监听器（含开头注释）位于 `login.html:1515-1535`。

**不要用 +11 推算行号**——实测两文件行偏移不是常量（+0/+4/+6/+11/+13/+21/+25/+33/+34/+31）。
desktop 的提交处理器在 `login.html:1477-1512`（android 1456-1487，偏移 **+21**），
其后 `1514` 是 `// Event: show add form`。先 `grep -n "Event: show add form"` 确认锚点，再插入。

监听器代码与 Task 4 **逐字节相同**（已核实 desktop 的 DOM 契约完全一致：
`name="addProtocol"` L1336/1340、`#addHost` L1350、`#addPort` L1352、`#addErrorMsg` L1373、
全局 `hideError` L1722）。它**不触碰 bridge**，因此不存在 Android 那份的异步差异——
desktop 与 android 的差异集中在 bridge 包装、`onConnectError`、i18n、注释四类，
本监听器不属于任何一类。

```js
    // Event: blur on the host field -> normalize a full URL into protocol + port.
    // Blur, not paste/input: the field is only rewritten after the user leaves it,
    // so typing is never disturbed mid-keystroke. Reading .value (rather than
    // clipboard data) also sidesteps WebView clipboard restrictions.
    document.getElementById('addHost').addEventListener('blur', function() {
      var parsed = parseServerInput(document.getElementById('addHost').value);
      // Not a URL we understand -> leave the field exactly as the user typed it.
      if (!parsed) return;

      hideError('addErrorMsg');
      document.getElementById('addHost').value = parsed.host;
      // Only override the port when the URL carried one explicitly; the field
      // keeps its default (20000) otherwise.
      if (parsed.port !== null) {
        document.getElementById('addPort').value = parsed.port;
      }
      // No scheme in the input -> leave the radio untouched.
      if (parsed.protocol !== null) {
        document.querySelector('input[name="addProtocol"][value="' + parsed.protocol + '"]').checked = true;
      }
    });
```

**Step 3: 运行测试，确认全绿**

```bash
npx vitest run web/src/__tests__/loginUrlUtils.test.ts
```

**预期输出：** `Test Files 1 passed (1)` / `Tests 30 passed (30)`，exit 0。

**Step 4: 验证两份副本未漂移（Task 3 只保证 url-utils.js 相同，这里确认监听器也相同）**

```bash
grep -A16 "addEventListener('blur'" android/app/src/main/assets/login.html > /tmp/a.txt
grep -A16 "addEventListener('blur'" desktop/assets/login.html > /tmp/d.txt
diff /tmp/a.txt /tmp/d.txt && echo "LISTENERS IDENTICAL"
```

**预期输出：** `LISTENERS IDENTICAL`

（`-A16` 恰好覆盖从 `addEventListener('blur'` 到收尾 `});` 的整块——再多一行就会吃到下一个
handler 的注释。测试文件里的 `login.html blur listener copies > are byte-identical` 用例用
`// Event: blur on the host field` 作锚点做同样的比对，二者互为补充。）

---

## Task 6: 最终验证与提交

**Step 1: 运行新测试文件（唯一的必跑测试）**

```bash
cd /root/code/clawbench/.worktrees/android-url-autofill
npx vitest run web/src/__tests__/loginUrlUtils.test.ts
```

**预期输出：** `Tests 30 passed (30)`，exit 0。

**Step 2: 检查项取舍（先读 `scripts/pre-push-checks.sh` 与 `AGENTS.md` 得出的结论）**

已读 `scripts/pre-push-checks.sh`：它跑 Go lint/test/build、前端 typecheck/lint:css、
Go 覆盖率门槛、前端覆盖率门槛、Android lint、Android 覆盖率、前端 build。本改动**不含 Go 代码**，
且 `AGENTS.md` 明确警告：全量 vitest 约 14 分钟、`npm run build` 1.5–4 分钟，
**二者绝不可并发**，且「若一个验证动作不改变结论，就不该跑」。据此：

| 检查 | 是否跑 | 理由 |
|---|---|---|
| `npx vitest run web/src/__tests__/loginUrlUtils.test.ts` | **跑** | 唯一能证明本功能正确性的检查 |
| Go 全部（lint / test / build / 覆盖率） | **跳过** | 本改动零 Go 变更，不改变结论 |
| `npm run build` | **跳过** | 本改动不进 Vue 主包（`url-utils.js` 在 `android/`、`desktop/` 下，不在 `web/src/`），构建产物不变 |
| 全量 vitest / 前端覆盖率门槛 | **跳过** | 14 分钟且不改变结论；新测试文件已单独跑过 |
| Android 覆盖率门槛 | **跳过** | 本改动不新增 Java/Kotlin，覆盖率不变 |
| `npm run lint`（eslint） | **可不跑** | 已核实 `web/eslint.config.js` L16-17 忽略 `**/*.test.ts`，而 `url-utils.js` 不在 `web/src/`（eslint 只扫 `src/`），故对本次改动是 no-op |
| `npm run typecheck` | **可不跑** | 已核实 `web/tsconfig.json` `exclude: ["src/**/__tests__/**"]`，新测试文件不被 typecheck |
| yml lint | **不存在** | 已核实 `scripts/` 下只有 `lint-android.sh` 与 `lint-go.sh`，无 yml lint 工具 |

**Step 3: 提交**

```bash
git add android/app/src/main/assets/url-utils.js \
        android/app/src/main/assets/login.html \
        desktop/assets/url-utils.js \
        desktop/assets/login.html \
        desktop/electron-builder.yml \
        web/src/__tests__/loginUrlUtils.test.ts
git status --short   # 确认只含上述文件，node_modules 链接不出现在列表中
git commit -m "feat(login): 粘贴完整 URL 自动解析协议与端口"
git rev-parse --short HEAD
```

**预期：** `git status --short` 只列上述 6 个文件（`node_modules`/`web/node_modules`
被 `.gitignore` 忽略）。提交后报告 commit hash。

---

## 验证清单

人工验收（Robolectric 无法执行 JS，自动化测试只覆盖纯解析函数与静态接线）：

1. 构建并运行客户端（至少其一，建议两者都验）：
   - Android：`./build.sh` 后安装 APK，或直接 `cd android && ./gradlew assembleDebug` 装机。
   - Electron：在 `desktop/` 下打包运行（**务必确认 `extraResources` 生效**——
     打包产物里要有 `resources/url-utils.js`；若缺失，页面会静默失去该功能）。
2. 打开登录页 → 点「添加服务器」展开表单，逐条把内容填入主机框（粘贴或手输均可），
   **然后让主机框失焦**（点表单别处或 Tab 到下一个控件），确认：

| 主机框内容 | 期望协议单选框 | 期望主机框 | 期望端口框 |
|---|---|---|---|
| `https://192.168.1.100:8443` | https 选中 | `192.168.1.100` | `8443` |
| `http://example.com` | http 选中 | `example.com` | `20000`（不变） |
| `https://example.com/chat?x=1` | https 选中 | `example.com` | `20000`（不变） |
| `192.168.1.100:8080` | 保持原选中项不变 | `192.168.1.100` | `8080` |
| `192.168.1.100` | 保持原选中项不变 | `192.168.1.100` | 不变 |
| `not a url` | 不变 | 保持用户原样输入 | 不变 |

3. 确认归一化后**没有残留错误提示**（`hideError('addErrorMsg')` 生效）。
4. 确认触发方式**只有 blur**：**手动逐字输入**主机名时，输入过程中字段不会被改写，
   只有离开字段（失焦）后才归一化。
5. 点「添加并连接」，确认拼出的 URL 正确（如 `https://192.168.1.100:8443`）。

> **已知限制（已接受，非缺陷修复项）**：`#addConnectBtn` 是 `type="submit"`，在 `#addHost`
> 中按 **Enter** 会触发表单 `submit`，此时读到的仍是**未归一化**的主机值，拼出的 URL 错误
> （如 `https://https://192.168.1.100:8443:20000`）。**点击**「连接」按钮不受影响——点击会先
> 让主机框失焦，blur 先于 submit 触发，归一化已完成。用户已审阅并选择**接受**该取舍，未加
> Enter/keydown 守卫；验收时请按「点击连接」路径确认，不要期待 Enter 也正确。
