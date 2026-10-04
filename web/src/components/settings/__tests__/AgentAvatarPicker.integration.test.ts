import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'

// Integration test against the REAL @dicebear/core + style JSON.
//
// The sibling unit test mocks loadAvatarKit with a plain object, which cannot
// reproduce the class-private-field proxy bug: DiceBear's `Style`/`Avatar` use
// `#private` fields, and wrapping them in a deep reactive proxy throws
// "Cannot read private member from an object whose class did not declare it".
// This test renders every tile for real so that regression is caught.
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/composables/useBackHandler', () => ({
  registerBackHandler: () => () => {},
  PRIORITY_OVERLAY: 100,
}))

import AgentAvatarPicker from '@/components/settings/AgentAvatarPicker.vue'
import { AVATAR_STYLES } from '@/utils/lazyAvatar'

let wrappers: VueWrapper[] = []

afterEach(() => {
  for (const w of wrappers) w.unmount()
  wrappers = []
})

describe('AgentAvatarPicker (real DiceBear)', () => {
  it('renders a non-empty data-URI for every style tile', async () => {
    const w = mount(AgentAvatarPicker, {
      props: { open: true, agentName: 'CodeBuddy' },
      global: { stubs: { Teleport: true } },
    })
    wrappers.push(w)

    // Wait for all 40 style chunks to land.
    await vi.waitFor(() => {
      expect(w.findAll('.avatar-picker__tile').length).toBe(AVATAR_STYLES.length)
    }, { timeout: 20000, interval: 100 })
    await flushPromises()

    const imgs = w.findAll('.avatar-picker__tile-img')
    const srcs = imgs.map(i => i.attributes('src') || '')
    expect(srcs.every(s => s.startsWith('data:image/svg+xml;charset=utf-8,'))).toBe(true)
    // No tile silently failed to render (which is what the proxy bug produced).
    expect(srcs.filter(s => s.length > 0).length).toBe(AVATAR_STYLES.length)
  }, 30000)

  it('changing the seed re-renders every tile', async () => {
    const w = mount(AgentAvatarPicker, {
      props: { open: true, agentName: 'CodeBuddy' },
      global: { stubs: { Teleport: true } },
    })
    wrappers.push(w)

    await vi.waitFor(() => {
      expect(w.findAll('.avatar-picker__tile-img').filter(i => (i.attributes('src') || '').length > 0).length)
        .toBe(AVATAR_STYLES.length)
    }, { timeout: 20000, interval: 100 })

    const before = w.findAll('.avatar-picker__tile-img').map(i => i.attributes('src'))
    await w.find('.avatar-picker__input').setValue('AnotherSeed')
    await flushPromises()
    const after = w.findAll('.avatar-picker__tile-img').map(i => i.attributes('src'))

    // Every tile must differ — a change to the seed is global, not per-tile.
    const changed = after.filter((s, i) => s !== before[i]).length
    expect(changed).toBe(AVATAR_STYLES.length)
  }, 30000)
})
