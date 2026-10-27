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
      'ClawBench wraps <strong>19</strong> AI coding agents — CodeBuddy, Claude Code, Codex and more — into a single web workbench. Execution stays on your server, the UI runs anywhere, and <strong>one session</strong> follows you across phone, tablet and desktop.',
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
      'The Electron shell reuses the same UI and adds native notifications, context menus, SSH port forwarding and in-app self-upgrade.',
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
      'A notification center collects session, task and repository events; DingTalk and Feishu bots let you read and reply from IM.',
    'feat.theme.t': 'Themes and wallpapers',
    'feat.theme.d':
      '36 named themes, system-following switch and a circular reveal transition; local gallery, daily Bing and animated wallpapers.',

    'dep.kicker': 'Deploy',
    'dep.title': 'One command.<br>Sixty seconds.',
    'dep.lede':
      'No Node runtime, no separate database service. The first launch prints a randomly generated password to the console.',
    'dep.tab.binary': 'Binary',
    'dep.copy': 'Copy',
    'code.npm.cmt': '# Mirror first if you are in mainland China — much faster',
    'code.bin.cmt': '# Grab the archive for your platform from Releases and unzip it',
    'dep.note':
      'Once it is running, open <code>http://your-server-ip:20000</code> from the app or any browser.',

    'gal.kicker': 'Screenshots',
    'gal.title': 'Screenshots beat adjectives.',
    'gal.lede': 'Everything below is a real screenshot, with no visual touch-ups.',
    'gal.chat': 'Chat · visible tool calls',
    'gal.office': 'Documents · native Office',
    'gal.agents': 'Agents · 19 backends',
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
      '19 built-in backends: CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, VeCLI, CodeWhale, DeepSeek Harness, MiMo-Code, Pi, Copilot, Kimi, Antigravity, Grok Build, ZCode, Cursor, Hermes Agent, Command Code and OpenClaw. You can also add a <strong>custom ACP agent</strong> — any tool that speaks ACP over stdio works with one launch command.',
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
  var titleEl = document.title;
  var metaDesc = document.querySelector('meta[name="description"]');
  var ogTitle = document.querySelector('meta[property="og:title"]');
  var ogDesc = document.querySelector('meta[property="og:description"]');

  // 记录中文原文，切回中文时直接还原，不必再维护一份中文表。
  nodes.forEach(function (n) { n.dataset.zh = n.textContent; });
  htmlNodes.forEach(function (n) { n.dataset.zh = n.innerHTML; });
  labelNodes.forEach(function (n) { n.dataset.zh = n.getAttribute('aria-label') || ''; });

  var META = {
    zh: {
      title: 'ClawBench — 从掌心到桌面，同一个 AI 工作台',
      desc: 'ClawBench 把 CodeBuddy、Claude Code、Codex 等 19 个 AI 编程智能体封装成一个 Web 工作台。服务端执行，客户端呈现，一个会话在手机、平板和桌面之间无缝继续。单文件部署，MIT 开源。',
      ogTitle: 'ClawBench — 从掌心到桌面，同一个 AI 工作台',
      ogDesc: '19 个 AI 编程智能体，一块屏幕就够。服务端执行，客户端呈现，单文件部署。'
    },
    en: {
      title: 'ClawBench — One AI workbench, from phone to desktop',
      desc: 'ClawBench wraps 19 AI coding agents into a single web workbench. Execution on your server, UI anywhere, one session across phone, tablet and desktop. Single binary, MIT licensed.',
      ogTitle: 'ClawBench — One AI workbench, from phone to desktop',
      ogDesc: '19 AI coding agents, one screen. Execution on your server, UI anywhere, single binary.'
    }
  };

  function apply(lang) {
    var dict = lang === 'en' ? EN : ZH;
    var isEn = lang === 'en';

    nodes.forEach(function (n) {
      var key = n.dataset.i18n;
      var val = isEn ? dict[key] : n.dataset.zh;
      if (val != null) n.textContent = val;
    });

    htmlNodes.forEach(function (n) {
      var key = n.dataset.i18nHtml;
      var val = isEn ? dict[key] : n.dataset.zh;
      if (val != null) n.innerHTML = val;
    });

    labelNodes.forEach(function (n) {
      var key = n.dataset.i18nLabel;
      var val = isEn ? dict[key] : n.dataset.zh;
      if (val != null) n.setAttribute('aria-label', val);
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
  }

  function setLang(lang, persist) {
    apply(lang);
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
      document.querySelectorAll('.code-panel').forEach(function (p) {
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

    // pointerenter 覆盖鼠标与触控笔；touchstart 单独兜住触摸设备
    gal.addEventListener('pointerenter', galPause);
    gal.addEventListener('pointerleave', galResume);
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
