import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * 表格样式的两条契约：
 *
 *  1. **归属**——表格的边框/底色/外壳只在 `web/css/table.css` 里声明。以前这些
 *     规则散在 markdown-common.css、content.css、code-block-header.css、
 *     ChatMessageItem.vue、DataTable.vue 五处，改一处另四处还在抢。
 *  2. **不叠线**——手机上「表格的线很粗」不是 1px 在高分屏变粗，而是两条 1px
 *     并排：separate 下相邻两格各画的边不合并；合计行的上边线又和最后一行
 *     的下边线叠在一起。
 *
 * jsdom 没有 CSS 引擎，量不出「实际画了几 px」，所以这里钉的是产生单线的
 * 那些声明（与 countBadge.css.test.ts 同一套源码守卫思路）。
 */

const WEB_ROOT = join(__dirname, '..', '..', '..', '..')
const CSS_DIR = join(WEB_ROOT, 'css')

function read(relPath: string): string {
  return readFileSync(join(WEB_ROOT, relPath), 'utf8')
}

const css = readFileSync(join(CSS_DIR, 'table.css'), 'utf8')
const dataTableSrc = read('src/components/common/DataTable.vue')

interface Rule {
  selectors: string[]
  decls: string
}

/** 只取顶层规则；@media 里的嵌套规则忽略（本文件的断言都不在里面）。 */
function topLevelRules(source: string): Rule[] {
  const body = source.replace(/\/\*[\s\S]*?\*\//g, '')
  const rules: Rule[] = []
  let depth = 0
  let selectorStart = 0
  for (let i = 0; i < body.length; i += 1) {
    const ch = body[i]
    if (ch === '{') {
      if (depth === 0) {
        const selectorText = body.slice(selectorStart, i)
        const close = body.indexOf('}', i)
        expect(close, `unterminated rule: ${selectorText}`).toBeGreaterThan(i)
        rules.push({
          selectors: selectorText.split(',').map(s => s.trim().replace(/\s+/g, ' ')),
          decls: body.slice(i + 1, close),
        })
        i = close
        selectorStart = close + 1
        continue
      }
      depth += 1
    } else if (ch === '}') {
      depth = Math.max(0, depth - 1)
      if (depth === 0) selectorStart = i + 1
    }
  }
  return rules
}

const RULES = topLevelRules(css)

function ruleFor(selector: string): Rule {
  const found = RULES.find(r => r.selectors.includes(selector))
  expect(found, `rule not found in css/table.css: ${selector}`).toBeDefined()
  return found!
}

function decl(rule: Rule, prop: string): string | null {
  const m = rule.decls.match(new RegExp(`(?:^|;)\\s*${prop}\\s*:\\s*([^;]+)`))
  return m ? m[1].trim() : null
}

describe('表格样式归属', () => {
  it('表格规则只在 css/table.css 里', () => {
    const others = [
      'css/markdown-common.css',
      'css/content.css',
      'css/code-block-header.css',
      'src/components/chat/ChatMessageItem.vue',
    ]
    for (const file of others) {
      const source = read(file)
      expect(source, `${file} 不该再声明表格边框`).not.toMatch(/border-collapse\s*:/)
      expect(source, `${file} 不该再声明表格边框`).not.toMatch(/var\(--table-border\)/)
    }
    // 组件的 <style> 也不该再写网格（只留滚动容器与表头按钮）
    expect(dataTableSrc).not.toMatch(/\.dt-grid\s*\{/)
  })

  it('table.css 被两个入口都引入（应用与分享页）', () => {
    expect(read('index.html')).toMatch(/\/css\/table\.css/)
    expect(read('src/share/main.ts')).toMatch(/css\/table\.css/)
  })
})

describe('DataTable 单元格边框（不许叠成 2px）', () => {
  it('简单表用 collapse，相邻两格的边才会合成一条', () => {
    expect(decl(ruleFor('.dt:not(.is-data) .dt-grid'), 'border-collapse')).toBe('collapse')
  })

  it('简单表的格子仍然是完整 1px 网格（collapse 的前提）', () => {
    expect(decl(ruleFor('.dt:not(.is-data) .dt-grid th'), 'border')).toBe(
      '1px solid var(--border-color)',
    )
    expect(decl(ruleFor('.dt:not(.is-data) .dt-grid td'), 'border')).toBe(
      '1px solid var(--border-color)',
    )
  })

  it('数据表的格子有自己的完整边框，但每条线只画一次', () => {
    // 全局 th/td 规则会给每格加「四边 1px」。只写需要的三边、另外几边归零，
    // 才能既保住网格又不让相邻两格各画一条叠成 2px。
    for (const selector of ['.dt .dt-grid th', '.dt .dt-grid td']) {
      const cell = ruleFor(selector)
      expect(decl(cell, 'border')).toBe('0')
      expect(decl(cell, 'border-right')).toBe('1px solid var(--border-color)')
      expect(decl(cell, 'border-bottom')).toBe('1px solid var(--border-color)')
    }
    // 左/上两条边各补一次，拼成闭合网格
    expect(decl(ruleFor('.dt .dt-grid th:first-child'), 'border-left')).toBe(
      '1px solid var(--border-color)',
    )
    expect(decl(ruleFor('.dt .dt-grid td:first-child'), 'border-left')).toBe(
      '1px solid var(--border-color)',
    )
    expect(decl(ruleFor('.dt .dt-grid thead th'), 'border-top')).toBe(
      '1px solid var(--border-color)',
    )
  })

  it('字号挂在 `.dt .dt-grid` 上（否则会被 .markdown-body table 抢走）', () => {
    expect(decl(ruleFor('.dt .dt-grid'), 'font-size')).toBe('var(--font-size-sm)')
  })

  it('数据表自己声明合并方式，不受聊天里 collapse 规则影响', () => {
    const grid = ruleFor('.dt.is-data .dt-grid')
    expect(decl(grid, 'border-collapse')).toBe('separate')
    expect(decl(grid, 'border-spacing')).toBe('0')
  })

  it('合计行存在时，最后一条数据行不再画下边线', () => {
    const last = ruleFor('.dt.is-data .dt-grid.has-total tbody tr:last-child td')
    expect(decl(last, 'border-bottom')).toBe('0')
    // 这个选择器只有在 table 真的带上 has-total 时才生效，两边必须成对。
    expect(dataTableSrc).toMatch(/<table[^>]*class="dt-grid"[^>]*:class="\{ 'has-total': showFooter \}"/)
  })

  it('冻结列不再额外画一条分隔线（会与格子自己的右边叠成 2px）', () => {
    expect(decl(ruleFor('.dt.is-data .dt-grid td.is-frozen'), 'box-shadow')).toBeNull()
    expect(decl(ruleFor('.dt.is-data .dt-grid th.is-frozen'), 'box-shadow')).toBeNull()
  })
})
