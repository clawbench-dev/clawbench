import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import ShortcutTipTicker from '../ShortcutTipTicker.vue'
import type { ShortcutContext, ShortcutTipDef } from '@/config/shortcutTips'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/config/shortcutTips', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/config/shortcutTips')>()
  return {
    ...actual,
    getShortcutTipsForContext: (ctx: ShortcutContext) =>
      ctx === 'chat'
        ? [{ context: 'chat' as ShortcutContext, contextKey: 'c.ctx', keys: ['Ctrl+K'], actionKey: 'a.ctx' }]
        : [],
  }
})

const TIPS: ShortcutTipDef[] = [
  { context: 'common', contextKey: 'c.send', keys: ['Enter', 'Shift+Enter'], actionKey: 'a.send' },
  { context: 'common', contextKey: 'c.search', keys: ['Ctrl+F'], actionKey: 'a.search' },
  { context: 'common', contextKey: 'c.recommend', actionKey: 'a.recommend' },
]

// jsdom measures clientWidth/scrollWidth as 0 → never overflows → the
// no-overflow path (showMs wait) is what runs under test.
const props = { tips: TIPS, showMs: 1000, vertMs: 100 }

async function mountTicker(overrides?: Record<string, unknown>) {
  const wrapper = mount(ShortcutTipTicker, {
    props: { ...props, ...overrides },
  })
  await nextTick()
  return wrapper
}

describe('ShortcutTipTicker', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders the first tip context/action', async () => {
    const wrapper = await mountTicker()
    expect(wrapper.text()).toContain('c.send')
    expect(wrapper.text()).toContain('a.send')
  })

  it('renders kbd for keys', async () => {
    const wrapper = await mountTicker()
    const kbs = wrapper.findAll('.stt-kbd')
    expect(kbs.map((k) => k.text())).toEqual(['Enter', 'Shift+Enter'])
  })

  it('does not render kbd when a tip has no keys', async () => {
    const wrapper = await mountTicker({ tips: [TIPS[2]] })
    expect(wrapper.findAll('.stt-kbd')).toHaveLength(0)
    expect(wrapper.text()).toContain('c.recommend')
  })

  it('advances to the next tip after showMs (vertical switch)', async () => {
    const wrapper = await mountTicker()
    expect(wrapper.text()).toContain('c.send')

    vi.advanceTimersByTime(props.showMs + 140) // out transition
    await nextTick()
    expect(wrapper.text()).toContain('c.search')
  })

  it('loops back to the first tip after the last', async () => {
    const wrapper = await mountTicker()
    for (let i = 0; i < TIPS.length; i++) {
      vi.advanceTimersByTime(props.showMs + 140 + props.vertMs + 80)
      await nextTick()
    }
    // after cycling through all tips, we're back at the first
    expect(wrapper.text()).toContain('c.send')
  })

  it('renders nothing for an empty tips list', async () => {
    const wrapper = await mountTicker({ tips: [] })
    expect(wrapper.find('.stt').exists()).toBe(false)
  })

  it('cleans up timers on unmount', async () => {
    const wrapper = await mountTicker()
    wrapper.unmount()
    // advancing timers after unmount must not throw
    vi.advanceTimersByTime(props.showMs * 10)
    expect(true).toBe(true)
  })

  it('renders context tips when no tips prop is given', async () => {
    const wrapper = await mountTicker({ tips: undefined, context: 'chat' })
    expect(wrapper.text()).toContain('c.ctx')
  })

  it('resets to the first tip when the tips list changes', async () => {
    const wrapper = await mountTicker()
    expect(wrapper.text()).toContain('c.send')

    // advance to the second tip (index 1)
    vi.advanceTimersByTime(props.showMs + 140)
    await nextTick()
    expect(wrapper.text()).toContain('c.search')

    // swap to a single-tip list -> watcher resets index to 0
    await wrapper.setProps({ tips: [TIPS[2]] })
    await nextTick()
    expect(wrapper.text()).toContain('c.recommend')
  })
})

// The header ticker sits in the free space between the file capsule and the
// theme toggle. jsdom cannot measure flex layout, so the centering contract is
// pinned on the CSS itself: the container centers its content, and the viewport
// must stay shrink-to-fit (a full-width viewport would make centering a no-op
// and re-hug the capsule on the left).
describe('ShortcutTipTicker centering contract', () => {
  const src = readFileSync(resolve(__dirname, '../ShortcutTipTicker.vue'), 'utf8')

  it('centers the tip within the container', () => {
    expect(src).toMatch(/\.stt\s*\{[^}]*justify-content:\s*center;/)
  })

  it('lets the viewport shrink to fit instead of spanning the full width', () => {
    expect(src).toMatch(/\.stt-viewport\s*\{[^}]*width:\s*fit-content;/)
    expect(src).toMatch(/\.stt-viewport\s*\{[^}]*max-width:\s*100%;/)
    // A bare `width: 100%` would make the viewport span the whole region and
    // defeat the centering. (Lookbehind excludes `max-width`.)
    expect(src).not.toMatch(/\.stt-viewport\s*\{[^}]*(?<![-\w])width:\s*100%;/)
  })
})

/**
 * The window drag region (frameless desktop shell).
 *
 * `.stt` is `flex: 1`, so it spans the header's ENTIRE free band. Marking it
 * `-webkit-app-region: no-drag` therefore punched a window-sized hole in the
 * header's drag region: the window could not be moved by dragging the middle of
 * the header — exactly where a user naturally grabs. Measured in a browser at
 * 1400px wide, `.stt` was 1131.5px of the header.
 *
 * `no-drag` belongs on `.stt-viewport` instead, which is `width: fit-content`:
 * it shrinks to the tip text, so the tip stays clickable while the blank space
 * around it still drags the window.
 *
 * jsdom cannot evaluate `-webkit-app-region` (Chromium-only), so the contract is
 * pinned on the CSS source, like the centering checks above.
 */
describe('ShortcutTipTicker drag-region contract', () => {
  const src = readFileSync(resolve(__dirname, '../ShortcutTipTicker.vue'), 'utf8')
  const globalCss = readFileSync(resolve(__dirname, '../../../../css/layout.css'), 'utf8')

  it('opts the shrink-to-fit viewport out of the drag region', () => {
    expect(src).toMatch(/\.stt-viewport\s*\{[^}]*-webkit-app-region:\s*no-drag;/)
  })

  it('does NOT opt the full-width container out of the drag region', () => {
    // This is the regression itself. `.stt` fills the header's free space, so
    // exempting it makes most of the header undraggable.
    expect(src).not.toMatch(/\.stt\s*\{[^}]*-webkit-app-region:\s*no-drag;/)
  })

  it('does not exempt the full-width tips band in the global header rules', () => {
    // The same mistake can be made from the other file: `.header-tips` is the
    // class AppHeader puts on the ticker, and it is also `flex: 1`.
    expect(globalCss).not.toMatch(/\.header-tips\s*\{[^}]*-webkit-app-region:\s*no-drag;/)
    expect(globalCss).not.toMatch(/,\s*\.header\s+\.header-tips\s*\{[^}]*-webkit-app-region:\s*no-drag;/)
  })

  it('still exempts the controls that genuinely need clicking', () => {
    // Guarding against the over-correction: removing every no-drag would make
    // the header's buttons unusable, which is worse than an undraggable band.
    expect(globalCss).toMatch(/\.header\s+button[^{]*\{[^}]*-webkit-app-region:\s*no-drag;/)
    expect(globalCss).toMatch(/\.header\s+\.badge-capsule\s*\{[^}]*-webkit-app-region:\s*no-drag;/)
  })
})
