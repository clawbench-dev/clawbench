import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import BusyBar from '../BusyBar.vue'
import { readWebFile } from '@/testUtils/readWebFile'

const SRC = readWebFile('src/components/common/BusyBar.vue')

/**
 * The component's own header comment explains WHY there is no reduced-motion
 * opt-out, so a naive `expect(src).not.toMatch(/prefers-reduced-motion/)` would
 * fail on the prose that documents the decision. Strip comments first — the
 * assertion is about CSS declarations, not about the words around them.
 */
function stripComments(s: string): string {
  return s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/<!--[\s\S]*?-->/g, '')
}
const CSS = stripComments(SRC)

describe('BusyBar', () => {
  it('renders nothing when not visible', () => {
    const wrapper = mount(BusyBar, { props: { visible: false, label: 'Working' } })
    expect(wrapper.find('.busy-bar').exists()).toBe(false)
  })

  it('renders the track and fill when visible', () => {
    const wrapper = mount(BusyBar, { props: { visible: true, label: 'Working' } })
    expect(wrapper.find('.busy-bar').exists()).toBe(true)
    expect(wrapper.find('.busy-bar-fill').exists()).toBe(true)
  })

  it('exposes the label as the accessible name (indeterminate: no aria-valuenow)', () => {
    const wrapper = mount(BusyBar, { props: { visible: true, label: 'Forking session' } })
    const bar = wrapper.find('.busy-bar')
    expect(bar.attributes('role')).toBe('progressbar')
    expect(bar.attributes('aria-label')).toBe('Forking session')
    // An indeterminate bar must NOT claim a value, or screen readers announce a
    // fake percentage.
    expect(bar.attributes('aria-valuenow')).toBeUndefined()
    expect(bar.attributes('aria-valuetext')).toBeUndefined()
  })

  it('toggles with the visible prop', async () => {
    const wrapper = mount(BusyBar, { props: { visible: false, label: 'Working' } })
    await wrapper.setProps({ visible: true })
    expect(wrapper.find('.busy-bar').exists()).toBe(true)
    await wrapper.setProps({ visible: false })
    expect(wrapper.find('.busy-bar').exists()).toBe(false)
  })

  it('never intercepts pointer events (it overlays the message area)', () => {
    // The bar is absolutely positioned across the top of the panel; if it were
    // clickable it would swallow taps on whatever sits underneath.
    expect(CSS).toMatch(/pointer-events:\s*none/)
  })

  // ── Deliberate absence of a reduced-motion opt-out ──
  // The sweep is the ONLY signal distinguishing "working" from "hung"; freezing
  // it leaves a static half-filled bar, which reads as a stalled transfer. This
  // mirrors TransferProgressBar's indeterminate fill and the WAAPI-driven
  // indicators, which never consult the preference. See the component's header
  // comment and design-guide.md § 动效.
  it('has no prefers-reduced-motion opt-out (the sweep is the information)', () => {
    expect(CSS).not.toMatch(/prefers-reduced-motion/)
  })

  it('actually animates the fill (guards against "deleted the animation and still passes")', () => {
    // Asserting only the absence of the media query would pass if someone
    // deleted the animation entirely, leaving a dead bar.
    expect(CSS).toMatch(/animation:\s*busy-bar-sweep/)
    expect(CSS).toMatch(/@keyframes\s+busy-bar-sweep/)
  })
})
