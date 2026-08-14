import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
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

  it('resets to the first tip when the context list changes', async () => {
    const wrapper = await mountTicker({ tips: [TIPS[0]] })
    expect(wrapper.text()).toContain('c.send')
    await wrapper.setProps({ tips: [] })
    expect(wrapper.find('.stt').exists()).toBe(false)
  })
})
