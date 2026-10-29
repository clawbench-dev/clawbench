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

  // The capsule's word must name WHAT the rule counts. A fixed "Usage" made a
  // $20 budget and a 20-token budget read identically, so the word carried no
  // information the number did not already carry.
  it('labels the capsule with the configured metric', () => {
    expect(mountPopover(status({ metric: 'cost' }), 'ok').get('.usage-label').text())
      .toBe('usageMeter.metricCost')
    expect(mountPopover(status({ metric: 'tokens' }), 'ok').get('.usage-label').text())
      .toBe('usageMeter.metricTokens')
    expect(mountPopover(status({ metric: 'credit' }), 'ok').get('.usage-label').text())
      .toBe('usageMeter.metricCredit')
  })

  it('falls back to the feature label when the payload carries no metric', () => {
    // An older frame omits `metric`; the capsule must still say something
    // rather than render an empty word.
    expect(mountPopover(status({ metric: undefined }), 'ok').get('.usage-label').text())
      .toBe('usageMeter.label')
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
    expect(wrapper.find('.um-link').exists()).toBe(false)
    expect(wrapper.get('[data-action="usage-meter-settings"]').exists()).toBe(true)
  })

  // The header gear is the popover's only route to the rule's settings (the
  // foot-of-panel text link was removed so the action is not repeated). Without
  // this the icon could be decorative and every other assertion would pass.
  it('opens the usage settings from the header gear', async () => {
    mockSetPendingSettingsCategory.mockClear()
    mockSwitchTab.mockClear()

    const wrapper = mountPopover(status(), 'over')
    await wrapper.get('[data-action="usage-meter-settings"]').trigger('click')

    expect(mockSetPendingSettingsCategory).toHaveBeenCalledWith('usageMeter')
    expect(mockSwitchTab).toHaveBeenCalledWith('settings')
  })
})

// ── Partial rules: show the largest readout the configuration supports ──────
// Every field is optional, and an unset one means INFINITE, not absent: no quota
// is an unlimited budget, no period is a cycle that never rolls over. The
// popover therefore keeps the full readout shape and shows ∞ where the user
// never set a bound — dropping the row instead made the panel change shape
// depending on configuration, and read as "measurement missing".
describe('UsageMeterPopover — optional quota and cycle', () => {
  const INFINITE = '\u221e'

  function withoutQuota(): UsageMeterStatus {
    return status({
      quota: 0,
      expected: 0,
      projectedExhaustDay: 0,
      series: Array.from({ length: 10 }, (_, i) => ({
        day: `2026-10-${String(i + 1).padStart(2, '0')}`,
        actual: (i + 1) * 1.5,
        expected: 0,
      })),
    })
  }

  function withoutCycle(): UsageMeterStatus {
    return status({
      periodDays: 0,
      cycleEnd: undefined,
      expected: 0,
      remainingDays: 0,
      projectedExhaustDay: 0,
    })
  }

  it('keeps the capsule and shows ∞ instead of a quota', () => {
    const wrapper = mountPopover(withoutQuota(), 'ok')

    expect(wrapper.find('[data-action="usage-meter"]').exists()).toBe(true)
    expect(wrapper.find('.usage-used').exists()).toBe(true)
    expect(wrapper.get('.usage-quota').text()).toContain(INFINITE)
    // The bar has no meaning against an unlimited budget: a track filled to 0%
    // would read as "you have used none of nothing".
    expect(wrapper.find('.usage-bar').exists()).toBe(false)
  })

  it('does not claim "on plan" when there is no plan to be on', () => {
    const wrapper = mountPopover(withoutQuota(), 'ok')
    const pop = wrapper.get('.um-pop')
    expect(pop.text()).toContain('usageMeter.badgeNoQuota')
    expect(pop.text()).not.toContain('usageMeter.badgeOk')
  })

  it('keeps every comparison row, showing ∞ for the unbounded ones', () => {
    const wrapper = mountPopover(withoutQuota(), 'ok')
    const rows = wrapper.findAll('.um-row')
    expect(rows).toHaveLength(3)

    const [actualRow, expectedRow, quotaRow] = rows
    expect(actualRow.text()).toContain('usageMeter.actual')
    expect(expectedRow.text()).toContain('usageMeter.expected')
    expect(expectedRow.text()).toContain(INFINITE)
    expect(quotaRow.text()).toContain('usageMeter.quota')
    expect(quotaRow.text()).toContain(INFINITE)
  })

  it('keeps every stat, showing ∞ for the budget-derived ones', () => {
    const wrapper = mountPopover(withoutQuota(), 'ok')
    const labels = wrapper.findAll('.um-stat-label').map(n => n.text())
    expect(labels).toEqual([
      'usageMeter.remaining',
      'usageMeter.remainingDays',
      'usageMeter.projected',
      'usageMeter.dailyLeft',
    ])

    const values = wrapper.findAll('.um-stat-value').map(n => n.text())
    // remaining and daily-left are infinite; the projection says it never runs
    // out (which is the accurate reading of an unlimited budget).
    expect(values[0]).toContain(INFINITE)
    expect(values[2]).toContain('usageMeter.projectedNone')
    expect(values[3]).toContain(INFINITE)
  })

  it('still draws the actual curve without a quota, but no baseline', () => {
    const wrapper = mountPopover(withoutQuota(), 'ok')

    expect(wrapper.find('.um-actual-line').exists()).toBe(true)
    expect(wrapper.find('.um-expected-line').exists()).toBe(false)
    expect(wrapper.find('.um-quota-line').exists()).toBe(false)
    expect(wrapper.find('.um-projection').exists()).toBe(false)
  })

  it('counts days against ∞ when there is no cycle', () => {
    const wrapper = mountPopover(withoutCycle(), 'ok')
    const period = wrapper.get('.um-period').text()

    // The day counter stays (it is real information); only its denominator
    // becomes infinite.
    expect(period).toContain('usageMeter.dayOfCycle')
    expect(period).toContain('usageMeter.sinceDate')
  })

  it('falls back to "all time" when neither a cycle nor an anchor exists', () => {
    const wrapper = mountPopover(
      status({ periodDays: 0, cycleStart: undefined, cycleEnd: undefined, dayInCycle: 0, expected: 0, remainingDays: 0 }),
      'ok',
    )
    expect(wrapper.get('.um-period').text()).toContain('usageMeter.allTime')
  })

  it('does not claim "on plan" when there is a quota but no cycle', () => {
    // A budget with no time axis has no "by now" to compare against, so the
    // baseline never exists and "on plan" would be an unsupported claim.
    const wrapper = mountPopover(withoutCycle(), 'ok')
    const pop = wrapper.get('.um-pop')
    expect(pop.text()).toContain('usageMeter.badgeNoCycle')
    expect(pop.text()).not.toContain('usageMeter.badgeOk')
  })

  it('keeps the quota row but shows ∞ days-left without a cycle', () => {
    const wrapper = mountPopover(withoutCycle(), 'ok')

    const quotaRow = wrapper.findAll('.um-row')[2]
    expect(quotaRow.text()).toContain('30')

    const labels = wrapper.findAll('.um-stat-label').map(n => n.text())
    const values = wrapper.findAll('.um-stat-value').map(n => n.text())
    const daysLeft = values[labels.indexOf('usageMeter.remainingDays')]
    expect(daysLeft).toContain(INFINITE)
  })
})
