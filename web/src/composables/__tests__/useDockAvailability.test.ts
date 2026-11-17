import { beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, nextTick, ref } from 'vue'

/**
 * The three "off" axes, in one object.
 *
 * They are deliberately separate: the server's feature switch, the host's
 * capability and the user's per-device preference have different owners and
 * different remedies. A test that merged them would pass while the manager
 * started greying the wrong rows.
 */

const hidden = ref(new Set<string>())
const featureRows = ref<Record<string, { enabled: boolean; available: boolean }>>({})
const pluginAvailable = ref(true)

vi.mock('@/composables/useDockPlugins', () => ({
  useDockPlugins: () => ({ hidden: computed(() => hidden.value) }),
}))

vi.mock('@/composables/useFeatureFlags', () => ({
  isFeatureEnabled: (key: string) => {
    const id = key.split('.')[0]
    const row = featureRows.value[id]
    return row ? row.enabled : true
  },
  serverFeature: (id: string) => {
    const row = featureRows.value[id]
    return row ? { id, ...row, reason: '', defaultOn: true } : undefined
  },
}))

vi.mock('@/composables/dockPluginRuntime', () => ({
  DOCK_TABS: [
    { id: 'browse', titleKey: 'a', descriptionKey: 'b', group: 'workspace', closable: false },
    { id: 'forge', titleKey: 'a', descriptionKey: 'b', group: 'project', closable: true, feature: 'forge.enabled' },
    { id: 'terminal', titleKey: 'a', descriptionKey: 'b', group: 'project', closable: true, feature: 'terminal.enabled' },
    { id: 'mail', titleKey: 'a', descriptionKey: 'b', group: 'personal', closable: true, feature: 'mail.enabled' },
    { id: 'stats', titleKey: 'a', descriptionKey: 'b', group: 'project', closable: true },
  ],
  isDockPluginAvailable: () => pluginAvailable.value,
}))

import { isDockTabBlocked, useDockAvailability } from '@/composables/useDockAvailability'

beforeEach(() => {
  hidden.value = new Set()
  featureRows.value = {}
  pluginAvailable.value = true
})

describe('useDockAvailability', () => {
  it('is empty when everything is on and visible', () => {
    const { availability } = useDockAvailability()
    expect(availability.value.featureDisabled.size).toBe(0)
    expect(availability.value.unavailable.size).toBe(0)
    expect(availability.value.hidden.size).toBe(0)
  })

  it('puts a switched-off feature on the FEATURE axis, not the capability one', () => {
    featureRows.value = { forge: { enabled: false, available: true } }
    const { availability } = useDockAvailability()
    expect(availability.value.featureDisabled.has('forge')).toBe(true)
    expect(availability.value.unavailable.has('forge')).toBe(false)
  })

  it('puts a host that cannot run the feature on the capability axis', () => {
    pluginAvailable.value = false
    const { availability } = useDockAvailability()
    expect(availability.value.unavailable.size).toBeGreaterThan(0)
    expect(availability.value.featureDisabled.size).toBe(0)
  })

  it('honours the server\'s own "available" flag as capability', () => {
    featureRows.value = { terminal: { enabled: true, available: false } }
    const { availability } = useDockAvailability()
    expect(availability.value.unavailable.has('terminal')).toBe(true)
    expect(availability.value.featureDisabled.has('terminal')).toBe(false)
  })

  it('keeps the user preference on its own axis', () => {
    hidden.value = new Set(['mail'])
    const { availability } = useDockAvailability()
    expect(availability.value.hidden.has('mail')).toBe(true)
    expect(availability.value.featureDisabled.has('mail')).toBe(false)
    expect(availability.value.unavailable.has('mail')).toBe(false)
  })

  it('re-evaluates when the sources change', async () => {
    const { availability } = useDockAvailability()
    expect(availability.value.featureDisabled.has('forge')).toBe(false)

    featureRows.value = { forge: { enabled: false, available: true } }
    await nextTick()
    expect(availability.value.featureDisabled.has('forge')).toBe(true)
  })
})

describe('isDockTabBlocked', () => {
  it('is false for an unknown id', () => {
    const { availability } = useDockAvailability()
    expect(isDockTabBlocked('nope', availability.value)).toBe(false)
    expect(isDockTabBlocked('chat', availability.value)).toBe(false)
  })

  it('is true when any axis blocks the tab', () => {
    const { availability } = useDockAvailability()
    hidden.value = new Set(['mail'])
    expect(isDockTabBlocked('mail', availability.value)).toBe(true)
    expect(isDockTabBlocked('forge', availability.value)).toBe(false)
  })
})
