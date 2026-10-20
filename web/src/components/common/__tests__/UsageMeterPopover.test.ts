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

function mountPopover(s: UsageMeterStatus) {
  return mount(UsageMeterPopover, {
    props: { status: s, state: 'over', fillPercent: 50, expectedPercent: 33 },
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
