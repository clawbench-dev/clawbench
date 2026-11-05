#!/usr/bin/env node
/**
 * ClawBench 用户手册静态站点生成器。
 *
 * 输入：docs/user-guide/*.md（同一份源文件，应用内 /cb-user-guide 也在用）
 * 输出：一套与项目主页（web/landing/）同风格的静态页面，可直接挂到任意
 *       静态托管上，也可以被 CI 调用：
 *
 *     node web/docs/build.mjs --out dist/docs
 *
 * 设计取舍：
 *   - 样式不复制：页面直接引用 ../css/landing.css，只额外加载 docs.css。
 *     落地页改配色/字体/导航，手册跟着变，不会漂移。
 *   - 渲染器用 marked + highlight.js——与前端应用内渲染 Markdown 用的是
 *     同一套，手册在站上和在应用里长得一样。
 *   - 链接全部相对路径，站点放在根目录或子目录（GitHub Pages 的
 *     /clawbench/）都能用。
 */
import { readFile, readdir, writeFile, mkdir, copyFile, rm, stat } from 'node:fs/promises'
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { Marked } from 'marked'
import hljs from 'highlight.js/lib/common'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(HERE, '../..')

/** 默认输入/输出。CI 用 --out 指到暂存目录，本地默认写进落地页目录便于预览。 */
export const DEFAULT_SRC = path.join(ROOT, 'docs/user-guide')
export const DEFAULT_OUT = path.join(ROOT, 'web/landing/docs')

const REPO_URL = 'https://github.com/clawbench-dev/clawbench'
/** 站点不收录的仓库文档（README、CONTRIBUTING 等）指回 GitHub。 */
const GITHUB_BLOB = `${REPO_URL}/blob/main/`
/** 手册首页在站点里的文件名。 */
const INDEX_PAGE = 'index.html'

// ── 小工具 ──────────────────────────────────────────────────────

const ENTITIES = { amp: '&', lt: '<', gt: '>', quot: '"', '#39': "'", '#x27': "'", nbsp: ' ' }

function decodeEntities(s) {
  return s.replace(/&(amp|lt|gt|quot|#39|#x27|nbsp);/g, (_, k) => ENTITIES[k])
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c])
}

/** 把 HTML 片段还原成纯文本，用于标题 id 与 meta description。 */
function toPlainText(html) {
  return decodeEntities(html.replace(/<[^>]*>/g, '')).replace(/\s+/g, ' ').trim()
}

/**
 * 与 GitHub 一致的标题锚点：
 *   去掉标点（中英文都去）、转小写、空格换 `-`、重名追加 -1/-2。
 * 手册里的 `settings.md#用量`、`#54-用户称谓` 都依赖这个规则，
 * 改这里等于改所有已发布的锚点，测试会钉住几个真实样例。
 */
export function slugify(text) {
  return text
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s_-]/gu, '')
    .trim()
    .replace(/\s/g, '-')
}

function createSlugger() {
  const seen = new Map()
  return (text) => {
    const base = slugify(text) || 'section'
    const count = seen.get(base) || 0
    seen.set(base, count + 1)
    return count === 0 ? base : `${base}-${count}`
  }
}

/** `README.md` → index.html，其余按小写文件名成页。 */
export function pageSlug(fileName) {
  const base = path.posix.basename(fileName).replace(/\.md$/i, '')
  if (base.toLowerCase() === 'readme') return INDEX_PAGE
  return `${base.toLowerCase()}.html`
}

// ── 链接与图片重写 ──────────────────────────────────────────────

const EXTERNAL_RE = /^(?:[a-z][a-z0-9+.-]*:|\/\/)/i

/**
 * 站内链接重写：`.md` → `.html`（保留 #锚点）。
 * 仓库里但不在手册目录的文档（../../README.md）站点没有副本，指回 GitHub。
 */
export function rewriteHref(href) {
  if (!href) return href
  if (EXTERNAL_RE.test(href)) return href
  if (href.startsWith('#')) return href

  const hashAt = href.indexOf('#')
  const target = hashAt === -1 ? href : href.slice(0, hashAt)
  const hash = hashAt === -1 ? '' : href.slice(hashAt)

  if (target.startsWith('../')) {
    const repoPath = path.posix.normalize(path.posix.join('docs/user-guide', target))
    return `${GITHUB_BLOB}${repoPath}${hash}`
  }
  if (/\.md$/i.test(target)) {
    return pageSlug(target) + hash
  }
  return href
}

// ── Markdown 渲染 ──────────────────────────────────────────────

function renderCode({ text, lang }) {
  const language = (lang || '').trim().split(/\s+/)[0]
  if (language === 'mermaid') {
    // 交给页面里的 mermaid 渲染；脚本没加载成功时这里就是一段可读的源码。
    return `<div class="mermaid">${escapeHtml(text)}</div>`
  }
  if (language && hljs.getLanguage(language)) {
    const body = hljs.highlight(text, { language, ignoreIllegals: true }).value
    return `<pre class="code-block"><code class="hljs language-${escapeHtml(language)}">${body}</code></pre>`
  }
  return `<pre class="code-block"><code class="hljs">${escapeHtml(text)}</code></pre>`
}

/**
 * 渲染一篇文档。
 * @returns {{ html: string, toc: Array<{depth:number,id:string,text:string}> }}
 */
export function renderMarkdown(source, { collectImages } = {}) {
  const slug = createSlugger()
  const toc = []

  const marked = new Marked({ gfm: true, breaks: false })
  marked.use({
    renderer: {
      heading({ tokens, depth }) {
        const inner = this.parser.parseInline(tokens)
        const id = slug(toPlainText(inner))
        if (depth >= 2 && depth <= 3) toc.push({ depth, id, text: toPlainText(inner) })
        const anchor = `<a class="heading-anchor" href="#${escapeHtml(id)}" aria-hidden="true" tabindex="-1">#</a>`
        return `<h${depth} id="${escapeHtml(id)}">${inner}${anchor}</h${depth}>\n`
      },
      link({ href, title, tokens }) {
        const text = this.parser.parseInline(tokens)
        const target = rewriteHref(href)
        const attrs = [`href="${escapeHtml(target)}"`]
        if (title) attrs.push(`title="${escapeHtml(title)}"`)
        if (EXTERNAL_RE.test(target)) attrs.push('target="_blank"', 'rel="noopener"')
        return `<a ${attrs.join(' ')}>${text}</a>`
      },
      image({ href, title, text }) {
        if (collectImages && href && !EXTERNAL_RE.test(href)) collectImages.add(href)
        const attrs = [
          `src="${escapeHtml(href)}"`,
          `alt="${escapeHtml(text || '')}"`,
          'loading="lazy"',
          'decoding="async"',
        ]
        if (title) attrs.push(`title="${escapeHtml(title)}"`)
        return `<img ${attrs.join(' ')}>`
      },
      code: renderCode,
    },
  })

  // 表格统一套一层滚动容器：手册里不少表格比手机屏宽。
  const html = marked.parse(source).replace(/<table>/g, '<div class="table-wrap"><table>').replace(/<\/table>/g, '</table></div>')

  return { html, toc }
}

// ── 目录（侧栏）────────────────────────────────────────────────

function walkLinks(node, out) {
  if (!node || typeof node !== 'object') return
  if (node.type === 'link' && typeof node.href === 'string') out.push(node.href)
  for (const key of Object.keys(node)) {
    if (key === 'raw' || key === 'text' || key === 'href') continue
    const value = node[key]
    if (Array.isArray(value)) value.forEach((item) => walkLinks(item, out))
    else if (value && typeof value === 'object') walkLinks(value, out)
  }
}

/**
 * 侧栏结构直接取自手册索引 README.md：它本来就是按「部分 → 章节」写的，
 * 另立一份清单迟早和正文对不上。
 * 每篇文档只出现一次，归到它第一次被提到的那个部分。
 */
export function buildOutline(readmeSource, docs) {
  const tokens = new Marked({ gfm: true }).lexer(readmeSource)
  const groups = []
  let current = { title: '开始', items: [] }
  const placed = new Set()

  const flush = () => {
    if (current.items.length) groups.push(current)
  }

  for (const token of tokens) {
    if (token.type === 'heading' && token.depth === 1) {
      const title = toPlainText(token.text || '')
      // 第一个 H1 是手册自身的标题，不是「部分」
      if (title && title !== 'ClawBench 使用手册') {
        flush()
        current = { title: title.replace(/^第[一二三四五六七八九十]+部分[：:]\s*/, ''), items: [] }
      }
    }
    const links = []
    walkLinks(token, links)
    for (const href of links) {
      if (!/\.md$/i.test(href.split('#')[0])) continue
      if (href.startsWith('../')) continue
      const page = pageSlug(href.split('#')[0])
      if (placed.has(page)) continue
      const doc = docs.find((d) => d.page === page)
      if (!doc) continue
      placed.add(page)
      current.items.push({ page, title: doc.title })
    }
  }
  flush()

  const rest = docs.filter((d) => d.page !== INDEX_PAGE && !placed.has(d.page))
  if (rest.length) groups.push({ title: '其他', items: rest.map((d) => ({ page: d.page, title: d.title })) })

  return groups
}

// ── 页面骨架 ───────────────────────────────────────────────────

function navHtml() {
  return `<header class="nav is-stuck">
  <div class="nav-inner">
    <a class="brand" href="../index.html" aria-label="ClawBench 主页">
      <img class="brand-logo" src="../img/logo.webp" alt="" width="34" height="34">
      <span class="brand-name">ClawBench</span>
      <span class="brand-tag">手册</span>
    </a>
    <div class="nav-actions">
      <a class="docs-top-link" href="../index.html">主页</a>
      <a class="icon-btn" href="${REPO_URL}" target="_blank" rel="noopener" aria-label="GitHub">
        <svg viewBox="0 0 16 16" width="18" height="18" fill="currentColor" aria-hidden="true"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
      </a>
      <button class="docs-menu-btn" id="docsMenuBtn" type="button" aria-expanded="false" aria-controls="docsSide">
        <svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true"><path d="M2.5 4h11M2.5 8h11M2.5 12h11"/></svg>
        <span>目录</span>
      </button>
    </div>
  </div>
</header>`
}

function sidebarHtml(groups, currentPage) {
  const groupsHtml = groups
    .map((group) => {
      const items = group.items
        .map((item) => {
          const active = item.page === currentPage ? ' class="is-active" aria-current="page"' : ''
          return `<li><a href="${item.page}"${active}>${escapeHtml(item.title)}</a></li>`
        })
        .join('\n')
      return `<div class="docs-group">
      <p class="docs-group-title">${escapeHtml(group.title)}</p>
      <ul>${items}</ul>
    </div>`
    })
    .join('\n')

  return `<aside class="docs-side" id="docsSide">
    <div class="docs-filter">
      <input id="docsFilter" type="search" placeholder="筛选章节（按 / 聚焦）" aria-label="筛选章节" autocomplete="off">
    </div>
    <nav class="docs-nav" aria-label="手册目录">
      <div class="docs-group docs-group-home">
        <ul><li><a href="${INDEX_PAGE}"${currentPage === INDEX_PAGE ? ' class="is-active" aria-current="page"' : ''}>手册首页</a></li></ul>
      </div>
      ${groupsHtml}
    </nav>
  </aside>`
}

function tocHtml(toc) {
  if (!toc.length) return '<aside class="docs-toc" aria-label="本页目录"></aside>'
  const items = toc
    .map((h) => `<li class="toc-h${h.depth}"><a href="#${escapeHtml(h.id)}">${escapeHtml(h.text)}</a></li>`)
    .join('\n')
  return `<aside class="docs-toc" aria-label="本页目录">
    <p class="docs-toc-title">本页目录</p>
    <ul>${items}</ul>
  </aside>`
}

function pagerHtml(flat, currentPage) {
  const at = flat.findIndex((d) => d.page === currentPage)
  if (at === -1) return ''
  const prev = flat[at - 1]
  const next = flat[at + 1]
  const cell = (doc, dir) =>
    doc
      ? `<a class="pager-link pager-${dir}" href="${doc.page}">
        <span class="pager-dir">${dir === 'prev' ? '上一篇' : '下一篇'}</span>
        <strong>${escapeHtml(doc.title)}</strong>
      </a>`
      : '<span></span>'
  return `<nav class="docs-pager" aria-label="翻页">${cell(prev, 'prev')}${cell(next, 'next')}</nav>`
}

function pageHtml({ title, description, body, groups, toc, flat, page, withMermaid }) {
  const fullTitle = page === INDEX_PAGE ? `${title} · ClawBench` : `${title} — ClawBench 使用手册`
  // 手册在两个域名下都有副本（Cloudflare Pages 与 GitHub Pages），canonical
  // 固定指主域，否则同一份内容在两个域名下互相稀释信号。
  const canonical = `https://clawbench.pages.dev/docs/${page}`
  return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover">
<title>${escapeHtml(fullTitle)}</title>
<meta name="description" content="${escapeHtml(description)}">
<meta name="theme-color" content="#0a0806">
<link rel="canonical" href="${canonical}">
<link rel="icon" type="image/png" sizes="32x32" href="../img/favicon.png">
<link rel="stylesheet" href="../css/landing.css">
<link rel="stylesheet" href="docs.css">
</head>
<body class="docs-page">

<div class="ambient" aria-hidden="true">
  <div class="ambient-glow"></div>
  <div class="ambient-grid"></div>
  <div class="ambient-grain"></div>
</div>

<a class="skip-link" href="#main">跳到正文</a>

${navHtml()}

<div class="docs-layout">
  <div class="docs-scrim" id="docsScrim" hidden></div>
  ${sidebarHtml(groups, page)}
  <main class="docs-main" id="main">
    <nav class="docs-crumb" aria-label="位置">
      <a href="${INDEX_PAGE}">使用手册</a>
      ${page === INDEX_PAGE ? '' : `<span aria-hidden="true">/</span><span>${escapeHtml(title)}</span>`}
    </nav>
    <article class="prose">
${body}
    </article>
    ${pagerHtml(flat, page)}
  </main>
  ${tocHtml(toc)}
</div>

<footer class="footer">
  <div class="footer-inner">
    <div class="footer-brand">
      <img src="../img/logo.webp" alt="" width="26" height="26" loading="lazy">
      <span>ClawBench</span>
      <span class="footer-year">· 使用手册</span>
    </div>
    <nav class="footer-links" aria-label="页脚导航">
      <a href="../index.html">项目主页</a>
      <a href="${GITHUB_BLOB}README.md" target="_blank" rel="noopener">README</a>
      <a href="${REPO_URL}/issues" target="_blank" rel="noopener">反馈</a>
    </nav>
  </div>
</footer>

<div class="lightbox" id="lightbox" hidden role="dialog" aria-modal="true" aria-label="图片预览">
  <button class="lightbox-close" type="button" aria-label="关闭">
    <svg viewBox="0 0 16 16" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8"/></svg>
  </button>
  <img id="lightboxImg" alt="">
</div>

${withMermaid ? '<script src="mermaid.min.js" defer></script>' : ''}
<script src="docs.js" defer></script>
</body>
</html>
`
}

// ── 构建 ───────────────────────────────────────────────────────

/** 去掉行内标记，只留文字（用于标题与 meta description）。 */
function stripInline(text) {
  return String(text || '')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/[`*_~]/g, '')
    .replace(/\s+/g, ' ')
    .trim()
}

/** 取第一篇正文段落当 description（H1 之后的第一段）。 */
function describe(source, fallback) {
  const tokens = new Marked({ gfm: true }).lexer(source)
  for (const token of tokens) {
    if (token.type === 'paragraph') {
      const text = stripInline(token.text)
      if (text) return text.length > 120 ? `${text.slice(0, 118)}…` : text
    }
  }
  return fallback
}

/** 从 `<h1>` 里取页面标题，取不到就用文件名。 */
function titleOf(source, fileName) {
  const tokens = new Marked({ gfm: true }).lexer(source)
  const h1 = tokens.find((t) => t.type === 'heading' && t.depth === 1)
  return h1 ? stripInline(h1.text) : fileName.replace(/\.md$/i, '')
}

async function listMarkdown(srcDir) {
  const entries = await readdir(srcDir)
  return entries.filter((name) => /\.md$/i.test(name)).sort()
}

async function copyAssets(srcDir, outDir, images) {
  let copied = 0
  let bytes = 0
  for (const rel of images) {
    const from = path.join(srcDir, rel)
    const to = path.join(outDir, rel)
    if (!existsSync(from)) continue
    await mkdir(path.dirname(to), { recursive: true })
    await copyFile(from, to)
    bytes += (await stat(to)).size
    copied++
  }
  return { copied, bytes }
}

/**
 * 生成整站。
 * @param {{src?: string, out?: string, quiet?: boolean}} options
 */
export async function buildSite({ src = DEFAULT_SRC, out = DEFAULT_OUT, quiet = false } = {}) {
  const log = quiet ? () => {} : (msg) => process.stdout.write(`${msg}\n`)

  const files = await listMarkdown(src)
  if (!files.length) throw new Error(`手册目录里没有 .md 文件：${src}`)

  // 先扫一遍标题，侧栏与翻页都要用
  const docs = []
  const sources = new Map()
  for (const file of files) {
    const source = await readFile(path.join(src, file), 'utf8')
    sources.set(file, source)
    docs.push({ file, page: pageSlug(file), title: titleOf(source, file) })
  }

  const dup = docs.find((d, i) => docs.findIndex((o) => o.page === d.page) !== i)
  if (dup) throw new Error(`两个文件名会生成同一个页面：${dup.page}`)

  const readme = sources.get('README.md')
  const groups = readme ? buildOutline(readme, docs) : [{ title: '目录', items: docs.map((d) => ({ page: d.page, title: d.title })) }]
  const flat = [docs.find((d) => d.page === INDEX_PAGE), ...groups.flatMap((g) => g.items.map((i) => docs.find((d) => d.page === i.page)))].filter(Boolean)
  // 没进侧栏的文档也要能翻到
  for (const doc of docs) if (!flat.includes(doc)) flat.push(doc)

  await rm(out, { recursive: true, force: true })
  await mkdir(out, { recursive: true })

  const images = new Set()
  let mermaidPages = 0

  for (const doc of docs) {
    const source = sources.get(doc.file)
    const { html, toc } = renderMarkdown(source, { collectImages: images })
    const withMermaid = html.includes('class="mermaid"')
    if (withMermaid) mermaidPages++

    const page = pageHtml({
      title: doc.title,
      description: describe(source, `${doc.title} — ClawBench 使用手册`),
      body: html,
      groups,
      toc,
      flat,
      page: doc.page,
      withMermaid,
    })
    await writeFile(path.join(out, doc.page), page, 'utf8')
  }

  await copyFile(path.join(HERE, 'docs.css'), path.join(out, 'docs.css'))
  await copyFile(path.join(HERE, 'docs.js'), path.join(out, 'docs.js'))

  // 只有真的用到流程图时才带上 mermaid（3MB），否则不占带宽
  if (mermaidPages) {
    const mermaidSrc = path.join(ROOT, 'node_modules/mermaid/dist/mermaid.min.js')
    if (existsSync(mermaidSrc)) await copyFile(mermaidSrc, path.join(out, 'mermaid.min.js'))
    else log('  ! 未找到 node_modules/mermaid/dist/mermaid.min.js，流程图将显示源码')
  }

  const { copied, bytes } = await copyAssets(src, out, images)

  log(`手册站点已生成：${path.relative(ROOT, out) || out}`)
  log(`  ${docs.length} 个页面 · ${copied} 张截图（${(bytes / 1024 / 1024).toFixed(1)} MB）· ${groups.length} 个分组`)
  return { pages: docs.length, images: copied, groups: groups.length, out }
}

// ── CLI ────────────────────────────────────────────────────────

/** CLI 参数解析（独立成函数便于测试）。 */
export function parseArgs(argv) {
  const options = {}
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]
    if (arg === '--out' || arg === '-o') options.out = path.resolve(argv[++i])
    else if (arg === '--src' || arg === '-s') options.src = path.resolve(argv[++i])
    else if (arg === '--quiet' || arg === '-q') options.quiet = true
    else if (arg === '--help' || arg === '-h') options.help = true
    else throw new Error(`未知参数：${arg}`)
  }
  return options
}

const invokedDirectly = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href
if (invokedDirectly) {
  const options = parseArgs(process.argv.slice(2))
  if (options.help) {
    process.stdout.write('用法：node web/docs/build.mjs [--out <目录>] [--src <目录>] [--quiet]\n')
  } else {
    buildSite(options).catch((err) => {
      process.stderr.write(`生成手册站点失败：${err.message}\n`)
      process.exit(1)
    })
  }
}
