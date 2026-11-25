/*
 * ClawBench landing page — 交互与双语。
 *
 * 双语策略：HTML 里写中文作为默认内容（无 JS 也能读），JS 加载后按
 * localStorage / navigator.language 决定是否切到英文。这样首屏不会
 * 出现空白，也避免把整页文案塞进 JS 才能渲染。
 */
(function () {
  'use strict';

  /* ── 文案表 ─────────────────────────────────────────────────
     只放与中文原文不同的项。英文缺项时自动回退中文，不会出现空节点。 */
  var EN = {
    skip: 'Skip to main content',

    'nav.platforms': 'Platforms',
    'nav.features': 'Features',
    'nav.download': 'Download',
    'nav.deploy': 'Deploy',
    'nav.gallery': 'Screenshots',
    'nav.faq': 'FAQ',
    'nav.docs': 'Docs',
    'nav.cta': 'Get started',
    'nav.aria': 'Main navigation',
    'nav.menu': 'Open menu',
    'menu.aria': 'Mobile navigation',
    'lang.toggle': 'Switch language',
    'strip.aria': 'Supported AI backends',
    'dep.aria': 'Install methods',
    'foot.aria': 'Footer navigation',

    'hero.eyebrow': 'Open source · MIT · Single binary',
    // 断行位置按英文节奏排：主语一行、谓语一行，"One workbench." 不拆开。
    'hero.title': 'From your palm<br>to your desk.<br><em>One workbench.</em>',
    'hero.sub':
      'ClawBench wraps <strong>25</strong> AI coding agents — CodeBuddy, Claude Code, Codex and more — into a single web workbench. Execution stays on your server, the UI runs anywhere, and <strong>one session</strong> follows you across phone, tablet and desktop.',
    'hero.cta1': 'Get started',
    'hero.cta2': 'View source',

    // Hero 界面稿里的文案

    'stat.agents': 'AI agents',
    'stat.themes': 'Themes',
    'stat.langs': 'Languages highlighted',
    'stat.deploy': 'Deployment',
    'stat.deployVal': '1 binary',

    'strip.label': 'One interface for the AI agents everyone uses',

    'plat.kicker': 'One server, many screens',
    'plat.title': 'A single backend,<br>three kinds of client.',
    'plat.lede':
      'The Go server bundles the frontend, the API and the data into one binary. Run it on a PC, drop it in Docker, or run the whole thing inside Termux on an Android phone.',
    'plat.phone.t': 'Phone',
    'plat.phone.d':
      'Android app, PWA, or the server itself running under Termux. Touch-first interactions that keep the input area clear of the soft keyboard.',
    'plat.phone.m': 'Android · PWA · Termux',
    'plat.tablet.t': 'Tablet',
    'plat.tablet.d':
      'Wide screens switch to a multi-column layout: files on the left, the document in the middle, the conversation on the right. Fingers and trackpads both work.',
    'plat.tablet.m': 'Responsive columns · draggable split',
    'plat.desktop.t': 'Desktop',
    'plat.desktop.d':
      'The Electron shell reuses the same UI and adds native notifications, context menus, SSH port forwarding, screen sharing and in-app self-upgrade.',
    'plat.desktop.m': 'Windows · macOS · Linux',

    'mob.kicker': 'Phone · Android app or any browser',
    'mob.title': 'A dev machine<br>in your pocket.',
    'mob.lede':
      "On the subway, on the couch, in the five minutes waiting for coffee — pull out your phone and pick up yesterday's conversation. Sessions don't break when you switch devices.",
    'mob.b1': 'Full session history, resumed from the same point on any device',
    'mob.b2': 'Input area rebuilt for touch and soft keyboards, never covering content',
    'mob.b3': 'Files, Git, terminal and tasks all reachable from one bottom bar',
    'mob.b4': 'Long-press to quote text, drag a file to upload it',
    'mob.b5': 'Desktop system notifications and native Android notifications',
    'mob.link': 'See how to install',
    'mob.zoom': 'Enlarge the phone screenshot',
    'mob.cap': 'A whole turn on one screen: reasoning, tool calls, the answer and the composer',
    'mob.note': 'This is the same turn that started on the desktop above — the device changed, but the <code>Bash</code> output and reasoning log came across intact.',

    'desk.kicker': 'Desktop · macOS / Windows / Linux',
    'desk.title': 'At your desk,<br>it is the main window.',
    'desk.lede':
      'File tree, document, conversation and terminal can share one screen with draggable dividers. Notifications still arrive when the window is minimized — events travel over WebSocket, not through the page-level Notification API.',
    'desk.b1': 'Draggable multi-column layout, no more tab switching on wide screens',
    'desk.b2': 'CodeMirror editing with VS Code-style sticky scroll',
    'desk.b3': 'Frameless windows, native context menus, external links in your browser',
    'desk.b4': 'In-app self-upgrade: side-by-side installs with a pointer switch, rollback included',
    'desk.link': 'Download the desktop app',
    'desk.cap': 'Three columns side by side: files and documents, AI conversation, session list',
    'desk.zoom': 'Enlarge the desktop screenshot',
    'desk.zoomHint': 'Click to enlarge',
    'zoom.dialog': 'Screenshot preview',
    'zoom.close': 'Close',

    // 平台名与架构标注在两种语言下相同，故不带 data-i18n；这里只翻标题与说明。
    'dl.client': 'Clients',
    'dl.server': 'Server binaries',
    'dl.serverNote':
      'One file with the frontend built in — unzip and run <code>./clawbench</code>. The Android build is the arm64 variant for Termux.',
    'dl.foot':
      'Need an older version or the changelog? <a href="https://github.com/clawbench-dev/clawbench/releases" target="_blank" rel="noopener">Browse all releases</a>.',
    'dl.integrated': 'Integrated',
    'dl.integratedNote': 'Ships the server inside the client — install it and you are done, no separate deployment.',
    'dl.integratedDesktopOnly': 'The integrated package is for desktop. On a phone, use the split setup.',
    'dl.integratedMacNote': 'After unzipping, keep ClawBench.app and the server folder together.',
    'dl.splitNote': 'Client only — it connects to a server you install separately (the server package in step 1, or another machine).',
    'dl.recommended': 'Recommended',
    'dl.switchToSplit': 'On a phone or tablet? Switch to separate client and server',
    'dl.moreAndroid': 'Phone (Android)',

    'feat.kicker': 'Features',
    'feat.title': 'Not a chat box.<br>A whole workflow.',
    'feat.lede':
      'From reading a file to committing code, from scheduled jobs to public access — the things you used to type into a terminal all happen in one interface.',
    'feat.chat.t': 'AI conversation',
    'feat.chat.d':
      'Streaming output, visible tool calls, visible reasoning, interactive question cards, and mid-turn steering while a reply is still generating.',
    'feat.group.t': 'Multi-agent group chat',
    'feat.group.d':
      'Put several backends in one room: host mode takes turns, free mode hands off with @mentions. Auto-cruise keeps going until the task is confirmed done.',
    'feat.files.t': 'File manager',
    'feat.files.d':
      'Recursive browsing, gitignored entries dimmed, multi-select batch actions, drag-to-move, directory downloads and share links.',
    'feat.code.t': 'Code editing',
    'feat.code.d':
      'CodeMirror in browse and edit modes, syntax highlighting for 170+ languages — long-tail grammars load on demand, so the first paint never pays for them.',
    'feat.md.t': 'Markdown and diagrams',
    'feat.md.d':
      'LaTeX formulas, Mermaid diagrams, a table-of-contents drawer, inline code-link previews and self-contained HTML export.',
    'feat.office.t': 'Documents and media',
    'feat.office.d':
      'Native rendering for Word, Excel and PowerPoint, paged PDF zoom, and inline playback with lightbox for images, audio and video.',
    'feat.git.t': 'Git and Forge',
    'feat.git.d':
      'Commit history, branch graph, diff views and worktree management; GitHub / GitLab issues, merges and pipelines can be quoted straight into a conversation.',
    'feat.term.t': 'Web terminal',
    'feat.term.d':
      'PTY-backed multi-tab terminal with a three-mode gesture system, virtual modifier keys and 157 themes — vim works on a phone.',
    'feat.cron.t': 'Scheduled and event tasks',
    'feat.cron.d':
      'Run on a cron or on a Git event, with an optional pre-script: if it succeeds quietly, the AI turn is skipped and no notification is sent.',
    'feat.ssh.t': 'SSH and port forwarding',
    'feat.ssh.d':
      'Forward and reverse port mapping, transparent to every protocol (HTTP / WS / SSE / gRPC) — reach localhost at home from your phone.',
    'feat.notify.t': 'Notifications and push',
    'feat.notify.d':
      'A notification center collects session, task and repository events; DingTalk and Feishu bots plus a WeChat bot you link by scanning a QR code all let you read and reply from IM.',
    'feat.live.t': 'Live screen share and meetings',
    'feat.live.d':
      'Share a screen on the local network — one link, no login, opens in any browser. Let the audience unmute to talk, while AI notes transcribe live, keep speakers on separate channels, and turn into minutes when the share ends.',
    'feat.live.tag1': 'Screen share',
    'feat.live.tag2': 'Voice',
    'feat.live.tag3': 'AI notes',
    'feat.theme.t': 'Themes and wallpapers',
    'feat.theme.d':
      '36 named themes, system-following switch and a circular reveal transition; local gallery, daily Bing and animated wallpapers.',

    'dep.kicker': 'Deploy',
    'dep.title': 'Pick a setup,<br>follow the steps.',
    'dep.lede':
      'Choose integrated or split first — only the matching steps are shown.',
    'mode.t': 'How do you want to install it?',
    'mode.kicker': 'Choose first',
    'mode.d': 'Both setups are the same product; the difference is whether the server runs separately.',
    'mode.aria': 'Installation type',
    'mode.integrated.t': 'Integrated',
    'mode.integrated.tag': 'Recommended',
    'mode.integrated.d':
      'The server ships inside the client: install it, open it, done. Best for office and everyday use — no command line, no second machine.',
    'mode.integrated.fit': 'Windows / macOS / Linux · one installer does everything',
    'mode.integrated.s1': 'Install the client',
    'mode.split.s1': 'Start the server',
    'mode.split.s2': 'Connect a client',
    'mode.done': 'Done',
    'mode.bridge.integrated':
      'Based on your choice there is just <strong>one step</strong> below — install it and you have the whole thing.',
    'mode.bridge.split':
      '<strong>Two steps</strong> below: start the server on one machine, then connect a client from wherever you are.',
    'step.from.split': 'Split · step 1 of 2',
    'step.from.split2': 'Split · step 2 of 2',
    'step.from.integrated': 'Integrated · the only step',
    'mode.split.t': 'Client and server separately',
    'mode.split.d':
      'The server runs on one machine; desktop, phone and browser connect from elsewhere. Best for development and tinkering — long jobs keep running on the server while you pick them up from anywhere.',
    'mode.split.fit': 'Windows / macOS / Linux / Android · works across devices',
    'dep.tab.script': 'One-line',
    'dep.tab.binary': 'Binary',
    'dep.copy': 'Copy',

    'step.server.t': 'Start the server',
    'step.server.d': 'Pick any one way. The first run prints a random password to the console.',
    'step.server.note': 'The console prints the address and a random password once it is up — you need them in the next step.',
    'step.next': 'Next: connect a client',
    'step.back': '← Pick a different setup',
    'step.client.t': 'Connect a client',
    'step.client.d': 'Install the desktop or Android client, then enter the address and password from the previous step.',
    'step.integrated.t': 'Install the client',
    'step.integrated.d': 'Download the integrated package — it opens ready to use, the server is already inside.',
    'step.client.note': 'No client needed — open the same address in any browser (<code>http://your-server-ip:20000</code>) and you get the same interface.',
    'code.npm.mirror': '# Mirror first if you are in mainland China — much faster',
    'code.npm.cmt': '# No global install — runs the latest version',
    'dep.script.note':
      'The script installs the <strong>latest</strong> version, so it is not reproducible; pin it with <code>CLAWBENCH_VERSION=x.y.z</code> (on Windows, <code>$env:CLAWBENCH_VERSION</code>) — the actual installed version is printed at the end. To verify the script itself, use the sha256 published in the <a href="https://github.com/clawbench-dev/clawbench/releases">release notes</a>.',

    'gal.kicker': 'Screenshots',
    'gal.title': 'Screenshots beat adjectives.',
    'gal.lede': 'Everything below is a real screenshot, with no visual touch-ups.',
    'gal.chat': 'Chat · visible tool calls',
    'gal.office': 'Documents · native Office',
    'gal.agents': 'Agents · 25 backends',
    'gal.ppt': 'Documents · native slides',
    'gal.git': 'Git · branch graph and history',
    'gal.mermaid': 'Markdown · Mermaid diagrams',
    'gal.tasks': 'Tasks · cron and events',
    'gal.ssh': 'Tunnel · port forwarding',
    'gal.rag': 'Search · aggregated retrieval',
    'gal.term': 'Terminal · multi-tab PTY',
    'gal.hint': 'Swipe to see more',

    'faq.kicker': 'FAQ',
    'faq.title': 'What you probably want to ask first.',
    'faq.q1': 'What do I need before starting?',
    'faq.a1':
      'A machine that can run an AI coding agent CLI (Linux / macOS / Windows, or an Android phone with Termux), with at least one agent installed. Run ClawBench on that same machine and open its address in any browser.',
    'faq.q2': 'Can it really run entirely on a phone?',
    'faq.a2':
      'Yes. The backend is a pure-Go <code>linux-arm64</code> static binary with the frontend embedded. Install it through Termux npm and run it — the agents execute on the phone too, with no extra PC or server.',
    'faq.q3': 'Where does my data live? Is it uploaded anywhere?',
    'faq.a3':
      'Everything stays on your own machine: a SQLite database plus files in the data directory, with no third-party relay. Passwords are stored salted with SHA-256, and even local requests carry a short-lived HMAC token. For public access, use a tunnel or reverse proxy with TLS.',
    'faq.q4': 'Which AI agents are supported?',
    'faq.a4':
      '25 built-in backends: ClawBench Agent (built in), CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, Qoder CN, CodeWhale, DeepSeek Harness, MiMo-Code, Pi, Kimi, Antigravity, Grok Build, ZCode, Cursor, Hermes Agent, Command Code, OpenClaw, Qwen Code, Cline, GitHub Copilot, MiniMax Code, Kiro and TRAE. You can also add a <strong>custom ACP agent</strong> — any tool that speaks ACP over stdio works with one launch command.',
    'faq.q5': 'How do I reach my home machine from outside?',
    'faq.a5':
      'IPv6 direct connection, EasyTier decentralized networking (no VPS needed), SSH tunnels and an HTTP/2 stream tunnel. Reverse mappings bind only to <code>127.0.0.1</code> and reserved ports are rejected, so nothing extra gets exposed.',
    'faq.q6': 'Is it free?',
    'faq.a6':
      'ClawBench itself is MIT-licensed open source and free. Your own agents (and their subscriptions or API billing) are separate and billed by their respective providers.',

    'outro.title': 'Put the workbench<br>in your pocket.',
    'outro.sub': 'One command, one binary — the rest is handled by the agents you already use.',
    'outro.cta': 'Deploy now',
    'outro.cta2': 'View on GitHub',

    'foot.issues': 'Issues'
  };

  var ZH = {
    'nav.aria': '主导航',
    'nav.menu': '打开菜单',
    'skip': '跳到主要内容'
  };

  var STORAGE_KEY = 'clawbench-landing-locale';

  /* ── 画廊无缝循环：先复制一份卡片 ─────────────────────────────
     必须放在 i18n 采集之前：翻译是遍历当时 DOM 里带 data-i18n 的
     节点做的，克隆晚于采集就会留下一批永远不翻译的英文/中文残片。 */
  var gal = document.getElementById('gal');
  var galSetWidth = 0;

  if (gal) {
    var galItems = Array.prototype.slice.call(gal.children);
    if (galItems.length >= 2) {
      galItems.forEach(function (item) {
        var clone = item.cloneNode(true);
        // 克隆只用于视觉衔接，对读屏和搜索都是重复内容
        clone.setAttribute('aria-hidden', 'true');
        clone.classList.add('is-clone');
        Array.prototype.forEach.call(clone.querySelectorAll('img'), function (img) {
          img.alt = '';
          img.loading = 'lazy';
        });
        gal.appendChild(clone);
      });
      // 一份卡片的精确宽度：首个克隆与原首卡的 offsetLeft 之差。
      // 不能用 scrollWidth/2 —— 它把两侧 padding 也算进去了。
      galSetWidth = gal.children[galItems.length].offsetLeft - galItems[0].offsetLeft;
    }
  }

  /* ── 语言切换 ───────────────────────────────────────────────── */
  var nodes = Array.prototype.slice.call(document.querySelectorAll('[data-i18n]'));
  var htmlNodes = Array.prototype.slice.call(document.querySelectorAll('[data-i18n-html]'));
  var labelNodes = Array.prototype.slice.call(document.querySelectorAll('[data-i18n-label]'));
  var kickerNodes = Array.prototype.slice.call(document.querySelectorAll('[data-kicker-i18n]'));
  kickerNodes.forEach(function (n) { n.dataset.kickerZh = n.getAttribute('data-kicker') || ''; });
  var titleEl = document.title;
  var metaDesc = document.querySelector('meta[name="description"]');
  var ogTitle = document.querySelector('meta[property="og:title"]');
  var ogDesc = document.querySelector('meta[property="og:description"]');

  // 记录中文原文，切回中文时直接还原，不必再维护一份中文表。
  nodes.forEach(function (n) { n.dataset.zh = n.textContent; });

  // 向导在运行时把标题/描述换成另一组 key（整合包跳过「装服务端」），
  // 新 key 的中文原文得先登记，否则切回中文会留下一句英文。这里只登记
  // 当前 DOM 里没有的 key，之后由 apply() 按语种填。
  var SPARE_KEYS = {
    'step.integrated.t': '装客户端',
    'step.integrated.d': '下载整合包，装完打开就能用——服务端已经在里面了。',
    // 承接条和步骤归属标签跟着方案换 key，换过去的那组中文原文不在 HTML 里
    'mode.bridge.split':
      '下面<strong>两步</strong>：先在一台机器上把服务端跑起来，再从任意设备连上客户端。',
    'mode.bridge.integrated':
      '按上面的选择，下面只有 <strong>1 步</strong>：装好就是完整的一套。',
    'step.from.split2': '分开装 · 第 2 步，共 2 步',
    'step.from.integrated': '整合包 · 唯一一步'
  };
  var spareZh = {};
  (function () {
    var known = {};
    // htmlNodes 也得算：承接条这类带 <strong> 的元素走的是 data-i18n-html，
    // 漏掉它们就会把自己在 HTML 里的中文原文当成「不存在」，于是每次切回
    // 中文都被这里手抄的那份覆盖——两边内容一旦不同步，页面就静默显示旧文案。
    nodes.forEach(function (n) { known[n.dataset.i18n] = true; });
    htmlNodes.forEach(function (n) { known[n.dataset.i18nHtml] = true; });
    Object.keys(SPARE_KEYS).forEach(function (key) {
      if (!known[key]) spareZh[key] = SPARE_KEYS[key];
    });
  })();
  htmlNodes.forEach(function (n) { n.dataset.zh = n.innerHTML; });
  labelNodes.forEach(function (n) { n.dataset.zh = n.getAttribute('aria-label') || ''; });

  var META = {
    zh: {
      title: 'ClawBench — 从掌心到桌面，同一个 AI 工作台',
      desc: 'ClawBench 把 CodeBuddy、Claude Code、Codex 等 25 个 AI 编程智能体封装成一个 Web 工作台。服务端执行，客户端呈现，一个会话在手机、平板和桌面之间无缝继续。单文件部署，MIT 开源。',
      ogTitle: 'ClawBench — 从掌心到桌面，同一个 AI 工作台',
      ogDesc: '25 个 AI 编程智能体，一块屏幕就够。服务端执行，客户端呈现，单文件部署。'
    },
    en: {
      title: 'ClawBench — One AI workbench, from phone to desktop',
      desc: 'ClawBench wraps 25 AI coding agents into a single web workbench. Execution on your server, UI anywhere, one session across phone, tablet and desktop. Single binary, MIT licensed.',
      ogTitle: 'ClawBench — One AI workbench, from phone to desktop',
      ogDesc: '25 AI coding agents, one screen. Execution on your server, UI anywhere, single binary.'
    }
  };

  function apply(lang) {
    var dict = lang === 'en' ? EN : ZH;
    var isEn = lang === 'en';

    nodes.forEach(function (n) {
      var key = n.dataset.i18n;
      var val = isEn ? dict[key] : n.dataset.zh;
      if (!isEn && spareZh[key] != null) val = spareZh[key];
      if (val != null) n.textContent = val;
    });

    htmlNodes.forEach(function (n) {
      var key = n.dataset.i18nHtml;
      var val = isEn ? dict[key] : n.dataset.zh;
      // 换 key 后的中文原文不在 DOM 快照里（那个元素原本写的是另一句），
      // 只能从 spareZh 取——和上面的文本节点分支同一套规则。
      if (!isEn && spareZh[key] != null) val = spareZh[key];
      if (val != null) n.innerHTML = val;
    });

    labelNodes.forEach(function (n) {
      var key = n.dataset.i18nLabel;
      var val = isEn ? dict[key] : n.dataset.zh;
      if (val != null) n.setAttribute('aria-label', val);
    });

    // 「先选」这类引导小标签是 CSS 的 content: attr(data-kicker) 画出来的，
    // 文字不在文本节点里，得单独改属性。
    kickerNodes.forEach(function (n) {
      var key = n.dataset.kickerI18n;
      var val = isEn ? dict[key] : n.dataset.kickerZh;
      if (val != null) n.setAttribute('data-kicker', val);
    });

    // 架构图是中英两份独立 SVG，不能靠替换文字
    document.querySelectorAll('.arch-zh').forEach(function (el) { el.hidden = isEn; });
    document.querySelectorAll('.arch-en').forEach(function (el) { el.hidden = !isEn; });

    var m = META[lang] || META.zh;
    document.title = m.title;
    if (metaDesc) metaDesc.setAttribute('content', m.desc);
    if (ogTitle) ogTitle.setAttribute('content', m.ogTitle);
    if (ogDesc) ogDesc.setAttribute('content', m.ogDesc);

    document.documentElement.lang = isEn ? 'en' : 'zh-CN';
    document.documentElement.dataset.locale = isEn ? 'en' : 'zh';

    document.querySelectorAll('.lang-opt').forEach(function (el) {
      el.classList.toggle('is-active', el.dataset.lang === lang);
    });

    // 英文标题更长，字号略收一档，避免在窄屏挤出四行
    document.body.classList.toggle('lang-en', isEn);

    // 换镜像源只对国内中文用户有意义，英文页里那两行被 .zh-only 隐藏，
    // 复制的内容也要跟着少一行，否则粘贴出来仍是镜像命令。
    document.querySelectorAll('[data-copy-zh]').forEach(function (b) {
      b.dataset.copy = isEn ? b.dataset.copyEn : b.dataset.copyZh;
    });

    window.__clawbenchApplyLang = lang;
  }

  function setLang(lang, persist) {
    apply(lang);
    // 下载芯片在中文页走国内镜像、英文页走 github 直连，切语言要重算。
    // 定义在下方，用存在性判断避免顺序耦合。
    if (window.__clawbenchRenderReleaseLinks) window.__clawbenchRenderReleaseLinks();
    if (persist) {
      try { localStorage.setItem(STORAGE_KEY, lang); } catch (e) { /* 隐私模式下不可写 */ }
    }
  }

  function detectLang() {
    var saved = null;
    try { saved = localStorage.getItem(STORAGE_KEY); } catch (e) { /* ignore */ }
    if (saved === 'en' || saved === 'zh') return saved;
    var nav = (navigator.language || navigator.userLanguage || 'zh').toLowerCase();
    return nav.indexOf('zh') === 0 ? 'zh' : 'en';
  }

  var langToggle = document.getElementById('langToggle');
  if (langToggle) {
    langToggle.addEventListener('click', function () {
      var next = document.documentElement.dataset.locale === 'en' ? 'zh' : 'en';
      setLang(next, true);
    });
  }

  setLang(detectLang(), false);

  /* ── 安装脚本地块按当前域名生成 ───────────────────────────────
     两个域名（Cloudflare Pages 主站 + GitHub Pages 项目页）各托管一份
     install.sh / install.ps1。写死主站地址时，从另一个域名访问的人拿到
     的命令指向副本 A，而页面本身是副本 B——两者更新不同步就会装出不同
     版本，且用户完全看不出来。改成按 location.host 生成：拿到的永远是
     「他正在看的这份」。代码块与复制内容一起改，避免看到的和复制的不一致。 */
  (function () {
    if (!window.location || !window.location.host) return;
    var base = window.location.protocol + '//' + window.location.host;
    var block = document.querySelector('[data-panel="script"]');
    if (!block) return;
    var commands = [
      { code: 'curl -fsSL ' + base + '/install.sh | bash' },
      { code: 'irm ' + base + '/install.ps1 | iex' }
    ];
    var panels = block.querySelectorAll('.code-panel');
    for (var i = 0; i < panels.length && i < commands.length; i++) {
      var cmd = commands[i].code;
      var codeEl = panels[i].querySelector('pre code');
      var copyBtn = panels[i].querySelector('.copy-btn');
      if (codeEl) codeEl.textContent = cmd;
      if (copyBtn) copyBtn.dataset.copy = cmd;
    }
  })();

  /* ── 下载直链 ─────────────────────────────────────────────────
     资产名里带版本号（clawbench-desktop-linux-x64-v0.99.1.zip），所以
     没有 /releases/latest/download/<name> 这种稳定地址——只能先问一次
     最新 tag 再拼。HTML 里的 href 已经写成 Releases 页：取不到（离线、
     被限流、API 改版）就原样保留，下载永远可用，只是多点一次。

     中文页的芯片直接指向国内镜像：github.com 的 release 在国内常常只有
     几 KB/s。前缀与后端 desktop_upgrade.go / drawio.go 的 githubReleaseMirrors
     是同一组，按实测速度排序（gh-proxy.com ≈ 3.9MB/s，ghfast.top ≈ 1.2MB/s；
     ghproxy.net 虽通但只有 33KB/s，已从三处一并剔除）。
     镜像站是社区服务、随时可能挂，所以只有一面：挂到根路径下的 /releases
     页同样是镜像，退路不会变成 github 直连。 */
  var MIRROR = 'https://gh-proxy.com/';
  var RELEASES = 'https://github.com/clawbench-dev/clawbench/releases';
  var releaseLinks = Array.prototype.slice.call(document.querySelectorAll('a[data-asset]'));

  function assetUrl(tag, a) {
    var ext = a.getAttribute('data-ext') || 'zip';
    return RELEASES + '/download/' + encodeURIComponent(tag) + '/' +
      a.getAttribute('data-asset') + '-' + tag + '.' + ext;
  }

  function applyReleaseTag(tag) {
    if (!tag) return;
    currentTag = tag;
    renderReleaseLinks();
  }

  /* 版本号回来之前（以及取不到时）芯片停在 Releases 页。中文页这期间也
     走镜像，否则国内用户在那几秒里点到的仍是 github 直连。
     语言切换后要重算一次，否则切到英文页的芯片还带着镜像前缀。 */
  var currentTag = null;

  function wantsMirror() {
    return document.documentElement.dataset.locale !== 'en';
  }

  function renderReleaseLinks() {
    var mirror = wantsMirror();
    releaseLinks.forEach(function (a) {
      var target = currentTag ? assetUrl(currentTag, a) : RELEASES + '/latest';
      a.href = mirror && a.hasAttribute('data-mirror') ? MIRROR + target : target;
    });
  }
  renderReleaseLinks();
  window.__clawbenchRenderReleaseLinks = renderReleaseLinks;

  /* ── 部署向导：「其他平台」折叠 + 安装方式切换 + 当前平台高亮 ──────
     英文页也要折叠（收起只是少占地方，与语种无关），所以这段不做
     zh-only 判断。高亮只是加一层描边，不隐藏别的卡——按 UA 猜错系统
     的代价不能是「看不到自己该下的包」。

     向导的默认值按访客系统给：桌面系统（Windows / macOS / Linux）默认看到
     整合包（他要的东西就在这个包里），手机默认看到分开装（整合包是桌面
     版，给手机默认选一个点下去没得下的方案等于第一步就撞墙）。选定后用户
     可以改，改完记住——同一次访问里刷新页面不该又跳回去。 */
  var dlMore = document.getElementById('dlMore');
  var dlMoreBody = document.getElementById('dlMoreBody');
  if (dlMore && dlMoreBody) {
    dlMore.addEventListener('click', function () {
      var open = dlMore.getAttribute('aria-expanded') === 'true';
      dlMore.setAttribute('aria-expanded', open ? 'false' : 'true');
      dlMoreBody.hidden = open;
    });
  }

  function guessOS() {
    var ua = (navigator.userAgent || '').toLowerCase();
    var plat = (navigator.platform || '').toLowerCase();
    if (/android/.test(ua)) return 'android';
    if (/iphone|ipad|ipod/.test(ua)) return 'android';
    if (/win/.test(plat) || /windows/.test(ua)) return 'win';
    if (/mac/.test(plat) || /mac os x/.test(ua)) return 'mac';
    if (/linux|x11/.test(plat) || /linux/.test(ua)) return 'linux';
    return '';
  }

  /* 服务端面板里的卡片同理：它也有 Windows / macOS / Linux / Android 四张，
     同一个人在两处面对的是同一台机器。 */
  var os = guessOS();
  if (os) {
    document.querySelectorAll('.dl-card[data-os]').forEach(function (card) {
      card.classList.toggle('is-current', card.getAttribute('data-os') === os);
    });
  }

  var stepServer = document.getElementById('step-server');
  var stepClient = document.getElementById('step-client');
  var modeCards = Array.prototype.slice.call(document.querySelectorAll('.mode-card'));
  var splitPanel = document.querySelector('[data-mode-panel="split"]');
  var integratedPanel = document.querySelector('[data-mode-panel="integrated"]');
  // 面板不止一个：客户端的下载网格、以及只对分开装成立的说明文字。
  // 逐个按 currentMode 的归属显隐，新增一条提示不用再改这里。
  var modePanels = Array.prototype.slice.call(document.querySelectorAll('[data-mode-panel]'));
  var stepBack = document.getElementById('stepBack');
  var stepNext = document.getElementById('stepNext');
  var modeBridge = document.getElementById('modeBridge');
  var stepClientFrom = document.getElementById('stepClientFrom');
  var mode = '';

  /* 整合包覆盖三个桌面系统，但装不了手机。手机访客选了它，卡要留着（他能
     看到有这么个东西、也能改主意），但下载芯片灰掉并写明原因——藏起来会
     让他以为整合包不存在。 */
  function markIntegratedAvailability() {
    if (!integratedPanel) return;
    var blocked = os === 'android';
    integratedPanel.classList.toggle('is-unavailable', !!blocked);
    integratedPanel.querySelectorAll('.dl-card-warn, .dl-card-altswap').forEach(function (el) {
      el.hidden = !blocked;
    });
    integratedPanel.querySelectorAll('a[data-asset]').forEach(function (a) {
      if (blocked) {
        a.setAttribute('aria-disabled', 'true');
        a.setAttribute('tabindex', '-1');
      } else {
        a.removeAttribute('aria-disabled');
        a.removeAttribute('tabindex');
      }
    });
  }

  function applyMode(next, scroll) {
    mode = next;
    var chosen = next === 'integrated' || next === 'split';
    modeCards.forEach(function (card) {
      card.setAttribute('aria-checked', String(card.dataset.mode === next));
    });

    var split = next === 'split';
    // 整合包不分服务端/客户端，所以第二步直接是「下载」：跳过装服务端
    // 那一步，序号也跟着变，否则同一页上会出现两个「2」。
    if (stepServer) stepServer.hidden = !split;
    if (stepClient) stepClient.hidden = !chosen;
    if (stepClient && split) {
      var no = stepClient.querySelector('.step-no');
      if (no) no.textContent = '2';
    } else if (stepClient) {
      var no1 = stepClient.querySelector('.step-no');
      if (no1) no1.textContent = '1';
    }
    if (stepClient && chosen) {
      var title = stepClient.querySelector('.step-title');
      var desc = stepClient.querySelector('.step-desc');
      // 整合包没有服务端那一步，标题换成「装客户端」，否则用户以为还缺一步
      var tKey = split ? 'step.client.t' : 'step.integrated.t';
      var dKey = split ? 'step.client.d' : 'step.integrated.d';
      if (title.getAttribute('data-i18n') !== tKey) title.setAttribute('data-i18n', tKey);
      if (desc.getAttribute('data-i18n') !== dKey) desc.setAttribute('data-i18n', dKey);
    }
    // 只对分开装成立的说明：「用浏览器打开同一地址」在整合包下不成立
    // （服务端就在本机），留着会让人以为要去连另一台机器。
    modePanels.forEach(function (el) {
      el.hidden = el.getAttribute('data-mode-panel') !== next;
    });
    if (stepNext) stepNext.hidden = !split;
    if (stepBack) stepBack.hidden = !chosen;
    // 承接条和步骤归属标签都跟着方案换：选了哪个、接下来几步、现在是第几步，
    // 这三件事要在选项和步骤之间彼此对得上，而不是靠用户记住自己点过什么。
    if (modeBridge) {
      modeBridge.hidden = !chosen;
      var bridgeText = document.getElementById('modeBridgeText');
      var bKey = split ? 'mode.bridge.split' : 'mode.bridge.integrated';
      if (bridgeText && bridgeText.getAttribute('data-i18n-html') !== bKey) {
        bridgeText.setAttribute('data-i18n-html', bKey);
      }
    }
    if (stepClientFrom) {
      var fromKey = split ? 'step.from.split2' : 'step.from.integrated';
      if (stepClientFrom.getAttribute('data-i18n') !== fromKey) {
        stepClientFrom.setAttribute('data-i18n', fromKey);
      }
    }
    // 重译必须在上面所有 data-i18n / data-i18n-html 都改成新键之后再做。
    // 放在改键之前，新键会拿不到值，页面上停在上一个方案的文案。
    if (chosen && window.__clawbenchApplyLang) apply(window.__clawbenchApplyLang);
    markIntegratedAvailability();

    if (scroll) {
      // 选完直接把人带到结果上，不用自己去滚。目标是该方案的「第一个」步骤：
      // 分开装要从装服务端看起，整合包直接就是装客户端。滚到 stepClient 会把
      // 分开装的人直接送到第 2 步，跳过第 1 步，等于跳过了真正的起点。
      var firstStep = split ? stepServer : stepClient;
      if (firstStep && firstStep.scrollIntoView) {
        firstStep.scrollIntoView({ behavior: 'smooth', block: 'start' });
      }
    }
  }

  if (modeCards.length && stepClient) {
    modeCards.forEach(function (card) {
      card.addEventListener('click', function () { applyMode(card.dataset.mode, true); });
    });
    if (stepBack) {
      stepBack.addEventListener('click', function () {
        applyMode('', false);
        var first = document.getElementById('step-mode');
        if (first && first.scrollIntoView) first.scrollIntoView({ behavior: 'smooth', block: 'start' });
      });
    }
    integratedPanel &&
      integratedPanel.querySelectorAll('[data-switch-to]').forEach(function (btn) {
        btn.addEventListener('click', function () { applyMode(btn.dataset.switchTo, false); });
      });

    // 默认先不选：让人看到两个选项，再自己点。但首次进入直接按系统
    // 需要的那条选中——「先看到选项」和「少点一次」可以同时满足：
    // 选中态本身就是两个选项并排呈现。
    applyMode(os === 'android' ? 'split' : 'integrated', false);
  }

  if (releaseLinks.length && typeof fetch === 'function') {
    fetch('https://api.github.com/repos/clawbench-dev/clawbench/releases/latest', {
      headers: { Accept: 'application/vnd.github+json' }
    })
      .then(function (res) { return res.ok ? res.json() : null; })
      .then(function (data) { applyReleaseTag(data && data.tag_name); })
      .catch(function () { /* 保留 Releases 页链接 */ });
  }

  /* ── 导航：滚动后加背景 ─────────────────────────────────────── */
  var nav = document.getElementById('nav');
  var lastStuck = null;

  function onScroll() {
    var stuck = window.scrollY > 12;
    if (stuck !== lastStuck) {
      lastStuck = stuck;
      nav.classList.toggle('is-stuck', stuck);
    }
  }

  window.addEventListener('scroll', onScroll, { passive: true });
  onScroll();

  /* ── 移动端菜单 ─────────────────────────────────────────────── */
  var burger = document.getElementById('burger');
  var menu = document.getElementById('mobileMenu');
  var menuOpen = false;

  function setMenu(open) {
    menuOpen = open;
    burger.setAttribute('aria-expanded', String(open));
    if (open) {
      menu.hidden = false;
      // 先显示再加类，否则 transition 不会触发
      requestAnimationFrame(function () { menu.classList.add('is-open'); });
      document.body.style.overflow = 'hidden';
    } else {
      menu.classList.remove('is-open');
      document.body.style.overflow = '';
      window.setTimeout(function () {
        if (!menuOpen) menu.hidden = true;
      }, 280);
    }
  }

  if (burger && menu) {
    burger.addEventListener('click', function () { setMenu(!menuOpen); });
    menu.addEventListener('click', function (e) {
      if (e.target.closest('a')) setMenu(false);
    });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && menuOpen) setMenu(false);
    });
    // 竖屏转横屏时菜单可能被挤在桌面布局里，宽屏直接关掉
    window.addEventListener('resize', function () {
      if (menuOpen && window.innerWidth >= 900) setMenu(false);
    });
  }

  /* ── 部署方式页签 ───────────────────────────────────────────── */
  var tabs = Array.prototype.slice.call(document.querySelectorAll('.tab'));

  tabs.forEach(function (tab) {
    tab.addEventListener('click', function () {
      var name = tab.dataset.tab;
      tabs.forEach(function (t) {
        var on = t === tab;
        t.classList.toggle('is-active', on);
        t.setAttribute('aria-selected', String(on));
      });
      document.querySelectorAll('[data-panel]').forEach(function (p) {
        var on = p.dataset.panel === name;
        p.classList.toggle('is-active', on);
        p.hidden = !on;
      });
    });
  });

  /* ── 复制命令 ───────────────────────────────────────────────── */
  var toast = document.getElementById('toast');
  var toastTimer = 0;

  function showToast(text) {
    if (!toast) return;
    toast.textContent = text;
    toast.classList.add('is-on');
    window.clearTimeout(toastTimer);
    toastTimer = window.setTimeout(function () {
      toast.classList.remove('is-on');
    }, 1800);
  }

  function fallbackCopy(text) {
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.top = '-1000px';
    document.body.appendChild(ta);
    ta.select();
    var ok = false;
    try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
    document.body.removeChild(ta);
    return ok;
  }

  document.querySelectorAll('.copy-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var text = btn.dataset.copy || '';
      var label = btn.querySelector('span');
      var original = label ? label.textContent : '';
      var done = function (ok) {
        var msg = ok
          ? (document.documentElement.dataset.locale === 'en' ? 'Copied' : '已复制')
          : (document.documentElement.dataset.locale === 'en' ? 'Copy failed' : '复制失败');
        showToast(msg);
        if (ok && label) {
          label.textContent = document.documentElement.dataset.locale === 'en' ? 'Copied' : '已复制';
          btn.classList.add('is-done');
          window.setTimeout(function () {
            label.textContent = original;
            btn.classList.remove('is-done');
          }, 1600);
        }
      };

      // navigator.clipboard 在非安全上下文（http + 局域网 IP）下是 undefined，
      // 而部署页最可能就在这种环境里被打开 —— 必须留 execCommand 兜底。
      if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(text).then(function () { done(true); }, function () {
          done(fallbackCopy(text));
        });
      } else {
        done(fallbackCopy(text));
      }
    });
  });

  /* ── 截图放大 ───────────────────────────────────────────────── */
  var lightbox = document.getElementById('lightbox');
  var lightboxImg = document.getElementById('lightboxImg');
  var lastFocused = null;

  function openLightbox(src, alt) {
    if (!lightbox || !lightboxImg) return;
    lastFocused = document.activeElement;
    lightboxImg.src = src;
    lightboxImg.alt = alt || '';
    lightbox.hidden = false;
    document.body.style.overflow = 'hidden';
    var close = lightbox.querySelector('.lightbox-close');
    if (close) close.focus();
  }

  function closeLightbox() {
    if (!lightbox || lightbox.hidden) return;
    lightbox.hidden = true;
    lightboxImg.removeAttribute('src');
    document.body.style.overflow = '';
    if (lastFocused && typeof lastFocused.focus === 'function') lastFocused.focus();
    lastFocused = null;
  }

  document.querySelectorAll('[data-zoom]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var img = btn.querySelector('img');
      openLightbox(btn.dataset.zoom, img ? img.alt : '');
    });
  });

  if (lightbox) {
    lightbox.addEventListener('click', function (e) {
      // 点背景关闭，点图片本身不关（否则想细看时一点就没了）
      if (e.target === lightbox || e.target.closest('.lightbox-close')) closeLightbox();
    });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') closeLightbox();
    });
  }

  /* ── 滚动进场 ───────────────────────────────────────────────── */
  var reveals = Array.prototype.slice.call(document.querySelectorAll('.reveal'));
  var noMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  if (noMotion || !('IntersectionObserver' in window)) {
    reveals.forEach(function (el) { el.classList.add('is-in'); });
  } else {
    var ro = new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        if (!en.isIntersecting) return;
        en.target.classList.add('is-in');
        ro.unobserve(en.target);
      });
    }, { rootMargin: '0px 0px -8% 0px', threshold: 0.08 });
    reveals.forEach(function (el) { ro.observe(el); });
  }

  /* ── 画廊：自动滚动 + 滚轮横滚 ─────────────────────────────── */
  if (gal) {
    // 桌面用鼠标滚轮时，横向滚动条本身不响应纵向滚轮；把纵向滚轮转成横向。
    // 到头就放行，否则会把整页滚动吞掉。
    gal.addEventListener('wheel', function (e) {
      if (Math.abs(e.deltaX) > Math.abs(e.deltaY)) return;
      var atStart = gal.scrollLeft <= 0;
      var atEnd = gal.scrollLeft + gal.clientWidth >= gal.scrollWidth - 1;
      if ((e.deltaY < 0 && atStart) || (e.deltaY > 0 && atEnd)) return;
      e.preventDefault();
      gal.scrollLeft += e.deltaY;
    }, { passive: false });

    /* 自动滚动。
       三件事同时满足才算对：
       ① 无缝：滚到克隆区的同一位置时把 scrollLeft 减去一份的宽度，
          肉眼看不到跳变；
       ② 悬停/触摸/聚焦即停，离开后接着走 —— 用户想看清某张时不能被
          它自己溜走；
       ③ 页面不可见、区块滚出视口、或系统开了减少动效时不跑，省电也
          避免"切回来发现已经跑过头"。 */
    var galSpeed = 0.55;            // px/帧（约 33px/s @60fps）
    var galPaused = false;
    var galRaf = 0;
    var galLast = 0;
    var galInView = true;
    var galReduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    function galStep(now) {
      galRaf = window.requestAnimationFrame(galStep);
      if (!galLast) { galLast = now; return; }
      var dt = Math.min(now - galLast, 64);   // 切标签页回来时 dt 会很大，钳住
      galLast = now;
      if (galPaused || !galInView || galSetWidth <= 0) return;

      gal.scrollLeft += galSpeed * (dt / 16.67);
      if (gal.scrollLeft >= galSetWidth) gal.scrollLeft -= galSetWidth;
    }

    function galStart() {
      if (galReduced || galRaf) return;
      galLast = 0;
      galRaf = window.requestAnimationFrame(galStep);
    }

    function galStop() {
      if (galRaf) window.cancelAnimationFrame(galRaf);
      galRaf = 0;
    }

    // 暂停时才允许 snap 生效：手动拖动是一格一格的手感，
    // 自动滚动期间则必须关掉，否则每帧都会被吸回去。
    function galPause() {
      galPaused = true;
      gal.classList.add('is-paused');
    }
    function galResume() {
      galPaused = false;
      galLast = 0;
      gal.classList.remove('is-paused');
    }

    // 暂停的判定放在「单张卡片」上，而不是整个画廊。
    // 画廊在宽屏下占 1344×539，几乎铺满视口 —— 绑在容器上意味着
    // 只要鼠标在页面中间就永远暂停，看起来就是"跑马灯没跑起来"。
    // 挂在卡片上，指针落在卡片之间的空隙里仍然继续滚。
    // pointermove 比 over/out 稳：out 在移到子元素时也会触发，
    // relatedTarget 为 null（移出窗口）时又会被误判成"离开卡片"。
    // 每次移动直接看指针当前压在哪张卡上，状态只由事实决定。
    gal.addEventListener('pointermove', function (e) {
      var onCard = !!(e.target.closest && e.target.closest('.gal-item'));
      if (onCard) galPause(); else galResume();
    });
    gal.addEventListener('pointerleave', galResume);
    // 触摸设备：手指按在画廊上时停（此时没有 pointerover 的 hover 语义）
    gal.addEventListener('touchstart', galPause, { passive: true });
    gal.addEventListener('touchend', galResume);
    gal.addEventListener('touchcancel', galResume);
    // 键盘用户 tab 到卡片里时也要停，否则焦点会跟着卡片漂走
    gal.addEventListener('focusin', galPause);
    gal.addEventListener('focusout', galResume);

    if ('IntersectionObserver' in window) {
      new IntersectionObserver(function (entries) {
        galInView = entries[0].isIntersecting;
      }, { threshold: 0.12 }).observe(gal);
    }

    document.addEventListener('visibilitychange', function () {
      if (document.hidden) galStop(); else galStart();
    });

    // 减少动效的偏好可能在页面打开期间被改
    var motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)');
    var onMotionChange = function (e) {
      galReduced = e.matches;
      if (galReduced) galStop(); else galStart();
    };
    if (motionQuery.addEventListener) motionQuery.addEventListener('change', onMotionChange);
    else if (motionQuery.addListener) motionQuery.addListener(onMotionChange);

    // 卡片宽度随视口与根字号变化（--pad、字号断点都会变），
    // 宽度失效后循环点会算错，表现为每隔一圈抖一下。
    var galResizeTimer = 0;
    window.addEventListener('resize', function () {
      window.clearTimeout(galResizeTimer);
      galResizeTimer = window.setTimeout(function () {
        if (!gal || gal.children.length < 2) return;
        var first = gal.children[0];
        var half = gal.children.length / 2;
        galSetWidth = gal.children[half].offsetLeft - first.offsetLeft;
        // 已经越过新的一圈时先归位，避免下一次回绕跳很远
        if (galSetWidth > 0 && gal.scrollLeft >= galSetWidth) {
          gal.scrollLeft -= galSetWidth;
        }
      }, 150);
    });

    galStart();
  }
})();
