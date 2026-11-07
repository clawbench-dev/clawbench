import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * The terminal toolbar only exists on touch devices and its tool group lives in
 * a horizontally scrolling strip, so a mount test would need the whole terminal
 * stack. These guards pin the wiring instead: the strip is what gets measured
 * (a strip that fits shows labels, one that scrolls does not), and every icon in
 * the tool group carries a label.
 */
describe('terminal toolbar labels', () => {
  const vue = readFileSync(resolve(__dirname, '../TerminalPanelContent.vue'), 'utf8')

  it('runs the strip in scroll mode, so it owns its own labels and fades', () => {
    expect(vue).toContain('class="toolbar-scroll" mode="scroll"')
  })

  it('labels every button in the tool group', () => {
    const start = vue.indexOf('class="key-group btn-func-group"')
    const end = vue.indexOf('<!-- Quick commands popup -->')
    expect(start).toBeGreaterThan(-1)
    expect(end).toBeGreaterThan(start)

    const group = vue.slice(start, end)
    const buttons = group.match(/class="toolbar-btn btn-action btn-func"/g) ?? []
    const labels = group.match(/class="action-label"/g) ?? []
    expect(buttons.length).toBe(6)
    expect(labels.length).toBe(buttons.length)
  })
})
