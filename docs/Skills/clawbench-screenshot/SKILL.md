---
name: clawbench-screenshot
description: 用浏览器自动化给 ClawBench 产品界面截高清宣传图 / 功能截图。涵盖登录、抑制欢迎/升级/完成通知浮层、设置深色主题与 UI 缩放、在文件浏览器打开指定文档（含 Markdown TOC）、桌面宽屏三栏布局下的 1080p/2x 截图，以及把桌面端截图嵌入 product_hero.html 宣传图的流程。触发词：截图、截一张、产品截图、宣传图、hero 图、hero截图、桌面端截图、三端截图、ClawBench 截图。
allowed-tools: Bash(playwright-cli:*), Bash(curl:*)
---

# ClawBench 产品截图（Browser Automation）

用 `playwright-cli` 驱动真实浏览器访问正在运行的 ClawBench 实例，截取产品宣传 / 功能截图。截图目标是**干净、无遮挡、深色主题、能直接放进 README 或 Hero 宣传图**的高清画面。

## 前置准备

- ClawBench 服务在跑（默认 `https://<host>:20000`，配置见 `~/.clawbench/config/config.yaml` 的 `password` / `port`）。
- 登录密码从 `~/.clawbench/config/config.yaml` 的 `password:` 字段读取。
- `playwright-cli` 已安装（`command -v playwright-cli`，未装则 `npm install -g @playwright/cli@latest`）。
- 服务走 HTTPS 时直接用真实域名（如 `https://xulongzhe.top:20000`）访问，证书有效即可；`playwright-cli open` 会自签忽略报错前先用域名。

## 关键经验（必须先读，否则反复被浮层干扰）

1. **每次 `playwright-cli open` 都是全新内存 profile，localStorage 为空** → welcome / 升级弹窗会重新弹出。必须在**刷新页面之前**写入抑制值，让应用初始化时直接跳过；弹窗出现后再去关是治标不治本（CompletionPopover 有队列、dismiss 会消费下一个）。
2. 三个持久化键（在登录后、`reload` 前通过 eval 设置）：
   - `theme` = `github-dark`（配合 `clawbench-settings-theme` = `JSON.stringify('github-dark')`）
   - `clawbench_welcome_dismissed` = `'true'`（抑制「欢迎使用 ClawBench / 智能体扫描」面板）
   - `clawbench-upgrade-skip` = **服务器最新版本号**（抑制「发现新版本」弹窗）。⚠️ 该值必须等于 `/api/upgrade/check` 返回的 `latest_version`，版本升级后旧 skip 值会失效，需重新查再写。弹窗文本里可直接读到版本号。
3. 画面里若出现右上角「文件链接分享…」完成卡片 / 顶部「AI 任务完成」浮层，是另一个 agent 的 CompletionPopover：点击 `.completion-popover-backdrop` 的空白处可关闭，但会排队重现——最稳的办法是清理后立刻截图。
4. UI 缩放（可选放大 1.25 让元素更大更清晰）直接改 DOM：`document.documentElement.style.zoom = '1.25'`。**reload 会丢失**，需在截图前重新设置。
5. 桌面端桌面模式：PC 单击文件只选中，**双击才打开**。用 `dispatchEvent(new MouseEvent('dblclick', {bubbles:true}))` 最可靠。

## 流程

### 1. 打开浏览器并登录

```bash
cd /tmp   # 工作目录自选，playwright-cli 产物落在 cwd/.playwright-cli/
playwright-cli -s=shot open --browser=chrome "https://xulongzhe.top:20000/"
playwright-cli -s=shot fill e16 "密码"        # e16 是密码框（以最新 snapshot ref 为准）
playwright-cli -s=shot click e17              # 「登 录」
```

### 2. 设窗口 + 抑制浮层（必须在 reload 之前）

```bash
playwright-cli -s=shot resize 1920 1080
playwright-cli -s=shot eval "() => {
  localStorage.setItem('theme', 'github-dark');
  localStorage.setItem('clawbench-settings-theme', JSON.stringify('github-dark'));
  localStorage.setItem('clawbench_welcome_dismissed', 'true');
  // 升级弹窗版本号：先看弹窗里写的 latest，或 fetch /api/upgrade/check
  localStorage.setItem('clawbench-upgrade-skip', '0.89.0');
  return 'set';
}"
playwright-cli -s=shot reload
sleep 3
```

> 若升级弹窗已出现：点「跳过此版本」按钮（它调用 skipVersion 持久化），再把对应值写进 localStorage。

### 3. 验证干净状态

```bash
playwright-cli -s=shot eval "() => ({
  theme: document.documentElement.getAttribute('data-theme'),
  wel: !!document.querySelector('.welcome-overlay')?.offsetParent,
  up: !!document.querySelector('.up-overlay')?.offsetParent,
  pop: !!document.querySelector('.completion-popover-backdrop')?.offsetParent
})"
# 期望：theme=github-dark，wel/up/pop 全 false
```

### 4. 打开目标文档（如 README.md）

```bash
# 文件浏览器在项目根目录时，直接双击项目内文件（data-path 相对项目根）
playwright-cli -s=shot eval "() => {
  const it = document.querySelector('.file-item[data-action=file][data-path=\"README.md\"]');
  if (it) { it.dispatchEvent(new MouseEvent('dblclick', {bubbles:true})); return 'opened'; }
  return 'not found';
}"
sleep 3
# 逐级目录进入：先 dblclick data-action=dir 的目录项，再打开目标文件
```

### 5. 打开 Markdown TOC

```bash
playwright-cli -s=shot eval "() => {
  const b = document.querySelector('button[title=\"目录\"]');
  if (b) { b.click(); return 'toc on'; }
  return 'no toc btn';
}"
sleep 1
# 验证：.toc-dock 可见且有链接
```

### 6. 可选：UI 缩放放大元素

```bash
playwright-cli -s=shot eval "() => { document.documentElement.style.zoom = '1.25'; return 'zoomed'; }"
sleep 1
```

### 7. 截图并 DOM 自检

**1080p**（1x，viewer 用）：
```bash
playwright-cli -s=shot screenshot
# 输出在 cwd/.playwright-cli/page-*.png（1920×1080）
```

**高清 2x**（4K，宣传素材 / 缩放查看用）：
```bash
playwright-cli -s=shot run-code "async page => {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1920, height: 1080, deviceScaleFactor: 2, mobile: false });
  await page.waitForTimeout(1500);
  await page.evaluate(() => { const p = document.querySelector('.completion-popover-backdrop'); if (p && p.offsetParent) p.click(); });
  await page.waitForTimeout(500);
  await page.screenshot({ path: '/tmp/clawbench_shot.png' });
  return await page.evaluate(() => ({
    pop: !!document.querySelector('.completion-popover-backdrop')?.offsetParent,
    up: !!document.querySelector('.up-overlay')?.offsetParent,
    toc: !!document.querySelector('.toc-dock')?.offsetParent,
    md: !!document.querySelector('.markdown-body')?.offsetParent,
    ss: !!document.querySelector('.session-sidebar')?.offsetParent
  }));
}"
```

期望返回 `pop:false up:false toc:true md:true ss:true`（或按目标场景取舍）。

### 8. 用 mmx 自查

```bash
mmx vision describe --file <png路径> --prompt "检查截图是否包含 X / 有无浮层遮挡 / 有无布局问题"
```

> ⚠️ mmx 对小尺寸整图常误判（曾把深色显示器部件误报为「不存在」、把布局读错）。**以 DOM 自检为准**，mmx 只作辅助；拿不准就裁剪局部放大再问。

### 9. 收尾

```bash
playwright-cli -s=shot close
```

## 桌面宽屏「有内容」场景速查

目标画面：左=文件/文档，中=AI 聊天，右=钉住的会话列表，整体深色、无浮层。

- 会话侧栏钉住由应用状态持久化，登录后一般自动恢复；若未恢复，先在聊天 tab 打开 SessionDrawer 再点其 pin 按钮（`data-action="pin"`）。
- 中间聊天有内容的会话由「当前项目 + 上次会话」决定；若要指定会话，在会话侧栏 `.session-sidebar .session-item` 里点击目标项。
- 截图前确认五件事（DOM）：`.markdown-body` 可见、`.toc-dock` 可见、`.chat-tab-panel` 有文本长度、`.session-sidebar` 可见、无 pop/up/wel。

## Hero 宣传图（product_hero.html）制作要点

- 源文件 `docs/screenshots/product_hero.html`（1920×1080 画布），素材在 `docs/screenshots/product_hero_assets/`；设备截图素材为纵向长图，经 `object-fit: cover` 裁入横屏/竖屏设备框。
- 改完用 `python3 -m http.server 8899 --directory docs/screenshots` 起静态服务，再 `playwright-cli open http://127.0.0.1:8899/product_hero.html`，**务必 `resize 1920 1080`**（viewport 默认 1280×720 会只截到画布一角）。
- 桌面端截图素材即上文流程产物（推荐 1080p 的桌面三栏画面）。
- 已踩坑：在已铺满 brand/tablet/phone/features/footer 的现有画布里「插入」一台显示器，会和前景设备互相遮挡、支架被盖。三端融合需整体重排或把显示器作为后景完整露出，勿在拥挤布局上硬叠。
