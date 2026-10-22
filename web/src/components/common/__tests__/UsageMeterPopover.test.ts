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

vi.mock('@/composables/useSettingsNavigation', () => ({
  setPendingSettingsCategory: vi.fn(),
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

function mountPopover(s: UsageMeterStatus, state: 'ok' | 'over' | 'full' | 'off' = 'over') {
  return mount(UsageMeterPopover, {
    props: { status: s, state, fillPercent: 50, expectedPercent: 33 },
    global: { plugins: [i18n] },
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

// ── No rule configured ───────────────────────────────────────────────────────
// The user asked for usage to stay visible without a budget; only the ratio
// parts may disappear. These pin each part individually — asserting "some
// element is missing" would pass even if the amount vanished too.
describe('UsageMeterPopover — no rule configured', () => {
  function unmetered(overrides: Partial<UsageMeterStatus> = {}): UsageMeterStatus {
    return status({
      enabled: false,
      quota: 0,
      anchorDate: undefined,
      dayInCycle: 0,
      actual: 42.5,
      expected: 0,
      remainingDays: 0,
      projectedExhaustDay: 0,
      cycleStart: '2026-09-22T00:00:00+08:00',
      cycleEnd: '2026-10-22T00:00:00+08:00',
      series: Array.from({ length: 5 }, (_, i) => ({
        day: `2026-10-${String(i + 18).padStart(2, '0')}`,
        actual: (i + 1) * 8,
        expected: 0,
      })),
      ...overrides,
    })
  }

  it('still shows the actual amount', () => {
    const wrapper = mountPopover(unmetered(), 'off')
    expect(wrapper.find('.usage-used').exists()).toBe(true)
    // The amount is the whole point of keeping the capsule visible.
    expect(wrapper.find('.usage-used').text()).not.toBe('')
  })

  it('hides the bar and the quota denominator in the capsule', () => {
    const wrapper = mountPopover(unmetered(), 'off')
    // An empty track plus "/ 0" would read as "your budget is zero", which is
    // the opposite of "you have not set one".
    expect(wrapper.find('.usage-bar').exists()).toBe(false)
    expect(wrapper.find('.usage-quota').exists()).toBe(false)
  })

  it('hides the badge, which is a verdict about a quota', () => {
    const wrapper = mountPopover(unmetered(), 'off')
    expect(wrapper.find('.um-badge').exists()).toBe(false)
  })

  it('hides the three comparison bars and shows a plain total instead', () => {
    const wrapper = mountPopover(unmetered(), 'off')
    expect(wrapper.find('.um-compare').exists()).toBe(false)
    expect(wrapper.find('.um-total').exists()).toBe(true)
    expect(wrapper.find('.um-total-value').text()).not.toBe('')
  })

  it('hides the whole derived stats grid', () => {
    const wrapper = mountPopover(unmetered(), 'off')
    // Remaining / days left / projected exhaustion / daily allowance are all
    // quota arithmetic.
    expect(wrapper.find('.um-stats').exists()).toBe(false)
  })

  it('drops the "day N of M" line but keeps the date range', () => {
    const wrapper = mountPopover(unmetered(), 'off')
    const period = wrapper.get('.um-period')
    // "day 0 of 30" would be a lie: there is no cycle.
    expect(period.text()).not.toContain('usageMeter.dayOfCycle')
    expect(period.find('.um-dates').text()).toBe('2026-09-22 → 2026-10-22')
  })

  it('omits the baseline line and quota line from the chart', () => {
    const wrapper = mountPopover(unmetered(), 'off')
    // Both are drawn from the quota; with none they would be a flat line on the
    // axis floor, reading as a measurement that had collapsed.
    expect(wrapper.find('.um-expected-line').exists()).toBe(false)
    expect(wrapper.find('.um-quota-line').exists()).toBe(false)
    expect(wrapper.find('.um-legend .is-expected').exists()).toBe(false)
  })

  it('still draws the actual curve', () => {
    const wrapper = mountPopover(unmetered(), 'off')
    const actual = parsePoints(wrapper.get('.um-actual-line').attributes('points'))
    expect(actual).toHaveLength(5)
  })

  it('omits the projection, which predicts a quota exhaustion that cannot happen', () => {
    const wrapper = mountPopover(unmetered(), 'off')
    expect(wrapper.find('.um-projection').exists()).toBe(false)
  })

  it('offers to set a budget rather than to adjust one', () => {
    const wrapper = mountPopover(unmetered(), 'off')
    expect(wrapper.get('.um-link').text()).toBe('usageMeter.setBudget')
  })

  // The mirror: with a rule, every one of those parts must still be there.
  // Without this the "hidden when off" assertions would also pass if the
  // elements were removed unconditionally.
  it('shows every ratio part again once a rule exists', () => {
    const wrapper = mountPopover(status(), 'over')
    expect(wrapper.find('.usage-bar').exists()).toBe(true)
    expect(wrapper.find('.usage-quota').exists()).toBe(true)
    expect(wrapper.find('.um-badge').exists()).toBe(true)
    expect(wrapper.find('.um-compare').exists()).toBe(true)
    expect(wrapper.find('.um-total').exists()).toBe(false)
    expect(wrapper.find('.um-stats').exists()).toBe(true)
    expect(wrapper.find('.um-expected-line').exists()).toBe(true)
    expect(wrapper.find('.um-quota-line').exists()).toBe(true)
    expect(wrapper.get('.um-link').text()).toBe('usageMeter.adjust')
  })
})
