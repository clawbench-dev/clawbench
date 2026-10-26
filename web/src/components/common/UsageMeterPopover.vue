<template>
  <!-- Placeholder: no rule configured.
       A bare "设置用量提醒" entry instead of a gauge with no numbers. Showing
       the capsule in its empty form would raise the question "why is it empty",
       and the popover behind it would have nothing to say — every figure in it
       is derived from a quota. A single tappable hint answers both: there is
       nothing here yet, and here is where to create it. -->
  <button
    v-if="!status.enabled"
    type="button"
    class="usage-pill is-off usage-pill--placeholder"
    :title="t('usageMeter.setupHint')"
    :aria-label="t('usageMeter.setupHint')"
    data-action="usage-meter-setup"
    @click.stop="openSettings"
  >
    <Gauge :size="13" class="usage-icon" aria-hidden="true" />
    <span class="usage-label">{{ t('usageMeter.setupHint') }}</span>
  </button>

  <div v-else class="usage-meter-wrap">
    <button
      ref="btnRef"
      type="button"
      class="usage-pill"
      :class="`is-${state}`"
      :title="t('usageMeter.capsuleTitle')"
      :aria-label="t('usageMeter.capsuleTitle')"
      :aria-expanded="menuOpen"
      data-action="usage-meter"
      @click.stop="toggle"
    >
      <!-- Icon + word, like the recent-project rows: the bar alone does not say
           WHAT is being measured, and a bare pair of numbers in the header is
           ambiguous next to the file/project badges. -->
      <Gauge :size="13" class="usage-icon" aria-hidden="true" />
      <span class="usage-label">{{ t('usageMeter.label') }}</span>
      <span class="usage-bar" aria-hidden="true">
        <span class="usage-fill" :style="{ width: fillPercent + '%' }"></span>
        <!-- Baseline tick: where a linear spend would be right now. Reading the
             fill against this tick is the whole point of the capsule. -->
        <span class="usage-mark" :style="{ left: expectedPercent + '%' }"></span>
        <span v-if="state === 'full'" class="usage-cap"></span>
      </span>
      <span class="usage-text">
        <span class="usage-used">{{ fmtCompact(actual) }}</span>
        <span class="usage-quota">/ {{ fmtCompact(quota) }}</span>
      </span>
    </button>

    <PopupMenu
      v-model:show="menuOpen"
      :target-element="btnRef"
      :max-width="320"
      :max-height="460"
      :menu-items-count="14"
      anchor="right"
      app-surface
      @after-leave="onAfterLeave"
    >
      <div class="um-pop">
        <div class="um-head">
          <span class="um-title">{{ t('usageMeter.title') }}</span>
          <div class="um-head-actions">
            <span class="um-badge" :class="`is-${state}`">{{ badgeText }}</span>
            <!-- Shortcut into the rule that produces every number below. Lives
                 in the header rather than at the foot of the panel: the figures
                 are long and the action was below the fold on short viewports. -->
            <button
              type="button"
              class="um-settings"
              :title="t('usageMeter.openSettings')"
              :aria-label="t('usageMeter.openSettings')"
              data-action="usage-meter-settings"
              @click="openSettings"
            >
              <Settings2 :size="14" aria-hidden="true" />
            </button>
          </div>
        </div>

        <div class="um-period">
          {{ t('usageMeter.dayOfCycle', { day: status.dayInCycle, days: status.periodDays ?? 0 }) }}
          <span class="um-sep">·</span>
          <span class="um-dates">{{ dateRange }}</span>
        </div>

        <!-- Three bars on one axis so their lengths are directly comparable. -->
        <div class="um-compare">
          <div class="um-row">
            <span class="um-row-name">{{ t('usageMeter.actual') }}</span>
            <span class="um-track">
              <span class="um-bar" :class="`is-${state}`" :style="{ width: fillPercent + '%' }"></span>
            </span>
            <span class="um-row-val" :class="`is-${state}`">{{ fmt(actual) }}</span>
          </div>
          <div class="um-row">
            <span class="um-row-name">{{ t('usageMeter.expected') }}</span>
            <span class="um-track">
              <span class="um-bar is-expected" :style="{ width: expectedPercent + '%' }"></span>
            </span>
            <span class="um-row-val">{{ fmt(expected) }}</span>
          </div>
          <div class="um-row">
            <span class="um-row-name">{{ t('usageMeter.quota') }}</span>
            <span class="um-track">
              <span class="um-bar is-quota"></span>
            </span>
            <span class="um-row-val is-muted">{{ fmt(quota) }}</span>
          </div>
        </div>

        <!-- Cumulative curve. Hand-drawn SVG rather than ECharts: this sits on
             the first-paint path and the chart library is a ~300KB dependency
             loaded lazily by the stats tab. -->
        <svg
          v-if="points.length > 1"
          class="um-spark"
          viewBox="0 0 288 90"
          preserveAspectRatio="none"
          role="img"
          :aria-label="t('usageMeter.trendLabel')"
        >
          <line x1="8" :y1="PLOT_BOTTOM" x2="280" :y2="PLOT_BOTTOM" class="um-axis" />
          <line x1="8" :y1="PLOT_TOP" x2="280" :y2="PLOT_TOP" class="um-quota-line" />
          <polyline :points="expectedLine" class="um-expected-line" />
          <polyline :points="actualLine" class="um-actual-line" :class="`is-${state}`" />
          <line
            v-if="projection"
            :x1="projection.x1" :y1="projection.y1" :x2="projection.x2" :y2="projection.y2"
            class="um-projection"
          />
          <line :x1="todayX" :y1="PLOT_TOP" :x2="todayX" :y2="PLOT_BOTTOM" class="um-today" />
          <circle :cx="todayX" :cy="todayY" r="2.5" class="um-today-dot" />
        </svg>
        <div v-if="points.length > 1" class="um-legend">
          <span><i class="um-dot is-actual"></i>{{ t('usageMeter.legendActual') }}</span>
          <span><i class="um-dot is-expected"></i>{{ t('usageMeter.legendExpected') }}</span>
          <span><i class="um-dot is-today"></i>{{ t('usageMeter.legendToday') }}</span>
        </div>

        <div class="um-stats">
          <div>
            <div class="um-stat-label">{{ t('usageMeter.remaining') }}</div>
            <div class="um-stat-value">{{ fmt(remaining) }}</div>
          </div>
          <div>
            <div class="um-stat-label">{{ t('usageMeter.remainingDays') }}</div>
            <div class="um-stat-value">{{ t('usageMeter.days', { n: status.remainingDays }) }}</div>
          </div>
          <div>
            <div class="um-stat-label">{{ t('usageMeter.projected') }}</div>
            <div class="um-stat-value" :class="{ 'is-over': state !== 'ok' }">
              {{ projectionText }}
            </div>
          </div>
          <div>
            <div class="um-stat-label">{{ t('usageMeter.dailyLeft') }}</div>
            <div class="um-stat-value">{{ fmt(dailyLeft) }}</div>
          </div>
        </div>
      </div>
    </PopupMenu>
  </div>
</template>

<script setup lang="ts">
import { computed, inject, ref, watch } from 'vue'
import { Gauge, Settings2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import PopupMenu from '@/components/common/PopupMenu.vue'
import { formatUsageMeterValue } from '@/utils/usageFormat'
import { setPendingSettingsCategory } from '@/composables/useSettingsNavigation'
import { useUsageMeter, type UsageMeterStatus } from '@/composables/useUsageMeter'

const props = defineProps<{
  status: UsageMeterStatus
  // 'off' never reaches the popover body: the placeholder branch renders
  // instead. It stays in the union because the caller passes meterState
  // directly, and narrowing it there would need a cast for no benefit.
  state: 'ok' | 'over' | 'full' | 'off'
  fillPercent: number
  expectedPercent: number
}>()

const { t } = useI18n()
const { refresh, openRequest } = useUsageMeter()
// Provided by App.vue — same inject AppHeader uses to reach the settings tab.
const switchTab = inject<(tab: string) => void>('switchTab')

const btnRef = ref<HTMLElement | null>(null)
const menuOpen = ref(false)

// Plot geometry. Kept in script so the template does not repeat magic numbers.
const PLOT_TOP = 8
const PLOT_BOTTOM = 82

const metric = computed(() => props.status.metric ?? 'cost')
const actual = computed(() => props.status.actual ?? 0)
const expected = computed(() => props.status.expected ?? 0)
const quota = computed(() => props.status.quota ?? 0)
const remaining = computed(() => Math.max(0, quota.value - actual.value))
const dailyLeft = computed(() => {
  const days = props.status.remainingDays
  if (days <= 0) return remaining.value
  return remaining.value / days
})
const points = computed(() => props.status.series ?? [])

function fmt(v: number): string {
  return formatUsageMeterValue(metric.value, v, { compact: false })
}

// The capsule has a fixed width budget: K/M/B with one decimal instead of the
// full amount. The popover body keeps the exact figure (see `fmt`).
function fmtCompact(v: number): string {
  return formatUsageMeterValue(metric.value, v, { compact: true })
}

const badgeText = computed(() => {
  if (props.state === 'full') return t('usageMeter.badgeFull')
  if (props.state === 'over') {
    const pct = expected.value > 0 ? Math.round(((actual.value - expected.value) / expected.value) * 100) : 0
    return t('usageMeter.badgeOver', { pct })
  }
  return t('usageMeter.badgeOk')
})

const dateRange = computed(() => {
  const s = props.status.cycleStart
  const e = props.status.cycleEnd
  if (!s || !e) return ''
  // Date-only: the time of day is noise for a multi-week cycle, and rendering
  // the raw RFC3339 string would show the UTC offset the user never chose.
  return `${s.slice(0, 10)} → ${e.slice(0, 10)}`
})

const projectionText = computed(() => {
  const day = props.status.projectedExhaustDay
  if (props.state === 'full') return t('usageMeter.projectedDone')
  if (!day) return t('usageMeter.projectedNone')
  return t('usageMeter.projectedDay', { day })
})

// ── Chart geometry ───────────────────────────────────────────────────────────
// x spans the WHOLE cycle, not just the elapsed days: anchoring today at the
// right edge left no room for the projection, so the extrapolation collapsed
// onto the "today" marker and was invisible. y spans [0, max(quota, peak)] so
// the quota line is always on screen — and with no quota it spans the peak
// alone, so the curve is not squashed flat against an axis whose top is 0.
const axisDays = computed(() => Math.max(1, props.status.periodDays ?? points.value.length))
const yMax = computed(() => {
  const peak = points.value.reduce((m, p) => Math.max(m, p.actual, p.expected), 0)
  return Math.max(quota.value, peak, 1)
})

function xAt(index: number): number {
  return 8 + (272 * index) / Math.max(1, axisDays.value - 1)
}
function yAt(value: number): number {
  const h = PLOT_BOTTOM - PLOT_TOP
  return PLOT_BOTTOM - (value / yMax.value) * h
}

const actualLine = computed(() =>
  points.value.map((p, i) => `${xAt(i).toFixed(1)},${yAt(p.actual).toFixed(1)}`).join(' '),
)
// The baseline is a property of the WHOLE cycle, so it is drawn all the way to
// the cycle end even though the actual curve stops at today. Deriving it from
// the series instead made the dashed line stop at today too, which read as a
// second measurement that had run out rather than as the plan being compared
// against. Same per-day slope the server uses for `point.expected`
// (quota / periodDays, counted from day 1), so the two agree where they overlap.
const expectedLine = computed(() => {
  const days = axisDays.value
  if (quota.value <= 0) return ''
  const perDay = quota.value / days
  const coords: string[] = []
  for (let i = 0; i < days; i++) {
    coords.push(`${xAt(i).toFixed(1)},${yAt(perDay * (i + 1)).toFixed(1)}`)
  }
  return coords.join(' ')
})
const todayX = computed(() => xAt(Math.max(0, points.value.length - 1)))
const todayY = computed(() => {
  const last = points.value[points.value.length - 1]
  return last ? yAt(last.actual) : PLOT_BOTTOM
})

/**
 * Extrapolation from today to the cycle end at the current daily rate.
 * Omitted when the pace is zero (a flat line adds nothing) or when the
 * projection would run past the quota line's right edge.
 */
const projection = computed(() => {
  const n = points.value.length
  // The projection predicts the day the QUOTA runs out; with no quota there is
  // no such day, so the dashed extrapolation would be a line to nowhere.
  if (n < 2) return null
  const last = points.value[n - 1]
  const first = points.value[0]
  const elapsed = Math.max(1, n - 1)
  const perDay = (last.actual - first.actual) / elapsed
  if (perDay <= 0) return null
  const totalDays = axisDays.value
  const remainingDays = Math.max(0, totalDays - n + 1)
  if (remainingDays <= 0) return null
  const projectedValue = Math.min(yMax.value, last.actual + perDay * remainingDays)
  const x2 = xAt(n - 1 + remainingDays)
  if (x2 <= todayX.value) return null
  return {
    x1: todayX.value.toFixed(1),
    y1: todayY.value.toFixed(1),
    x2: Math.min(280, x2).toFixed(1),
    y2: yAt(projectedValue).toFixed(1),
  }
})

// Notification tap / notification-center row: open the popover even if it is
// already open (a second tap should not close it — the user asked to SEE it).
watch(openRequest, (n) => {
  if (n === 0) return
  menuOpen.value = true
  void refresh()
})

function toggle() {
  menuOpen.value = !menuOpen.value
  // Refetch on open: the WS event carries the headline numbers but not the
  // series, and a user opening the popover wants the curve up to date.
  if (menuOpen.value) void refresh()
}

function onAfterLeave() {
  // no-op: kept so PopupMenu's after-leave contract has a handler if the
  // popover ever needs transition cleanup
}

function openSettings() {
  menuOpen.value = false
  setPendingSettingsCategory('usageMeter')
  switchTab?.('settings')
}
</script>

<style scoped>
.usage-meter-wrap {
  position: relative;
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

/* ── Capsule ─────────────────────────────────────────────────────────────── */
.usage-pill {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  height: calc(24 * var(--dp));
  padding: 0 var(--space-5) 0 var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-full);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font: inherit;
  flex-shrink: 0;
  transition: background var(--duration-base), border-color var(--duration-base);
}

@media (hover: hover) {
  .usage-pill:hover {
    background: var(--bg-tertiary);
    border-color: var(--text-muted);
  }
}

.usage-pill:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 1px;
}

.usage-pill.is-ok { --meter-color: var(--color-green); }
.usage-pill.is-over { --meter-color: var(--color-orange); }
.usage-pill.is-full { --meter-color: var(--color-red); }
/* No rule: the accent is muted rather than green. Green would claim "you are
   within budget", which is a judgement this state has no basis for. */
.usage-pill.is-off { --meter-color: var(--text-muted); }

/* Placeholder entry. Dashed rather than solid: the pill is not a readout yet,
   it is a slot waiting to be filled, and the border says so before the label is
   read. Muted text keeps it from competing with real header controls. */
.usage-pill--placeholder {
  border-style: dashed;
  color: var(--text-muted);
}

.usage-pill--placeholder .usage-label {
  color: var(--text-muted);
}

.usage-bar {
  position: relative;
  display: block;
  width: calc(40 * var(--dp));
  height: calc(4 * var(--dp));
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  flex-shrink: 0;
}

.usage-fill {
  position: absolute;
  display: block;
  top: 0;
  bottom: 0;
  left: 0;
  border-radius: var(--radius-full);
  background: var(--meter-color);
  transition: width 0.3s ease, background var(--duration-base);
}

/* Baseline tick. The 1px outline is painted in the header's own background so
   the tick stays readable where it crosses the filled portion. */
.usage-mark {
  position: absolute;
  display: block;
  top: calc(-2 * var(--dp));
  bottom: calc(-2 * var(--dp));
  width: 1px;
  background: var(--text-primary);
  box-shadow: 0 0 0 1px var(--bg-secondary);
  border-radius: 1px;
}

/* End cap: only rendered once the quota is spent, so "full" is distinguishable
   from "exactly at 100% and still on plan". */
.usage-cap {
  position: absolute;
  display: block;
  right: 0;
  top: calc(-2 * var(--dp));
  bottom: calc(-2 * var(--dp));
  width: calc(2 * var(--dp));
  border-radius: 1px;
  background: var(--color-red);
}

.usage-icon {
  flex-shrink: 0;
  color: var(--meter-color, var(--text-hint));
}

.usage-label {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-hint);
  line-height: 1;
  white-space: nowrap;
}

.usage-text {
  display: inline-flex;
  align-items: baseline;
  gap: calc(3 * var(--dp));
  font-size: var(--font-size-xs);
  font-variant-numeric: tabular-nums;
  line-height: 1;
  white-space: nowrap;
}

.usage-used {
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}

.usage-pill.is-over .usage-used { color: var(--color-orange); }
.usage-pill.is-full .usage-used { color: var(--color-red); }

.usage-quota {
  color: var(--text-muted);
}

/* ── Popover ─────────────────────────────────────────────────────────────── */
.um-pop {
  padding: var(--space-6);
  font-size: var(--font-size-xs);
  color: var(--text-primary);
}

.um-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-3);
}

.um-head-actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-shrink: 0;
}

.um-settings {
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  padding: 0;
  border: none;
  border-radius: var(--radius-full);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  flex-shrink: 0;
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .um-settings:hover {
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }
}

.um-settings:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 1px;
}

.um-title {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
}

.um-badge {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  padding: 1px var(--space-3);
  border-radius: var(--radius-full);
  white-space: nowrap;
}

.um-badge.is-ok {
  background: color-mix(in srgb, var(--color-green) 16%, transparent);
  color: var(--color-green);
}
.um-badge.is-over {
  background: color-mix(in srgb, var(--color-orange) 16%, transparent);
  color: var(--color-orange);
}
.um-badge.is-full {
  background: color-mix(in srgb, var(--color-red) 16%, transparent);
  color: var(--color-red);
}

.um-period {
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
  margin-bottom: var(--space-6);
}

.um-sep {
  margin: 0 var(--space-2);
}

/* ── Comparison bars ─────────────────────────────────────────────────────── */
.um-compare {
  margin-bottom: var(--space-6);
}

.um-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  height: calc(18 * var(--dp));
  font-size: var(--font-size-2xs);
  font-variant-numeric: tabular-nums;
}

.um-row + .um-row {
  margin-top: var(--space-2);
}

.um-row-name {
  width: calc(34 * var(--dp));
  flex-shrink: 0;
  color: var(--text-muted);
}

.um-track {
  display: block;
  flex: 1;
  height: calc(8 * var(--dp));
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  overflow: hidden;
}

.um-bar {
  display: block;
  height: 100%;
  border-radius: var(--radius-full);
  transition: width 0.3s ease;
}

.um-bar.is-ok { background: var(--color-green); }
.um-bar.is-over { background: var(--color-orange); }
.um-bar.is-full { background: var(--color-red); }
.um-bar.is-expected { background: var(--text-muted); }
.um-bar.is-quota {
  width: 100%;
  background: color-mix(in srgb, var(--text-muted) 22%, transparent);
}

.um-row-val {
  width: calc(56 * var(--dp));
  flex-shrink: 0;
  text-align: right;
  font-weight: var(--font-weight-medium);
}

.um-row-val.is-over { color: var(--color-orange); }
.um-row-val.is-full { color: var(--color-red); }
.um-row-val.is-muted { color: var(--text-muted); }

/* ── Sparkline ───────────────────────────────────────────────────────────── */
.um-spark {
  display: block;
  width: 100%;
  height: auto;
  margin-bottom: var(--space-2);
}

.um-axis { stroke: var(--border-color); stroke-width: 1; }
.um-quota-line { stroke: var(--color-red); stroke-width: 1; opacity: 0.35; }
.um-expected-line {
  fill: none;
  stroke: var(--text-muted);
  stroke-width: 1;
  stroke-dasharray: 3 3;
  opacity: 0.7;
}
.um-actual-line {
  fill: none;
  stroke-width: 1.75;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.um-actual-line.is-ok { stroke: var(--color-green); }
.um-actual-line.is-over { stroke: var(--color-orange); }
.um-actual-line.is-full { stroke: var(--color-red); }
.um-projection {
  stroke: var(--color-orange);
  stroke-width: 1.25;
  stroke-dasharray: 3 3;
  opacity: 0.65;
}
.um-today { stroke: var(--border-color); stroke-width: 1; opacity: 0.8; }
.um-today-dot { stroke: var(--bg-elevated); stroke-width: 1.5; }
.um-actual-line.is-ok ~ .um-today-dot,
.um-spark .um-today-dot { fill: var(--color-orange); }

.um-legend {
  display: flex;
  gap: var(--space-6);
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
  margin-bottom: var(--space-6);
}

.um-legend span {
  display: inline-flex;
  align-items: center;
  gap: calc(4 * var(--dp));
}

.um-dot {
  display: inline-block;
  width: calc(6 * var(--dp));
  height: calc(6 * var(--dp));
  border-radius: 50%;
}

.um-dot.is-actual { background: var(--color-orange); }
.um-dot.is-expected { background: var(--text-muted); }
.um-dot.is-today { background: var(--border-color); }

/* ── Stats grid ──────────────────────────────────────────────────────────── */
.um-stats {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-4) var(--space-6);
  padding-top: var(--space-5);
  border-top: 1px solid var(--border-color);
  font-size: var(--font-size-2xs);
}

.um-stat-label {
  color: var(--text-muted);
  margin-bottom: calc(2 * var(--dp));
}

.um-stat-value {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  font-variant-numeric: tabular-nums;
}

.um-stat-value.is-over { color: var(--color-orange); }
</style>
