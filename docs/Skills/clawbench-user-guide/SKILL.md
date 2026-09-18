---
name: clawbench-user-guide
description: 为 ClawBench 生成桌面端图文使用说明 / 用户手册配图。涵盖 720p 降采样截图参数（2560×1440 视口 + zoom=2 再 LANCZOS 降到 1280×720）、浮层抑制与自检（三个 fixed 浮层的 offsetParent 判据失效陷阱）、演示数据搭建与还原、按模块的取景清单与 DOM 选择器、三段式文档结构。触发词：使用说明、用户手册、user guide、图文说明、功能说明文档、手册配图、文档配图。
allowed-tools: Bash(playwright-cli:*), Bash(curl:*), Bash(python3:*)
---

# ClawBench 桌面端使用说明（截图 + 文档）

为 ClawBench 生成「图文并茂的桌面端使用说明」：在真实实例上截取 720p 功能图，配上按模块组织的说明文字。

> 与 `clawbench-screenshot` skill 的区别：那个是**宣传图/hero 图**导向（1080p、2x、追求好看、无遮挡深色）；这个是**文档配图**导向（720p、信息完整、能看清操作、允许界面里有真实数据）。两者的登录/浮层/主题流程相通，但参数与验收标准不同。

## 一、截图参数（实测确定，别改）

| 项 | 值 | 说明 |
|---|---|---|
| 视口 | **2560×1440** | 设备像素 |
| `documentElement.style.zoom` | **`'2'`** | 有效 CSS 布局 = 1280×720 |
| 截图原始尺寸 | 2560×1440 | |
| 后处理 | `Image.resize((1280,720), Image.LANCZOS)` | 降采样，输出 720p |
| 单图体积 | 约 400–620 KB | PNG optimize |

**为什么用 zoom 超采样而不是 CDP 的 deviceScaleFactor：**
`Emulation.setDeviceMetricsOverride({deviceScaleFactor: 2})` 在本环境**实测不生效**（`devicePixelRatio` 仍是 1，输出还是 1280×720）。改用「视口放大 2 倍 + `style.zoom=2`」可稳定得到 2x 渲染。

**关键：布局等价性已验证。** zoom=2 / 视口 2560 与直接 1280×720 / zoom=1 的归一化几何**逐值相同**（实测侧栏都是 `[1000, 36, 280, 684]`）。所以不会因为超采样而改变布局——只是渲染更锐利。

**不要固定 1280×720 视口 + zoom=1**：文字会有明显锯齿。也不要 zoom=1.25 配 1280 视口——有效宽度降到 1024，正好卡宽屏断点，三栏被压扁。

### 主题必须显式钉成 github-dark

**新浏览器 profile 的默认主题是 `auto`（跟随系统）**。在本环境系统是亮色 → 截图整片白底，与既有图（深色）完全不一致。

```js
localStorage.setItem('clawbench-settings-theme', JSON.stringify('github-dark'));
```

既有图的实测背景色（用于自检）：`#161B22` / `#0D1117` / `#21262D`。

**同时**这个键是 `localStorage`，所以必须**在 reload 之前写**（reload 后由 `useSettingsConfig` 读取生效）。同理适用于分栏比例与两个浮层抑制键——见 §二、§五。

> 用 `--persistent --profile <dir>` 可让 localStorage 跨浏览器重启保留，省去重复登录与重复设置。但实测本机内存紧张时持久化 profile 更容易被 OOM 杀掉，改用「单次调用内完成全部操作」更稳（见 §二十 第 25 条）。

### 登录页是例外

`.login-page` 是 `min-height:100vh` + `overflow:hidden` + flex 居中（`web/src/components/LoginView.vue:326-334`）。内容（品牌区 + 表单）在 720px 高度下放不下，而 `overflow:hidden` **禁止滚动**，表单会被永久裁掉。

- 解法：登录页用 **zoom 1.5**（视口仍 2560×1440），实测内容完整容纳。
- 不要试图 `scrollIntoView` / `window.scrollTo`——`overflow:hidden` 下无效，会静默失败（截图字节数完全不变是征兆）。

## 二、浮层处理（最容易翻车的地方）

### 陷阱：`offsetParent` 判据对 fixed 浮层恒为 false

现有 `clawbench-screenshot` skill 里的 `!!document.querySelector('.up-overlay')?.offsetParent` **是无效检查**——三个浮层全是 `position: fixed`，而 **fixed 元素的 `offsetParent` 恒为 `null`**。实测：弹窗正盖在画面上，该表达式仍返回 `false`。

可靠判据（`web/src/components/UpgradePromptOverlay.vue:83`、`WelcomeOverlay.vue:237`、`CompletionPopover.vue:304` 均为 fixed）：

```js
const vis = (e) => {
  if (!e) return false;
  const cs = getComputedStyle(e), r = e.getBoundingClientRect();
  return cs.display !== 'none' && cs.visibility !== 'hidden'
    && parseFloat(cs.opacity) > 0 && r.width > 0 && r.height > 0
    && (typeof e.checkVisibility === 'function'
        ? e.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true }) : true);
};
```

`elementFromPoint(innerWidth/2, innerHeight/2)` 也可作交叉验证（返回 `up-panel` 即被盖）。

### 三个浮层的抑制

| 浮层 | 抑制方式 |
|---|---|
| `.welcome-overlay` | `localStorage['clawbench_welcome_dismissed']='true'`（reload 前写） |
| `.up-overlay` | `localStorage['clawbench-upgrade-skip']=<latest_version>`；版本升级后旧值失效，须重新取。也可点 `.up-skip` |
| `.completion-popover-backdrop` | **先 `page.reload()`**（首选，见下）；点「标记已读」循环清空只作兜底 |

### 完成通知卡片：**截图前必须 reload**（用户明确要求）

**这是清除卡片的第一手段，不是兜底。** 循环点「标记已读」只清当前队列，仍有三个漏网场景：脚本没写清队列逻辑、清完又有新会话完成、以及最坑的——**整轮 setup 从未执行**（见下文「登录判定」）。

为什么 reload 可靠（已核源码）：

- `useCompletionPopover` 的 `queue` / `active` 是**模块级内存状态**（`web/src/composables/useCompletionPopover.ts:29-30`），reload 即清零。
- 刷新后的 WS replay 会补发历史 `completed` 事件，但 `web/src/App.vue:1209` 有显式拦截：
  ```js
  if (skipReplay && isReplayingEvents.value) return   // 重放阶段不弹窗
  ```
  → **卡片不会复活**。

**实测代价（第一版踩坑）**：`openfile.sh` / `annot.sh` / `termqc.sh` 三个脚本**没有**清卡片逻辑，导致 **22 张**截图带着卡片交付（全部 `preview-*`、`annot-01/02`、`term-04/05`、两个 `dialog-*`、`sess-03/04`）。用户一眼看出「非常碍眼」。

**像素级自检判据**（比 DOM 检查更可靠，因为卡片是 fixed 定位、DOM 上一直在）：

PC 宽屏 1280px 下卡片几何固定（`max-width: min(680px,92vw)` + `inset:0` + `align-items:flex-start`）→ 占 **x=300..980, y=8..220**。取左右竖边框像素比对参考色：

```python
CARD_L=(47,71,100); CARD_R=(48,73,102)   # github-dark 主题下的实测边框色
l = px[300,110]; r = px[979,110]
card = sum(abs(l[i]-CARD_L[i]) for i in range(3)) < 40 and \
       sum(abs(r[i]-CARD_R[i]) for i in range(3)) < 40
```

命中图 Δ=0–30，干净图 Δ=80–180，双峰分离清晰。**交付前必须对全部图跑一遍**，期望只剩 `00-completion-popover.png`（有意演示）。

- 升级弹窗是**延迟出现**的（登录后数秒），DOM 检查跑早了会误判为无弹窗 → 截完图才发现被盖。**截图动作与浮层检查要在同一次 evaluate 内完成**。
- 机会性取材：升级弹窗本身是「应用自升级」特性、完成通知卡是「完成通知弹窗」特性，遇到了就顺手存档（编号 `00-*`）。

### 登录判定：必须用 `/api/me`，不能用 DOM

**最隐蔽的坑：判定「要不要登录」若用 `document.querySelector('input[type=password]')`，在已登录但 SPA 尚未渲染完时可能为真 → `page.click('button.login-btn')` 等 30s 超时 → 整个 `evaluate` 从未执行 → 截图落成上一轮残留图，且**与上一张 md5 相同**。**

实测：一批 5 张设置图（`set-12`~`set-16`）md5 完全一致，全部是同一张陈旧图。

正确写法：

```js
// 用接口判定登录态，超时可控
let meStatus = 0;
try {
  meStatus = await page.evaluate(async () => {
    try { const r = await fetch('/api/me', { credentials: 'include' }); return r.status; }
    catch (e) { return 0; }
  });
} catch (e) { meStatus = 0; }

if (meStatus !== 200) {
  await page.waitForSelector('input[type=password]', { timeout: 15000 }).catch(() => {});
  // ... 有表单才填，填完 page.click('button.login-btn')
}
```

**并且必须加产出防线**：解析脚本输出，若 `setup` 返回字符串错误（含 `MISS` / `not found`）或 `ov.card === true` 或登录异常，就在文件名后打 `[NEEDS REVIEW]` 标记，不要静默写图。否则残留图会被当成新图混进交付。

## 三、演示数据（可新建，不动存量）

用户授权边界：**可新建**演示会话/任务/文件；**只读浏览**现有会话/文件/Git 历史；**不可改**任何存量数据。

- 记基线：`sqlite3 "file:$HOME/.clawbench/ClawBench.db?mode=ro&immutable=1" "select count(*) from chat_sessions;"`
- 演示会话：会话侧栏 `[data-action=create]` → 弹「选择智能体」`.bs-overlay` → 选 Codebuddy → 发**只读型**提问（如「阅读 docs/spec/README.md 介绍模块划分，不要修改任何文件」）。只读提问能自然触发 Read 工具调用 + Markdown 回复，且不会改动仓库。
- 收尾：删除新建会话；`git status` 比对基线。

**注意**：主工作区可能有其他 agent 在跑（实测遇到），他们的会话/未提交改动会出现在截图里。构图时尽量避开，或接受它们作为背景。

## 四、按模块取景清单与选择器

宽屏布局：`.wide-dock`（左竖栏）+ `.col-left`（左内容）+ `.col-right`（聊天）+ `.session-sidebar`（会话侧栏，固定 280px）。

**Dock 按钮无 `data-tab`，只有 `title`**（中文界面）：文件管理器 / 文件 / 项目历史 / 议题与合并请求 / 任务 / 终端 / 数据统计 / 端口映射 / 设置。

| 模块 | 面板根选择器 | 备注 |
|---|---|---|
| 登录 | `.login-form-card` | zoom 1.5；见上文 |
| 文件管理器 | `.browse-panel .file-manager-content` | 条目 `.file-item[data-path][data-action=file\|dir]` |
| 文件预览窗格 | `.fm-preview-pane` | 先点 `title="开启预览模式：单击文件快捷预览"`，再单击文件 |
| Markdown 查看器 | `.file-viewer` / `.markdown-body` | 双击文件打开；TOC 按钮 `title="目录"` → `.toc-dock` |
| 聊天 | `.chat-tab-panel` / `.chat-messages` | 消息 `.chat-message`，工具卡 `.chat-tool-call`、`.tool-detail.chat-inline-card` |
| Git 历史 | `.git-history-content` | 提交行 `.drilldown-item`；文件行 `.git-file-info` → diff 抽屉 |
| 任务 | `.task-tab` | 含事件任务 + 定时任务 |
| 终端 | `.terminal-panel` | xterm；输入焦点 `.xterm-helper-textarea` |
| 数据统计 | `.stats-tab-host` | 三子页：用量统计/代码存量/代码增量 |
| 设置 | `.settings-page` | 首页 `.settings-index__row`（16 分类，两列） |

**TabPanel 用 `v-show` 不是 `v-if`**：切过的面板留在 DOM（opacity:0）。自检必须判 `.tab-panel-active` 或走 `checkVisibility`，不能数元素个数。

**宽屏分栏比例是全局共享的**（`localStorage['clawbench-widescreen-split-ratio']`，默认 0.5），所有左侧页签共用。拖 `.split-view__divider` 调整。不同面板需要不同宽度时分别拖——**注意这是改浏览器状态，不要改源码默认值**。

## 五、720p 的固有构图约束

- **设置首页 16 个分类**（每行 96px，两列）在 648px 可视高度里只能完整显示 13 个，必然截断。接受它，或用滚动分两张。
- **Git 提交列表**同理，只能显示前几条。

### 左栏必须收窄到 441px（用户明确要求）

**默认要求，不是可选项。** 宽屏三栏在 1280px 下：左栏 + 聊天 + 会话侧栏（固定 280px）。

| 左栏宽 | 聊天可见宽 | 评价 |
|---|---|---|
| 615–619px（第一版实际用的） | **334px** | ❌ 用户反馈「聊天消息显示很小」 |
| **441px（正确值）** | **510px** | ✅ 文件名 0 截断，聊天够宽 |

实测：左栏 441px 时，文件管理器 **338 个叶子节点 0 截断**（`scrollWidth > clientWidth` 计数为 0）、Markdown 预览 **922 个节点 0 截断**、表格 **183 个单元格 0 裁剪**（最长 `package-lock.json`、`CONTRIBUTING.en.md` 完整）。

**为什么第一版会跑到 615px**：分栏比例是**全局共享**的（`localStorage['clawbench-widescreen-split-ratio']`），默认 0.5；1280px 视口下容器 1232px，0.5 就是左 616 / 聊天 335。必须显式设置。

**正确设置方式——写 localStorage 比例 + reload**（不要用拖拽事件，脆弱且被 zoom 干扰）：

```js
// 容器 = (colRight.x + colRight.w) - colLeft.x，实测 zoom=2 下 2464 设备px → 1232 逻辑px
// ratio = 441 / 1232 = 0.3580
localStorage.setItem('clawbench-widescreen-split-ratio', String(441 / 1232));
```

**三个必须注意的时序陷阱**（都实测踩过）：

1. **测量必须在「视口 2560 + zoom=2」已生效之后**。若在 `setViewportSize` 之前测，视口还是默认 1280，容器算成 641，比例偏大一倍 → 左栏被拉到 848px、聊天只剩 103px。
2. **必须在 `.col-left` / `.col-right` 渲染完成之后测**。登录后立刻测会拿到 `no layout`，静默沿用旧比例。
3. **改完比例要再 reload 一次**——`useWideScreenLayout` 只在启动时读 localStorage。

比例生效范围：`clampRatio()` 会把左栏限制在 `[MIN_PANEL_WIDTH=320, container-320]`；441px 在此范围内，安全。

> 这是**改浏览器状态**，不要改源码默认值。

## 六、验收方法

**以 DOM 实测尺寸为主判据，视觉模型只作辅助。**

- 视觉模型（mmx vision）在小尺寸整图上**反复误判**：把右侧固定的 280px 会话侧栏当成「聊天区被遮挡」，报「用户消息被遮挡」而 DOM 实测重叠为 **0**；也把不存在的工具调用报成缺失。**不要据它下结论。**
- 可靠判据：`getBoundingClientRect()` 量各栏 x/width，算重叠；用 `scrollWidth > clientWidth` 判文本截断；用 `.filter(e => e.scrollWidth > e.clientWidth + 1)` 数截断条目。
- 视觉模型仍适合：判断「整体观感是否协调」「有没有明显异常」，以及在多张候选图之间做定性比较。

## 七、三段式文档结构

`docs/user-guid/user-guid.md`，中文单文件：

1. **快速上手** — 登录与多服务器、选择项目、界面总览（三栏/页签/会话侧栏）、发起第一次对话
2. **功能详解** — AI 对话（模式切换、斜杠命令、@引用、附件、权限审批、提问卡、推荐回复、分叉/回溯/续接）、会话管理、文件管理、Git 管理、终端、任务、Forge、RAG、用量统计、系统监控、设置
3. **进阶** — 快捷键速查、多项目与 worktree、上下文与摘要、推送通知、常见问题

图片放 `docs/user-guid/screenshots/`，按 `NN-module-name.png` 编号（编号与章节顺序对应）。**桌面端视角**：移动端专属功能（悬浮窗、手势返回、PWA 安装）不写或只作一句说明。

## 八、Vue 交互陷阱（第二版实测补充，最重要的一节）

给 Vue 3 应用做 playwright 截图时，以下陷阱会静默失败（无报错、图拍出来是错的）：

1. **`element.click()` 不触发 Vue 的 Teleport/响应式渲染，必须用 Playwright 真实鼠标点击。**
   - 实测：`document.querySelector('.toolbar-btn').click()` 后 `.toolbar-dropdown` 始终不存在；换成 `const el = await page.$(sel); await el.click()` 立刻出现。
   - **Why:** 菜单用 `<Teleport to="body">` + `v-if`，合成事件在 Vue 的调度链路上不完整。
   - **How to apply:** 任何展开菜单/抽屉/弹窗的操作，一律用 Playwright 的 `el.click()`，不要用 `page.evaluate` 里调 `.click()`。

2. **点击「已激活」的 Dock 页签会折叠左栏**（VS Code 行为）。左栏宽度变 0，截图里只剩聊天区，**且无任何报错**。切页签前必须判断 `b.classList.contains('active')`。

3. **`page.evaluate` 里不能用 `arguments`**；**`new Function()` 不支持 async 函数体**。需要异步就用 `async () => {...}` 箭头函数字符串。

4. **Node 作用域没有 `document`**。`run-code` 的函数体在 Node 里跑，所有 DOM 访问必须包进 `page.evaluate`。典型错误：把 dock 查找写在 `run-code` 顶层 → `ReferenceError: document is not defined`。

5. **浮层遮罩拦截点击。** 完成通知卡的 `.completion-popover-backdrop` 是全屏 fixed 遮罩，Playwright 报 `intercepts pointer events` 并重试。解法：`el.click({ force: true })` 或先清空通知队列。

6. **完成通知是队列且可能持续涌入。** 若同时有别的 agent 在完成任务，通知会不断新增。清 15 个后可能又冒出新的。

7. **`title` 属性是 i18n 文本，且部分按钮 title 与直觉不符。** 实测：网格视图按钮 title 是「图标视图」（不是「切换为网格视图」）、Git 管理入口 title 是「管理」、预览模式 title 是「开启预览模式：单击文件快捷预览」。**先探测实际 title 再写选择器**。

8. **设置分类进入后没有 `.settings-index__row`。** 分类列表在子页消失，脚本必须先用返回按钮回首页，否则第二次点击分类静默失败。

9. **浏览器会话会中途断开**（报 `Browser 'ug' is not open`）。脚本要能重开 + 重登 + 重设主题。**登录后主题会被重置为 `github-light`**（`index.html` 按 `prefers-color-scheme` 判定），必须再写一次 `clawbench-settings-theme` 并 reload。

10. **`@` 提及菜单在此环境不弹出。** 已确认输入框值正确为 `@`、光标在末尾，但 `.completion-menu` 始终不出现（斜杠 `/` 菜单正常）。疑似依赖 `caretVersion` 的事件链在合成输入下不触发。**不要在这上面耗时间**。

11. **按 Escape 不一定能关抽屉**（尤其 `.bs-overlay`）。可靠做法：点遮罩（`.bs-overlay`）或调 `document.body.click()`，然后断言 `document.querySelectorAll('.bs-overlay')` 可见数为 0。

## 九、逐子功能取景清单（第二版）

按模块细化，每个大模块下的子功能都要单独出图：

**文件管理器**：工具栏 / 排序菜单 / 多选模式 / 网格视图 / 文件右键菜单 / 目录右键菜单 / 预览窗格 / 搜索三模式
**文件查看器**：Markdown+TOC / 代码编辑 / 图片灯箱 / PDF / Office / 分享弹窗
**AI 对话**：输入栏全貌 / 斜杠命令菜单 / @引用 / 附件抽屉 / 消息操作按钮 / 推荐回复 / 工具卡展开 / 权限审批 / 提问卡 / 深度思考 / 会话设置抽屉
**会话**：侧栏全貌 / 搜索抽屉 / 标签过滤 / 右键菜单
**Git**：提交列表 / 分支 / 标签 / 工作树 / diff 抽屉 / 工作区变更
**任务**：列表 / 定时表单 / 事件表单（含事件类型勾选）/ 详情 / 执行历史
**Forge**：动态 / 议题 / 合并 / 流水线 / 详情 / 绑定弹窗
**终端**：标签栏 / 虚拟键工具栏 / 主题选择器 / 键位配置 / 帮助抽屉 / 快速指令
**统计**：用量统计（含筛选与图表切换）/ 代码存量 / 代码增量
**设置**：**16 个分类逐个出图**（外观/项目与文件/聊天/智能体/终端/语音朗读/语音识别/AI 摘要/会话搜索/端口映射/内网穿透/GitHub GitLab 集成/推送通知/安全/调试/关于）

## 十、设置分类截图脚本要点

设置面板有「首页（分类列表）」与「分类子页」两种状态。可靠流程：

```js
// 1. 打开设置（已激活则不重复点）
const b = [...document.querySelectorAll('.wide-dock .dock-btn')].find(x => x.title === '设置');
if (b && !b.classList.contains('active')) b.click();
// 2. 若不在首页，先返回
if (!document.querySelector('.settings-index')) {
  document.querySelector('.settings-page__header button')?.click();
}
// 3. 点分类行（用 startsWith 匹配，文本含计数）
[...document.querySelectorAll('.settings-index__row')]
  .find(x => (x.textContent||'').trim().startsWith(name))?.click();
```

## 十一、截图目录与引用校验

- 输出到 `docs/user-guid/screenshots/`，按 `<模块前缀>-<序号>-<名称>.png` 命名。
- **写完文档必须校验引用**：用 `comm` 双向比对文档里 `grep -o` 出的路径与实际文件，确认**无缺失、无冗余**。
- 迭代过程中会留下被新图取代的旧图，及时删除未引用项。

## 十三、测试素材库（补拍各类型文件预览用）

**`test/` 目录下有全套可直接预览的素材，不要自己造文件：**

| 目录 | 内容 |
|---|---|
| `test/office/` | `docx_sample1-4.docx`、`xlsx_sample1-3.xlsx`、`pptx_sample_1mb.pptx` 等 |
| `test/pdf/` | `sample-local-pdf.pdf`、`pdf-with-toc.pdf` |
| `test/media/` | `voice-demo.mp4`、`voice-demo.mp3` |
| `test/images/` | 多张 jpg + svg |
| `test/markdown/` | `table-demo.md`、`formula-demo.md`（LaTeX）、`mermaid-demo.md`、`images-demo.md`、`code-block-demo.md` |
| `test/openapi/` | `petstore.yaml` 等 |
| `test/excalidraw/` | `demo.excalidraw` |

**这些是人工测试文件，只读使用，绝对不要删除或修改。**

### 打开指定文件的可靠流程

1. 先关查看器：`.file-header-back-btn`（否则无法导航）
2. 切到文件管理器（已激活则不重复点）
3. **点面包屑第一段回项目根**（否则从当前目录出发找不到目标路径）
4. 逐级双击目录进入
5. 双击文件打开

**`data-path` 是相对项目根的完整路径**（如 `test/office/demo1.docx`），进入子目录后不会变成裸文件名。匹配用 `dp === name || dp.endsWith('/' + name)`。

### 各类型的 DOM 判定（用于断言渲染成功）

| 类型 | 判定 |
|---|---|
| PDF | `[class*=pdf]` 存在且 `.file-viewer canvas` 存在 |
| Office | `[class*=office]` 存在 |
| Excel | `.file-viewer canvas` 存在 |
| 媒体 | `.file-viewer audio` 或 `video` 存在 |
| OpenAPI | `[class*=openapi], [class*=swagger]` 存在 |
| Excalidraw | `iframe[src*=excalidraw]` 存在 |
| Mermaid | `.file-viewer svg` 数量 > 0（实测 31 个） |

## 十四、顶栏下拉菜单（不是 bs-overlay）

顶栏的项目切换、分支切换用 `AppMenuPanel` 渲染，类名是 **`.app-menu`**，**不是** `.bs-overlay`。用 `.bs-overlay` 判可见性会永远为 false。

另外顶栏按钮常被**完成通知遮罩挡住**——实测 `document.elementFromPoint()` 在项目按钮坐标处返回 `completion-popover-backdrop`。此时 `el.click()` 无效，需 `el.click({ force: true })`。

## 十六、快捷发送 / 快捷指令（两处易混淆的入口）

| 功能 | 存储表 | 入口 | 菜单类名 |
|---|---|---|---|
| **聊天快捷发送** | `chat_quick_send` | **清空输入框后点 `.chat-send-btn`** | `.quick-send-item`（在 `.popup-menu` 内） |
| **终端快捷指令** | `terminal_quick_commands` | `button[title="快捷指令"]` | 同上 |

两者菜单结构相同：条目列表 + 末尾「编辑」项。点「编辑」打开各自的 `.bs-overlay` 抽屉（`bs-overlay-wide-auto`），抽屉内含拖拽手柄（≡）、右上角「+」新增、「✨」消息聚类（仅聊天）、「⋮」导入导出。

**踩坑：**

1. **发送按钮常被浮层挡住**——实测 `elementFromPoint` 在按钮坐标返回 `ctx-overlay`（右键菜单遮罩）、`modal-overlay`（如「设置标签」弹窗）、`completion-popover-backdrop`（完成通知）。**必须先清这些遮罩**，否则点击静默无效。
2. **`page.evaluate` 里调 `.click()` 有时能开菜单（快捷指令），有时不能（快捷发送）**——不确定时两条路都试：先试 `page.evaluate` 内的 `.click()`，不行再试 Playwright 的 `el.click()`。
3. **不要点菜单项后立刻断言**——菜单是 Teleport 渲染，需 `waitForTimeout(1500+)`。
4. **终端快捷指令按钮在页面里有 3 个同名 `button[title="快捷指令"]`**（顶栏 + 工具栏重复渲染），取 `[0]` 即可。

## 十七、会话标签设置（弹窗 + 过滤栏）

### 弹窗

入口：会话条目**右键 → 设置标签**。弹窗容器是 **`.session-tags-dialog`**（在 `.modal-overlay` 内），**不是** `.bs-panel`/`.bs-overlay`——用后者判可见性会误判为「弹窗没打开」。

内部结构：

| 元素 | 选择器 | 说明 |
|---|---|---|
| 已有标签卡片 | `.st-chip` | 选中态加 `active` 类；名称在 `.st-chip-name`，删除按钮是卡片内的 icon |
| 新建标签输入框 | `.session-tags-dialog input` | 输入后点「+ 创建」 |
| 范围选择 | 按钮文本「仅本项目」/「全局」 | 默认「仅本项目」 |
| 底部 | `.modal-footer` 内的「取消」/「确定」 | 拍完记得点取消，避免写入 |

**可靠流程（单次 `run-code` 内完成，分步做会被 `snap.sh` 的 zoom 重设打断）：**

```js
// 1. 用 dispatchEvent 开右键菜单（Playwright 的 click 会被可操作性等待卡住）
await page.evaluate(() => {
  const row = document.querySelector('.session-row');
  const r = row.getBoundingClientRect();
  row.dispatchEvent(new MouseEvent('contextmenu', {
    bubbles: true, clientX: r.x + 30, clientY: r.y + r.height / 2, button: 2,
  }));
});
await page.waitForTimeout(1200);
// 2. 点「设置标签」（element.click 对 Teleport 菜单有效）
await page.evaluate(() => {
  [...document.querySelectorAll('.context-menu-item')]
    .find(x => (x.textContent||'').trim() === '设置标签')?.click();
});
await page.waitForTimeout(2200);
// 3. 选中 chip（element.click 有效，Playwright 的 el.click 会被拦截）
await page.evaluate(() => document.querySelector('.session-tags-dialog .st-chip')?.click());
```

**要点：**
- 右键菜单项是 `.context-menu-item`（文本「置顶 / 重命名会话 / 设置标签 / 归档」），**没有「删除会话」**。
- 会话行上用 Playwright `el.click()` 会被拦截（行内有 hover 才出现的按钮），但 `el.click({force:true})` 又**不会派发真实事件**导致菜单不出现 → **用 `dispatchEvent` 开右键菜单**是唯一稳定路径。
- 分步操作时弹窗会消失：`snap.sh` 每次都会重设 `zoom`，触发 Vue 重渲染。**打开 → 选中 → 截图必须在同一次 `run-code` 里**。

### 过滤栏

- 数据源 `GET /api/ai/session/tags?inUse=1&project_path=...`，只返回**未归档会话正在使用**的标签。
- **若返回空数组，过滤栏就该隐藏**——这是正确行为，不是缺陷。此时截不到过滤栏，不要反复尝试。
- 要拍到过滤栏，必须有一个**未归档且带标签**的会话。当前库里那条带标签的会话是 `archived=1`（它本身就是标签功能的实现会话），所以拍不到。
- **归档是单向的**（`DELETE /api/ai/session/archive`，无「取消归档」端点），所以不要为了截图去改归档状态。

### 数据安全

标签相关的**全部操作都在真实库上**（`session_tags` / `session_tag_links`）。截图时：
- 只**打开弹窗**看，不点「确定」——点「取消」关闭，数据零改动。
- 验证方式：操作前后比对 `select count(*) from session_tags` 与 `session_tag_links`，并确认没有新增输入的标签名。

## 十八、文件导出与分享

### 入口

导出与分享**都在文件查看器的「更多」菜单里**（工具栏 `button[title="更多"]`）：

| 菜单项 | 说明 |
|---|---|
| 文件详情 / 打开目录 / 文件历史 | 常规 |
| **分享链接** | 打开 `.modal-dialog`（`ShareLinkDialog.vue`） |
| **导出 HTML** | 仅 Markdown 文件可用 |
| 删除 | 危险操作 |

菜单容器是 **`.file-header-dropdown-menu`**，项是 **`.dropdown-item`**。打开方式：先点「更多」，再点菜单项（`element.click()` 对这类菜单有效）。

### 分享弹窗的两种状态（要分别出图）

`ShareLinkDialog` 按 `linkUrl` 是否存在渲染两套 footer：

| 状态 | 关键选择器 | 按钮 |
|---|---|---|
| **未生成** | `.share-dialog-hint`（说明文案） | 「生成链接」（`.fbtn-primary`） |
| **已生成** | `.share-dialog-link-input`（只读链接框） | 「打开页面」+「关闭分享」（`.fbtn-danger`） |

- 链接栏内还有两个图标按钮：复制（`.share-dialog-link-btn`）与重新生成（同上，第二个）。
- 生成链接会**真实写入 `file_shares` 表**，记得拍完撤销（见下）。

### 分享页面（独立 SPA）

链接形如 `https://<host>/share/<32位token>`。用**独立浏览器会话**打开（未登录态才能体现「无需登录」）：

```bash
playwright-cli -s=sh open --browser=chrome "https://<host>/share/<token>"
SESSION=sh ./snap.sh "share-page"
```

页面 `document.title` 是 **`ClawBench Share`**，可按此断言。只读渲染 + 右侧 TOC + 下载入口。

### 已分享文件抽屉

入口：文件管理器工具栏 `button[title="已分享文件"]`。容器是 `.bs-panel`，内部：

| 元素 | 选择器 |
|---|---|
| 一键清空 | `.shared-files-clear` |
| 单条复制链接 | `.shared-file-btn` |
| 单条取消分享 | `.shared-file-btn.danger` |

### 撤销分享（必做，恢复数据）

**UI 上的「取消分享」按钮经常点不动**——实测 `element.click()` 无效，Playwright `el.click()` 报 `intercepts pointer events`。查 `document.elementFromPoint()` 发现是 **`.completion-popover-backdrop`（完成通知遮罩）** 挡着。

**可靠做法是直接调 API**（与 UI 同一端点）：

```js
// DELETE /api/share?path=<绝对路径>   —— 注意是 query 参数 path，不是 body 里的 token
await page.evaluate(async () => {
  const res = await fetch('/api/share?path=' + encodeURIComponent('/abs/path/to/file'), {
    method: 'DELETE', credentials: 'include',
  });
  return res.status; // 200 + {"ok":true}
});
```

**踩过的坑**：我最初按 body `{token}` 提交，返回 400 `缺少路径`（`MissingPath`）。读 `file_share.go` 的 `shareRequestPath` 才确认 **DELETE 走 query `path`，POST/PUT 才读 body**。

### 数据安全

分享是**写入真实库**的操作（`file_shares` 表）。流程：

1. 拍前记基线：`select count(*) from file_shares;`
2. 生成链接 → 拍弹窗与分享页 → 拍抽屉
3. **撤销并验证回到基线**
4. 校验：`select count(*), group_concat(name) from file_shares;`

`file_shares` 的列是 `token / path / name / created_at / root`（**没有 `id`**，别按 id 查）。

## 十九、标注与跳转系统（截图要点）

四类标注的 DOM 选择器与触发条件：

| 类型 | 选择器 | 点击后 | 触发前提 |
|---|---|---|---|
| 文件路径 | `.chat-file-path[data-path-type="file"]` | 打开查看器并定位行 | `data-path-type` 必须是 `file`/`dir`（路径已验证存在） |
| 代码链接预览 | 同上 | 弹 `.code-preview-*` 浮层 | **必须先开「预览模式」** |
| Commit | `.chat-commit-open-btn[data-commit-sha]` | 切项目历史 + 定位提交详情 | — |
| Worktree | `.chat-worktree-btn[data-worktree-path]` | 打开 worktree 文件 | — |
| localhost | `.chat-url-open-btn[data-url][data-port]` | 建隧道 + 开 WebView | — |

**关键坑：**

1. **代码链接预览的 `enabled` 门控在「预览模式」上，不是全局设置。** `FileManagerContent.vue:900` 传的是 `enabled: computed(() => filePreviewMode.value)`。不开启预览模式，点路径只会跳查看器，浮层永远不弹。**必须先点** `button[title="开启预览模式：单击文件快捷预览"]`。
2. **浮层根类名不是 `.code-preview-sheet`**（那是移动端 BottomSheet 模式的类）。PC 下实际渲染的是 `.code-preview-header` / `.code-preview-line-row` / `.code-preview-line-code` 等**没有统一根类**的一组元素。判断浮层是否弹出用 `document.querySelectorAll('[class*=code-preview]').length > 0`，不要找单一根节点。
3. **标注元素在虚拟滚动容器里会被回收**——`el.scrollIntoView()` 后立刻取 `getBoundingClientRect()` 可能拿到 `{x:0,y:0,w:0,h:0}`。滚动与取坐标要在同一次 `page.evaluate` 内完成，或直接用 Playwright 的 `el.click()`。
4. **聊天区可能被折叠**（`colRight` 宽 0），此时聊天内所有元素尺寸为 0、点击无效。检查 `.wide-dock-bottom .dock-btn` 的 `aria-pressed`，必要时先点它展开。
5. **commit 标注点击用「页面内派发 click」最可靠**：`el.dispatchEvent(new MouseEvent('click',{bubbles:true,clientX,clientY}))` 能成功触发跳转；纯 `page.mouse.click` 在虚拟滚动下常落空。
6. **标注管线在流式期间不运行**——流式中只做纯 Markdown 渲染，回复结束才启动标注。所以截图前必须等流式结束。

## 二十、踩坑速查（汇总）

1. `offsetParent` 判浮层 → fixed 元素恒 null，**完全失效**（第一版最重要的一条）
2. 升级弹窗延迟出现 → 检查与截图必须同一 evaluate
3. 完成通知是队列 → 循环关到空
4. CDP `deviceScaleFactor` 不生效 → 用 zoom 超采样
5. 登录页 `overflow:hidden` → 不能滚动，只能降 zoom
6. 截图存进项目目录会污染工作区 → 先放 `/tmp`，定稿再拷
7. 主工作区有其他 agent → 截图里会出现他们的会话与改动
8. `playwright-cli eval` 里不能用 `arguments` → 用 python 生成字面量
9. 会话行点击目标是 `.session-item`（不是 `.session-row`）
10. `run-code` 里的循环必须写在 `page.evaluate` 内
11. **`element.click()` 不触发 Teleport 渲染 → 用 Playwright `el.click()`**
12. **点已激活的 Dock 页签会折叠左栏（无报错）**
13. **设置子页没有分类列表 → 先返回首页**
14. **登录后主题被重置为 light → 重写 localStorage 并 reload**
15. **打开文件前先关查看器 + 回项目根**，否则目录导航静默失败
16. **顶栏菜单是 `.app-menu` 不是 `.bs-overlay`**
17. **完成通知遮罩会挡住顶栏 → 用 `click({ force: true })`**
18. **Git 提交行是 `.drilldown-item`**，点第一项会进「工作区变更」而非提交；点索引 1+ 才是真实提交
19. **PC 布局下终端没有「工具栏配置」和「终端操作帮助」按钮**（仅移动端布局有），不要照文档硬找
20. **视觉模型（mmx）在小尺寸整图上频繁误判** —— 实测把已渲染的 Mermaid 报成「聊天内容」、把视频播放器报成「静态图片」、把提交详情报成「文件树」。**以 DOM 实测为准**，mmx 只用于「整体观感是否协调」这类定性判断。
21. **聊天快捷发送入口是「空输入时点发送按钮」**，不是单独的按钮；终端快捷指令按钮有 3 个同名元素
22. **点菜单/抽屉前先清 `ctx-overlay`、`modal-overlay`、`completion-popover-backdrop` 三种遮罩**，否则点击静默失败

### 第二版补充（返工实测）

23. **完成通知卡片：截图前 `page.reload()`**（首选手段）——卡片是内存队列，reload 即清；`App.vue:1209` 跳过 replay 的历史 completed 事件，故不会复活。第一版三个脚本没做这件事，交付了 **22 张带卡片的图**。判据见 §二（像素级 x=300/979, y=110）。**交付前对全部图跑一遍检测。**
24. **左栏默认收窄到 441px**（聊天 510px）。默认比例 0.5 在 1280 视口下是左 616 / 聊天 **334px**，太窄。设置方式＝写 `localStorage['clawbench-widescreen-split-ratio'] = 441/1232` 再 reload；**测量必须在 zoom=2 且三栏已渲染之后**。见 §五。
25. **`cap.sh` 式的多命令往返在本机不可靠** —— 有 11+ 个并发 `codebuddy --acp` agent（各 ~750MB），可用内存常 < 5GB，playwright 的 chrome 会在两条命令之间被 OOM 杀掉（`Browser 'ug' is not open` / `Session closed`）。**解法：把 open → 登录 → 设置 → setup → 截图压进一次 `run-code`**，并内置重试。
26. **登录判定必须用 `/api/me` 而非 DOM**。用 `input[type=password]` 判会在已登录时误判 → `page.click('button.login-btn')` 等 30s 超时 → **整个 evaluate 从未执行，截图落成上一轮残留图**（实测 5 张设置图 md5 完全相同）。并且**产出侧要加防线**：setup 返回错误字符串 / `ov.card===true` 时给文件名打 `[NEEDS REVIEW]`。
27. **`page.evaluate` 的页面上下文没有裸 `setTimeout`** —— 必须 `window.setTimeout`。统一在 setup 包装里注入 `const sleep = ms => new Promise(r => window.setTimeout(r, ms))`，否则每段 setup 各自声明会撞 `Identifier 'sleep' has already been declared`。
28. **右键菜单只认「图标或名称」命中区** —— 在行的 padding / 元信息列上右键会落到**空白区菜单**（粘贴/新建文件/新建文件夹/在此打开终端，仅 4 项）。文件菜单 9 项、目录菜单 11 项。命中区选择器：`.file-icon-wrap, .grid-thumb, .file-name, .grid-name`。用 `.file-name` 派发 `contextmenu` 最稳。
29. **Git「管理」入口按钮 title 就是「管理」**（`git.manage.title`），点开后是「提交列表 › 管理」，内部三个标签：分支 / 标签 / 工作树。`git-02/03/04` 分别是这三个标签页。
30. **终端 Dock 页签会因状态未加载而缺席** —— `isTerminalDisabled` 来自 `/api/terminal/status` 的异步结果；页面长期未刷新时该页签不渲染。**reload 后即正常出现**。实测终端按钮有 3 个同名 `button[title="快捷指令"]`。
31. **主题必须显式钉成 `github-dark`**（新 profile 默认 `auto` → 跟随系统亮色，整片白底）。见 §一。
32. **交付前检查图片唯一性** —— `md5sum *.png | awk '{print $1}' | sort | uniq -d`。第二版发现 `annot-04-filepath-jump.png` 与 `share-01-dialog-initial.png` **md5 完全相同**（既有错配：前者应为「文件路径跳转后的查看器」，实际却是分享弹窗），已重截修正。**"文件数对得上"不代表内容对**。
