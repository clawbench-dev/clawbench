import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * 全屏浮层（`.block-landscape`）的三条契约，全部只能靠源码守卫：jsdom 没有
 * CSS 引擎，Electron 的窗口拖拽区也不在单测里存在。
 *
 *  1. **不能落在桌面窗口的拖拽区里**。应用顶栏是 `-webkit-app-region: drag`
 *     的窗口拖动把手，而 Chromium 的拖拽区是按布局算的、不看 z-index ——
 *     浮层盖在顶栏上，它最上面那一条仍然会被当成「拖窗口」吞掉。表格的表头
 *     栏正好在最上面，于是表头按钮全部点不动、连 hover 都没有。
 *  2. **半透明遮罩**，不是把整块页面盖死的不透明底。
 *  3. **小的居中、大的占满**：表格按内容高度垂直居中，高过面板时被
 *     `max-height: 100%` 卡住并自己滚。
 */

const WEB_ROOT = join(__dirname, '..', '..', '..', '..')
const css = readFileSync(join(WEB_ROOT, 'css', 'code-block-header.css'), 'utf8')
  .replace(/\/\*[\s\S]*?\*\//g, '')

const overlaySrc = readFileSync(
  join(WEB_ROOT, 'src', 'components', 'common', 'BlockLandscape.vue'),
  'utf8',
)

/** 取某个选择器**完全相同**的那条顶层规则的声明体。 */
function declsOf(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const match = css.match(new RegExp(`(?:^|\\n)\\s*${escaped}\\s*\\{([^}]*)\\}`))
  expect(match, `missing rule: ${selector}`).not.toBeNull()
  return match![1]
}

describe('block landscape chrome', () => {
  it('opts out of the desktop window drag region', () => {
    // 少了这一条，全屏表格最上面的表头栏在桌面壳里是死区。
    expect(declsOf('.block-landscape')).toMatch(/-webkit-app-region:\s*no-drag/)
  })

  it('uses a semi-transparent scrim instead of an opaque page cover', () => {
    const decls = declsOf('.block-landscape')
    expect(decls).toMatch(/background:\s*color-mix\([^;]*transparent/)
    expect(decls).not.toMatch(/background:\s*var\(--bg-primary\)\s*;/)
  })

  it('centers a short table and caps a tall one at the panel height', () => {
    expect(declsOf('.block-landscape-live')).toMatch(/display:\s*flex/)
    expect(declsOf('.block-landscape-live')).toMatch(/flex-direction:\s*column/)
    const dt = declsOf('.block-landscape-live .dt')
    expect(dt).toMatch(/margin:\s*auto 0/)
    expect(dt).toMatch(/max-height:\s*100%/)
    // 居中用的是自动外边距：内容比面板高时它会归零，第一行仍然可达。
    expect(dt).not.toMatch(/justify-content/)
  })

  it('centers the cloned block the same way without losing the scroll start', () => {
    expect(declsOf('.block-landscape-scroll > .table-block-wrapper,\n.block-landscape-scroll > .code-block-wrapper')).toMatch(/margin:\s*auto/)
  })

  it('does not pass `fill` to the live table (it would force flex:1 and defeat centering)', () => {
    const tag = overlaySrc.match(/<DataTable[\s\S]*?\/>/)
    expect(tag).not.toBeNull()
    expect(tag![0]).not.toMatch(/\sfill\s/)
  })
})
