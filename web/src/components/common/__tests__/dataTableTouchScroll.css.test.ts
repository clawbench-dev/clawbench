import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * 表格滚动容器的两条契约（手机上「手指落在表格上，屏幕滚不动」的两个成因）：
 *
 *  1. **纵向不许拦**——`overscroll-behavior: contain` 是整条轴的：只要
 *     `.dt-scroll` 是滚动容器（表格比气泡宽就够了，纵向根本不需要滚），
 *     竖滑就再也不会链给聊天区。实测：仅横向溢出 + 该简写，竖滑后页面
 *     `scrollY` 恒为 0；换成 `overscroll-behavior-x: contain` 同一次手势滚 399px。
 *  2. **触屏不设高度上限**——有上限就有嵌套纵向滚动：手指在表格上滑，滚的是
 *     那 420px 的窗口，屏幕不动；滑到头还有第 1 条拦着。触屏上表格应当跟着
 *     消息一起长，滚动交给消息本身（长表用「横屏查看」，浮层自己滚）。
 *
 * 高度上限还必须走自定义属性：内联 `max-height` 会赢过任何选择器，媒体查询
 * 永远压不掉它。jsdom 没有 CSS 引擎，所以这里钉的是源码声明。
 */

const COMPONENT = join(__dirname, '..', 'DataTable.vue')
const source = readFileSync(COMPONENT, 'utf8')

const styleBlock = (() => {
  const match = source.match(/<style scoped>([\s\S]*?)<\/style>/)
  if (!match) throw new Error('DataTable.vue must keep a <style scoped> block')
  return match[1].replace(/\/\*[\s\S]*?\*\//g, '')
})()

const templateBlock = source.slice(0, source.indexOf('<script'))

/** 取一个选择器的声明块（只找第一个匹配，够用且不会误伤 @media 里的同名规则）。 */
function declarations(selector: string): string {
  const index = styleBlock.indexOf(`${selector} {`)
  expect(index, `${selector} rule is missing`).toBeGreaterThanOrEqual(0)
  const open = styleBlock.indexOf('{', index)
  const close = styleBlock.indexOf('}', open)
  return styleBlock.slice(open + 1, close)
}

function mediaBlock(query: string): string {
  const index = styleBlock.indexOf(`@media ${query} {`)
  expect(index, `@media ${query} block is missing`).toBeGreaterThanOrEqual(0)
  let depth = 0
  for (let i = styleBlock.indexOf('{', index); i < styleBlock.length; i += 1) {
    if (styleBlock[i] === '{') depth += 1
    else if (styleBlock[i] === '}') {
      depth -= 1
      if (depth === 0) return styleBlock.slice(index, i + 1)
    }
  }
  throw new Error(`unterminated @media ${query}`)
}

describe('DataTable 滚动容器的触屏行为', () => {
  it('只拦横向 overscroll，纵向留给外层', () => {
    const decls = declarations('.dt-scroll')
    expect(decls).toMatch(/overscroll-behavior-x:\s*contain/)
    // 简写会把纵向一起拦掉 —— 这正是「手指落在表格上屏幕滚不动」的成因之一。
    expect(decls).not.toMatch(/overscroll-behavior\s*:/)
  })

  it('高度上限走自定义属性，触屏上整条关掉', () => {
    expect(declarations('.dt-scroll')).toMatch(/max-height:\s*var\(--dt-scroll-max-h,\s*none\)/)
    const coarse = mediaBlock('(pointer: coarse)')
    expect(coarse).toMatch(/\.dt-scroll\s*\{[^}]*max-height:\s*none/)
  })

  it('模板不写内联 max-height（会压过媒体查询）', () => {
    expect(templateBlock).not.toMatch(/:style="[^"]*maxHeight/)
    expect(source).toMatch(/'--dt-scroll-max-h':\s*props\.maxHeight/)
  })
})
