import { computed, ref } from 'vue'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import type { UsageMeterMetric } from '@/utils/usageFormat'

/**
 * Usage-meter state — a module-level singleton.
 *
 * Why a singleton: the header capsule, the popover and the notification-center
 * click handler all render the same numbers. Independent instances would each
 * fetch and each apply WS updates, and a missed update in one of them would show
 * two different values on one screen.
 */

export interface UsageMeterPoint {
  day: string
  actual: number
  expected: number
}

export interface UsageMeterStatus {
  enabled: boolean
  metric?: UsageMeterMetric
  anchorDate?: string
  /** Length of the CURRENT cycle: in monthly mode this follows the calendar month. */
  periodDays?: number
  quota?: number
  cycleStart?: string
  cycleEnd?: string
  dayInCycle: number
  actual: number
  expected: number
  remainingDays: number
  projectedExhaustDay: number
  series?: UsageMeterPoint[]
}

const EMPTY: UsageMeterStatus = {
  enabled: false,
  dayInCycle: 0,
  actual: 0,
  expected: 0,
  remainingDays: 0,
  projectedExhaustDay: 0,
}

/**
 * Whether the meter has anything to render.
 *
 * Deliberately NOT `enabled`: a user without a budget still gets their spend
 * reported, so "should this render at all" and "is there a rule" are different
 * questions. `loaded` guards the first paint so the capsule does not flash for
 * users whose fetch has not returned yet.
 */
const hasData = computed(() => loaded.value)

const status = ref<UsageMeterStatus>(EMPTY)
const loading = ref(false)
const loaded = ref(false)

/**
 * Counter bumped by `requestOpen()`. The header capsule watches it, so a
 * notification tap can open the popover without App.vue holding a component
 * ref. A counter (not a boolean) so two consecutive taps both open it.
 */
const openRequest = ref(0)

let inflight: Promise<void> | null = null
let subscribed = false

/**
 * True when usage has passed the linear baseline.
 *
 * Requires `enabled`: with no rule there is no baseline, so `expected` is 0 and
 * `actual > 0` would report every user with any spend as "over plan".
 */
const isOverPlan = computed(
  () => status.value.enabled && status.value.actual > status.value.expected,
)

/** True when the whole quota is consumed. */
const isQuotaExhausted = computed(
  () =>
    status.value.enabled &&
    status.value.quota != null &&
    status.value.quota > 0 &&
    status.value.actual >= status.value.quota,
)

/**
 * Capsule state: drives colour and the end-cap marker.
 *
 * `off` means "no rule configured": the numbers are still shown, but nothing
 * ratio-derived (fill, baseline tick, badge) applies. It is a distinct state
 * rather than a reuse of `ok` because `ok` claims "you are on plan", which is a
 * statement about a quota the user never set.
 */
const meterState = computed<'ok' | 'over' | 'full' | 'off'>(() => {
  if (!status.value.enabled) return 'off'
  if (isQuotaExhausted.value) return 'full'
  if (isOverPlan.value) return 'over'
  return 'ok'
})

/** Fill width, clamped to 100% so an overshoot does not overflow the track. */
const fillPercent = computed(() => {
  const quota = status.value.quota ?? 0
  if (quota <= 0) return 0
  return Math.max(0, Math.min(100, (status.value.actual / quota) * 100))
})

/** Baseline marker position, clamped the same way. */
const expectedPercent = computed(() => {
  const quota = status.value.quota ?? 0
  if (quota <= 0) return 0
  return Math.max(0, Math.min(100, (status.value.expected / quota) * 100))
})

function fetchStatus(): Promise<void> {
  if (inflight) return inflight
  loading.value = true
  inflight = (async () => {
    try {
      const data = await apiGet<UsageMeterStatus>('/api/usage/meter')
      status.value = { ...EMPTY, ...data }
      loaded.value = true
    } catch (err) {
      // A failed fetch must not hide a capsule that was already loaded: the
      // meter is informational, and a transient network error should not look
      // like the user turned the rule off.
      appLog.w('UsageMeter', 'failed to load usage meter', err)
    } finally {
      loading.value = false
      inflight = null
    }
  })()
  return inflight
}

/**
 * Apply a `usage_meter_update` payload.
 *
 * The event carries the headline numbers, so the capsule moves immediately
 * without a round-trip. `series` is NOT in the payload — only the popover needs
 * it, and it refetches when opened, so the old series is left in place rather
 * than flashing empty.
 */
function applyServerUpdate(data: Record<string, unknown> | undefined): void {
  if (!data) return

  // A disabled frame means the rule was turned off. It carries no numbers on
  // purpose, so the whole readout must be dropped — patching only the fields
  // present would leave the previous quota and actual in place, and the capsule
  // would keep showing a budget that no longer exists.
  if (data.enabled === false) {
    status.value = { ...EMPTY }
    loaded.value = true
    return
  }

  const quota = typeof data.quota === 'number' ? data.quota : status.value.quota
  const actual = typeof data.actual === 'number' ? data.actual : status.value.actual
  const expected = typeof data.expected === 'number' ? data.expected : status.value.expected
  status.value = {
    ...status.value,
    // Only a frame that explicitly reports `enabled` can turn the meter on; an
    // older frame that omits the field must not silently resurrect a disabled
    // meter, so fall back to the current value rather than hard-coding true.
    enabled: typeof data.enabled === 'boolean' ? data.enabled : status.value.enabled,
    quota,
    actual,
    expected,
    metric: (data.metric as UsageMeterMetric) ?? status.value.metric,
  }
}

/**
 * Subscribe to WS updates once. Idempotent.
 *
 * Registered via DYNAMIC import, following the precedent in useAgents: a static
 * `useGlobalEvents` import closes a module cycle
 * (useUsageMeter → useGlobalEvents → useNotification → useSettingsConfig → …),
 * and calling `useGlobalEvents()` from module scope in a cycle hits the TDZ
 * error `Cannot access 'connected' before initialization`. A dynamic import's
 * promise resolves only after the target module has finished evaluating, which
 * breaks the cycle by construction. The `.catch` covers both a failed chunk load
 * and the partial `useGlobalEvents` mocks several tests install.
 */
function ensureSubscribed(): void {
  if (subscribed) return
  subscribed = true
  void import('@/composables/useGlobalEvents')
    .then(({ useGlobalEvents }) => {
      useGlobalEvents().onEvent((event, data) => {
        if (event !== 'usage_meter_update' && event !== 'usage_meter_tick') return
        // Both events carry the headline numbers, so the capsule moves without a
        // round-trip. The difference is intent, not shape: `_update` means a
        // threshold was crossed (every notification surface fires), `_tick`
        // means the numbers moved after a turn and only this capsule cares.
        applyServerUpdate(data as Record<string, unknown> | undefined)
        // Re-sync the derived fields (day counters, projection, series) the
        // event does not carry.
        void fetchStatus()
      })
    })
    .catch((err: unknown) => {
      appLog.e('UsageMeter', 'failed to subscribe to usage_meter_update', err)
    })
}

export function useUsageMeter() {
  ensureSubscribed()
  return {
    status,
    loading,
    loaded,
    hasData,
    openRequest,
    isOverPlan,
    isQuotaExhausted,
    meterState,
    fillPercent,
    expectedPercent,
    refresh: fetchStatus,
    requestOpen,
  }
}

/**
 * Ask the header capsule to open its popover.
 *
 * Used by the notification-center row and the Android notification tap. A no-op
 * on narrow screens — there is no capsule there, so the caller falls back to
 * opening the settings entry (see App.vue's handler).
 */
export function requestOpen(): void {
  openRequest.value += 1
}

/** Test-only: reset module state between cases. */
export function __resetUsageMeterForTest(): void {
  status.value = EMPTY
  loading.value = false
  loaded.value = false
  openRequest.value = 0
  inflight = null
  subscribed = false
}
