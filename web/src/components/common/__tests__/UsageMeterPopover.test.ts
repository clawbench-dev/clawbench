import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent } from 'vue'
import UsageMeterPopover from '@/components/common/UsageMeterPopover.vue'
import type { UsageMeterStatus } from '@/composables/useUsageMeter'

// PopupMenu teleports and gates its body on `show`; the chart is what this test
// observes, so render the slot unconditionally.
vi.mock('@/components/common/PopupMenu.vue', () => ({
  default: defineComponent({
    props: ['show'],
    template: '<div class="popup-stub"><slot /></div>',
  }),
}))

vi.mock('@/composables/useUsageMeter', () => ({
  useUsageMeter: () => ({ refresh: vi.fn(), openRequest: { value: 0 } }),
}))

const mockSetPendingSettingsCategory = vi.fn()
vi.mock('@/composables/useSettingsNavigation', () => ({
  setPendingSettingsCategory: (id: string) => mockSetPendingSettingsCategory(id),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: { zh: { usageMeter: {} } },
  // Missing keys fall back to the key itself, which is irrelevant here.
  missingWarn: false,
  fallbackWarn: false,
})

const PLOT_LEFT = 8
const PLOT_RIGHT = 280

function status(overrides: Partial<UsageMeterStatus> = {}): UsageMeterStatus {
  return {
    enabled: true,
    metric: 'cost',
    anchorDate: '2026-10-01',
    periodDays: 30,
    quota: 30,
    cycleStart: '2026-10-01T00:00:00+08:00',
    cycleEnd: '2026-10-31T00:00:00+08:00',
    dayInCycle: 10,
    actual: 15,
    expected: 10,
    remainingDays: 20,
    projectedExhaustDay: 20,
    series: Array.from({ length: 10 }, (_, i) => ({
      day: `2026-10-${String(i + 1).padStart(2, '0')}`,
      actual: (i + 1) * 1.5,
      expected: i + 1,
    })),
    ...overrides,
  }
}

const mockSwitchTab = vi.fn()

function mountPopover(s: UsageMeterStatus, state: 'ok' | 'over' | 'full' | 'off' = 'over') {
  return mount(UsageMeterPopover, {
    props: { status: s, state, fillPercent: 50, expectedPercent: 33 },
    global: {
      plugins: [i18n],
      // App.vue provides this; the settings CTA needs it to navigate.
      provide: { switchTab: mockSwitchTab },
    },
  })
}

function parsePoints(raw: string | undefined): Array<[number, number]> {
  return (raw ?? '')
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map((pair) => {
      const [x, y] = pair.split(',').map(Number)
      return [x, y] as [number, number]
    })
}

describe('UsageMeterPopover — baseline spans the whole cycle', () => {
  it('draws the expected line to the cycle end, not just to today', () => {
    // The baseline is the plan for the whole cycle. Stopping it at today made
    // the dashed line look like another measurement that had run out.
    const wrapper = mountPopover(status())
    const expected = parsePoints(wrapper.get('.um-expected-line').attributes('points'))

    expect(expected).toHaveLength(30)
    expect(expected[0][0]).toBeCloseTo(PLOT_LEFT, 1)
    expect(expected[expected.length - 1][0]).toBeCloseTo(PLOT_RIGHT, 1)
  })

  it('the actual line still stops at today', () => {
    const wrapper = mountPopover(status())
    const actual = parsePoints(wrapper.get('.um-actual-line').attributes('points'))
    const expected = parsePoints(wrapper.get('.um-expected-line').attributes('points'))

    expect(actual).toHaveLength(10)
    expect(actual[actual.length - 1][0]).toBeLessThan(expected[expected.length - 1][0])
  })

  it('baseline reaches the quota at the cycle end and matches the server series where they overlap', () => {
    // Day 1 = quota/periodDays, day 30 = quota. The series' `expected` values
    // use the same formula, so the two must agree point by point.
    const s = status()
    const wrapper = mountPopover(s)
    const expected = parsePoints(wrapper.get('.um-expected-line').attributes('points'))

    // y axis: [PLOT_TOP=8, PLOT_BOTTOM=82] mapped over [0, max(quota, peak)]
    const yMax = Math.max(s.quota!, 15)
    const yOf = (v: number) => 82 - (v / yMax) * (82 - 8)

    expect(expected[0][1]).toBeCloseTo(yOf(s.quota! / 30), 1)
    expect(expected[29][1]).toBeCloseTo(yOf(s.quota!), 1)

    for (let i = 0; i < s.series!.length; i++) {
      expect(expected[i][1]).toBeCloseTo(yOf(s.series![i].expected), 1)
    }
  })
})

// ── No rule configured: placeholder, not an empty readout ────────────────────
// Every figure in the popover is derived from a quota, so with no rule there is
// nothing to show behind it. The component renders a single placeholder entry
// that names the feature and leads to the settings page, instead of a gauge
// whose numbers are all zero.
describe('UsageMeterPopover — no rule configured', () => {
  function unmetered(overrides: Partial<UsageMeterStatus> = {}): UsageMeterStatus {
    return status({
      enabled: false,
      quota: 0,
      anchorDate: undefined,
      dayInCycle: 0,
      actual: 0,
      expected: 0,
      remainingDays: 0,
      projectedExhaustDay: 0,
      series: undefined,
      ...overrides,
    })
  }

  it('renders a placeholder entry instead of the capsule', () => {
    const wrapper = mountPopover(unmetered(), 'off')

    expect(wrapper.find('.usage-pill--placeholder').exists()).toBe(true)
    // The real capsule is gone, not merely emptied: it would offer a popover
    // whose every figure is zero.
    expect(wrapper.find('[data-action="usage-meter"]').exists()).toBe(false)
  })

  it('names the feature and the action in the placeholder label', () => {
    const wrapper = mountPopover(unmetered(), 'off')
    const pill = wrapper.get('.usage-pill--placeholder')

    expect(pill.text()).toBe('usageMeter.setupHint')
    // The label IS the message here, so it must not fall back to the bare
    // feature name ("usageMeter.label").
    expect(pill.text()).not.toBe('usageMeter.label')
    // Accessible name matches, since the visible text is the only affordance.
    expect(pill.attributes('aria-label')).toBe('usageMeter.setupHint')
  })

  it('does not render any numbers, bar, badge or chart', () => {
    const wrapper = mountPopover(unmetered(), 'off')

    expect(wrapper.find('.usage-bar').exists()).toBe(false)
    expect(wrapper.find('.usage-quota').exists()).toBe(false)
    expect(wrapper.find('.usage-used').exists()).toBe(false)
    expect(wrapper.find('.um-pop').exists()).toBe(false)
    expect(wrapper.find('.um-spark').exists()).toBe(false)
  })

  it('uses a distinct action hook so the placeholder is not the capsule', () => {
    const wrapper = mountPopover(unmetered(), 'off')
    expect(wrapper.get('.usage-pill--placeholder').attributes('data-action'))
      .toBe('usage-meter-setup')
  })

  // The placeholder's whole purpose is to lead somewhere. Without this the
  // button could be inert and every other assertion would still pass.
  it('opens the usage settings when the placeholder is clicked', async () => {
    mockSetPendingSettingsCategory.mockClear()
    mockSwitchTab.mockClear()

    const wrapper = mountPopover(unmetered(), 'off')
    await wrapper.find('.usage-pill--placeholder').trigger('click')

    expect(mockSetPendingSettingsCategory).toHaveBeenCalledWith('usageMeter')
    expect(mockSwitchTab).toHaveBeenCalledWith('settings')
  })

  // The mirror: with a rule the capsule and its popover must be back. Without
  // this, deleting the whole body would also satisfy every assertion above.
  it('renders the full capsule and popover once a rule exists', () => {
    const wrapper = mountPopover(status(), 'over')

    expect(wrapper.find('.usage-pill--placeholder').exists()).toBe(false)
    expect(wrapper.find('[data-action="usage-meter"]').exists()).toBe(true)
    expect(wrapper.find('.usage-bar').exists()).toBe(true)
    expect(wrapper.find('.usage-quota').exists()).toBe(true)
    expect(wrapper.find('.um-pop').exists()).toBe(true)
    expect(wrapper.find('.um-compare').exists()).toBe(true)
    expect(wrapper.find('.um-stats').exists()).toBe(true)
    expect(wrapper.get('.um-link').text()).toBe('usageMeter.adjust')
  })
})
