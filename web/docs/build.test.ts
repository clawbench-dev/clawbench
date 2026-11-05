/**
 * 用户手册静态站点的守卫测试。
 *
 * 这里不断言样式，只钉住几类「肉眼看不出来、上线才发现」的坏法：
 *   - 手册里的 `[设置](settings.md#用量)` 没被改写成 `.html`，点进去 404；
 *   - 标题锚点算法与 GitHub 不一致，`#54-用户称谓` 这类深链全部落空；
 *   - 引用了不存在的截图，深色背景上表现为一块空白；
 *   - 站点漏引 landing.css，页面变成白底黑字的裸 HTML。
 *
 * 断言跑在**真实构建产物**上（不是源码文本），所以渲染器一改就会被抓到。
 */
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { existsSync, mkdtempSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve, sep } from 'node:path'

import { buildSite, pageSlug, parseArgs, rewriteHref, slugify, DEFAULT_SRC } from './build.mjs'

let out = ''
let pages: string[] = []
const allIds = new Map<string, Set<string>>()

beforeAll(async () => {
  out = mkdtempSync(join(tmpdir(), 'clawbench-docs-'))
  await buildSite({ out, quiet: true })
  pages = readdirSync(out).filter((f) => f.endsWith('.html'))
  for (const page of pages) {
    allIds.set(page, new Set([...read(page).matchAll(/\sid="([^"]*)"/g)].map((m) => m[1])))
  }
}, 120_000)

afterAll(() => {
  if (out) rmSync(out, { recursive: true, force: true })
})

function read(page: string): string {
  return readFileSync(join(out, page), 'utf8')
}

describe('slugify', () => {
  // 这几个是从手册里真实存在的深链反推出来的，改算法会立刻断链
  it('matches GitHub anchors for numbered Chinese headings', () => {
    expect(slugify('5.4 用户称谓')).toBe('54-用户称谓')
    expect(slugify('7. 成员管理')).toBe('7-成员管理')
    expect(slugify('4.4 自动巡航')).toBe('44-自动巡航')
  })

  it('keeps latin words and lowercases them', () => {
    expect(slugify('Android 音量键')).toBe('android-音量键')
    expect(slugify('MCP 服务')).toBe('mcp-服务')
  })

  it('drops punctuation that GitHub also drops', () => {
    expect(slugify('用量（设置 → 用量）')).toBe('用量设置--用量')
  })
})

describe('pageSlug', () => {
  it('maps README to the site index', () => {
    expect(pageSlug('README.md')).toBe('index.html')
    expect(pageSlug('readme.md')).toBe('index.html')
  })

  it('lowercases file names so URLs are stable across case-insensitive checkouts', () => {
    expect(pageSlug('TERMINAL.md')).toBe('terminal.html')
    expect(pageSlug('chat.md')).toBe('chat.html')
  })
})

describe('rewriteHref', () => {
  it('turns sibling markdown links into pages, keeping the anchor', () => {
    expect(rewriteHref('settings.md#用量')).toBe('settings.html#用量')
    expect(rewriteHref('README.md')).toBe('index.html')
  })

  it('sends links that leave the manual to GitHub', () => {
    expect(rewriteHref('../../README.md#核心功能')).toBe(
      'https://github.com/clawbench-dev/clawbench/blob/main/README.md#核心功能',
    )
  })

  it('leaves external links and in-page anchors alone', () => {
    expect(rewriteHref('https://example.com/a.md')).toBe('https://example.com/a.md')
    expect(rewriteHref('mailto:a@b.c')).toBe('mailto:a@b.c')
    expect(rewriteHref('#用量')).toBe('#用量')
    expect(rewriteHref('screenshots/chat-01.png')).toBe('screenshots/chat-01.png')
  })
})

describe('generated site', () => {
  it('produces one page per markdown file', () => {
    const sources = readdirSync(DEFAULT_SRC).filter((f) => f.endsWith('.md'))
    expect(pages.length).toBe(sources.length)
    expect(pages).toContain('index.html')
  })

  it('leaves no markdown links in the output', () => {
    for (const page of pages) {
      const bad = [...read(page).matchAll(/href="([^"]*\.md[^"]*)"/g)]
        .map((m) => m[1])
        // 指回 GitHub 的仓库文档（README 等）本来就该是 .md
        .filter((href) => !/^https?:/.test(href))
      expect(bad, `${page} 里还有 .md 链接`).toEqual([])
    }
  })

  it('resolves every internal link and anchor', () => {
    const broken: string[] = []
    for (const page of pages) {
      for (const [, href] of read(page).matchAll(/href="([^"]*)"/g)) {
        if (/^(https?:|mailto:|tel:)/.test(href)) continue
        const [target, hash] = href.split('#')
        let file = page
        if (target) {
          const rel = resolve(join(out, dirname(page)), target)
          // 指向落地页的共享资源（../css、../img、../index.html）不在这里生成，
          // 由部署时的暂存步骤保证；本测试只管站点自己产出的文件。
          if (!rel.startsWith(out + sep)) continue
          if (!existsSync(rel)) {
            broken.push(`${page} → ${href}（文件不存在）`)
            continue
          }
          file = rel.slice(out.length + 1)
          if (!file.endsWith('.html')) continue
        }
        if (hash) {
          const id = decodeURIComponent(hash)
          if (!allIds.get(file)?.has(id)) broken.push(`${page} → ${href}（锚点不存在）`)
        }
      }
    }
    expect(broken).toEqual([])
  })

  it('copies every referenced screenshot', () => {
    const missing: string[] = []
    for (const page of pages) {
      for (const [, src] of read(page).matchAll(/<img src="([^"]*)"/g)) {
        if (/^(https?:|\.\.\/)/.test(src)) continue
        if (!existsSync(join(out, src))) missing.push(`${page} → ${src}`)
      }
    }
    expect(missing).toEqual([])
  })

  it('reuses the landing stylesheet so the two sites cannot drift apart', () => {
    for (const page of pages) {
      const html = read(page)
      expect(html, page).toContain('href="../css/landing.css"')
      expect(html, page).toContain('href="docs.css"')
      expect(html, page).toContain('class="nav is-stuck"')
    }
  })

  it('lists every page in the sidebar', () => {
    const html = read('index.html')
    for (const page of pages) {
      if (page === 'index.html') continue
      expect(html, `${page} 不在侧栏里`).toContain(`href="${page}"`)
    }
  })

  it('marks the current page in the sidebar and links back to the landing page', () => {
    const html = read('settings.html')
    expect(html).toContain('href="settings.html" class="is-active" aria-current="page"')
    expect(html).toContain('href="../index.html"')
  })

  it('builds a table of contents from the page headings', () => {
    const html = read('settings.html')
    expect(html).toContain('class="docs-toc"')
    // 目录链接与正文标题的 id 必须是同一个字符串，否则点了没反应
    expect(html).toContain('href="#用量"')
    expect(html).toContain('id="用量"')
  })

  it('wraps tables for horizontal scrolling and highlights code', () => {
    const withTable = read('settings.html')
    expect(withTable).toContain('<div class="table-wrap"><table>')
    const withCode = read('public_access.html')
    expect(withCode).toContain('class="hljs language-bash"')
  })

  it('only ships mermaid on pages that actually draw a diagram', () => {
    expect(read('public_access.html')).toContain('src="mermaid.min.js"')
    expect(read('chat.html')).not.toContain('src="mermaid.min.js"')
  })

  it('chains pages with prev/next links', () => {
    const html = read('settings.html')
    expect(html).toContain('class="docs-pager"')
    expect(html).toContain('pager-prev')
    expect(html).toContain('pager-next')
  })
})

describe('parseArgs', () => {
  it('accepts the flags the deploy workflows use', () => {
    const options = parseArgs(['--out', 'dist/docs', '--quiet'])
    expect(options.out).toContain('dist/docs')
    expect(options.quiet).toBe(true)
  })

  it('accepts the short spellings too', () => {
    const options = parseArgs(['-o', 'x', '-s', 'y', '-h'])
    expect(options.help).toBe(true)
    expect(options.out).toContain('x')
    expect(options.src).toContain('y')
  })

  it('rejects unknown flags instead of silently ignoring them', () => {
    expect(() => parseArgs(['--verbose'])).toThrow(/未知参数/)
  })
})

describe('buildSite guards', () => {
  it('refuses to build when two files collapse onto the same page', async () => {
    const src = mkdtempSync(join(tmpdir(), 'clawbench-docs-src-'))
    writeFileSync(join(src, 'FAQ.md'), '# 一\n\n正文\n')
    writeFileSync(join(src, 'faq.md'), '# 二\n\n正文\n')
    // 大小写不敏感的文件系统（macOS 默认）写不下这两个名字，跳过
    if (readdirSync(src).length < 2) {
      rmSync(src, { recursive: true, force: true })
      return
    }
    await expect(buildSite({ src, out: join(src, 'out'), quiet: true })).rejects.toThrow(/同一个页面/)
    rmSync(src, { recursive: true, force: true })
  })

  it('refuses an empty source directory instead of emitting an empty site', async () => {
    const src = mkdtempSync(join(tmpdir(), 'clawbench-docs-empty-'))
    await expect(buildSite({ src, out: join(src, 'out'), quiet: true })).rejects.toThrow(/没有 .md/)
    rmSync(src, { recursive: true, force: true })
  })

  it('keeps docs.js and docs.css next to the pages', () => {
    expect(existsSync(join(out, 'docs.css'))).toBe(true)
    expect(existsSync(join(out, 'docs.js'))).toBe(true)
  })

  it('is reproducible: two builds produce byte-identical pages', async () => {
    const second = mkdtempSync(join(tmpdir(), 'clawbench-docs-2-'))
    await buildSite({ out: second, quiet: true })
    for (const page of pages) {
      expect(readFileSync(join(second, page), 'utf8'), page).toBe(read(page))
    }
    rmSync(second, { recursive: true, force: true })
  })
})
