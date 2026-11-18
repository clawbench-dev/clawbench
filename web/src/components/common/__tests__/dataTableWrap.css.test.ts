import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * 数据表不能把内容藏起来。
 *
 * 曾经的 `.dt-cell` 是 `max-width: 320dp + nowrap + ellipsis`：布局好看，但长
 * 说明/长链接被截成一个省略号，用户看不到完整内容。现在的契约是：
 *
 *  - 数据表正文格在 320dp 处**折行**（Word wrap / anywhere），不裁剪；
 *  - 数字列保持单行（折行会毁掉按位对齐）；
 *  - 简单表维持原来的「宽度跟内容走 + 可换行」。
 *
 * jsdom 没有 CSS 引擎，所以这里钉的是产生该行为的声明。
 */

const WEB_ROOT = join(__dirname, '..', '..', '..', '..')
const css = readFileSync(join(WEB_ROOT, 'css', 'table.css'), 'utf8')

interface Rule {
  selectors: string[]
  decls: string
}

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

function decl(rule: Rule, prop: string): string | null {
  const match = rule.decls.match(new RegExp(`(?:^|;)\\s*${prop}\\s*:\\s*([^;]+)`))
  return match ? match[1].trim() : null
}

function ruleFor(selector: string): Rule {
  const found = RULES.find(r => r.selectors.includes(selector))
  expect(found, `rule not found in css/table.css: ${selector}`).toBeDefined()
  return found!
}

describe('data table cell wrapping', () => {
  it('wraps long cell content instead of clipping it with an ellipsis', () => {
    const cell = ruleFor('.dt-cell')
    expect(decl(cell, 'white-space')).toBe('normal')
    expect(decl(cell, 'overflow-wrap')).toBe('anywhere')
    // 320dp is the point where it wraps, not where it is cut off.
    expect(decl(cell, 'max-width')).toBe('calc(320 * var(--dp))')
    expect(decl(cell, 'text-overflow')).toBeNull()
    expect(decl(cell, 'overflow')).toBeNull()
  })

  it('keeps numeric columns on one line', () => {
    const numeric = ruleFor('.dt.is-data .is-numeric .dt-cell')
    expect(decl(numeric, 'white-space')).toBe('nowrap')
    expect(decl(numeric, 'max-width')).toBe('none')
  })

  it('lets a long header label wrap too', () => {
    const label = ruleFor('.dt-th-label')
    expect(decl(label, 'white-space')).toBe('normal')
    expect(decl(label, 'text-overflow')).toBeNull()
  })

  it('overrides the chat bubble nowrap for data-table body cells only', () => {
    const th = ruleFor('.table-block-wrapper.is-data-table .dt.is-data th')
    const td = ruleFor('.table-block-wrapper.is-data-table .dt.is-data td')
    expect(decl(th, 'white-space')).toBe('nowrap !important')
    expect(decl(td, 'white-space')).toBe('normal !important')
    expect(decl(td, 'word-break')).toBeNull()
  })

  it('keeps the simple table on its old inline / content-sized behaviour', () => {
    const simple = ruleFor('.dt:not(.is-data) .dt-cell')
    expect(decl(simple, 'display')).toBe('inline')
    expect(decl(simple, 'white-space')).toBe('inherit')
    expect(decl(simple, 'max-width')).toBe('none')
  })
})
