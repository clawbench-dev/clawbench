<template>
  <div class="system-resources-panel">
    <!-- Server info header -->
    <div class="server-info-header">
      <Server :size="13" class="server-info-icon" />
      <span class="server-info-address">{{ serverAddress }}</span>
    </div>
    <div class="header-divider"></div>
    <!-- Connection error / reconnecting state -->
    <div v-if="wsDisconnected" class="connection-status">
      <LoadingIndicator v-if="wsStatus === 'reconnecting'" size="md" inline class="connection-status-icon status-reconnecting" />
      <WifiOff v-else :size="28" class="connection-status-icon status-disconnected" />
      <span class="connection-status-text">{{ wsStatus === 'reconnecting' ? t('systemResources.reconnecting') : t('systemResources.disconnected') }}</span>
    </div>
    <!-- Resource rows (only when connected) -->
    <template v-else>
      <!-- Load Average -->
      <div class="resource-row">
        <div class="resource-header">
          <Activity :size="13" class="resource-icon" />
          <span class="resource-label">{{ t('systemResources.loadAvg') }}</span>
          <span class="resource-value">{{ resources.load.load1.toFixed(2) }}</span>
        </div>
        <div class="progress-bar">
          <div class="progress-fill" :style="{ width: loadBarPercent + '%' }" :class="getBarClass(loadBarPercent)"></div>
        </div>
      </div>
      <!-- CPU -->
      <div class="resource-row">
        <div class="resource-header">
          <Cpu :size="13" class="resource-icon" />
          <span class="resource-label">{{ t('systemResources.cpu') }}</span>
          <span class="resource-value">{{ cpuPercent }}%</span>
        </div>
        <div class="progress-bar">
          <div class="progress-fill" :style="{ width: cpuBarWidth + '%' }" :class="getBarClass(resources.cpu.percent)"></div>
        </div>
      </div>
      <!-- Memory -->
      <div class="resource-row">
        <div class="resource-header">
          <MemoryStick :size="13" class="resource-icon" />
          <span class="resource-label">{{ t('systemResources.memory') }}</span>
          <span class="resource-value">{{ formatBytes(resources.memory.used) }} / {{ formatBytes(resources.memory.total) }}</span>
        </div>
        <div class="progress-bar">
          <div class="progress-fill" :style="{ width: resources.memory.percent.toFixed(1) + '%' }" :class="getBarClass(resources.memory.percent)"></div>
        </div>
      </div>
      <!-- Disk -->
      <div class="resource-row">
        <div class="resource-header">
          <Database :size="13" class="resource-icon" />
          <span class="resource-label">{{ t('systemResources.disk') }}</span>
          <span class="resource-value">{{ formatBytes(resources.disk.used) }} / {{ formatBytes(resources.disk.total) }}</span>
        </div>
        <div class="progress-bar">
          <div class="progress-fill" :style="{ width: resources.disk.percent.toFixed(1) + '%' }" :class="getBarClass(resources.disk.percent)"></div>
        </div>
      </div>
      <!-- Disk Read -->
      <div class="resource-row">
        <div class="resource-header">
          <HardDriveDownload :size="13" class="resource-icon disk-read" />
          <span class="resource-label">{{ t('systemResources.diskRead') }}</span>
          <span class="resource-value">{{ formatRate(resources.disk_io.read_rate) }}</span>
        </div>
      </div>
      <!-- Disk Write -->
      <div class="resource-row">
        <div class="resource-header">
          <HardDriveUpload :size="13" class="resource-icon disk-write" />
          <span class="resource-label">{{ t('systemResources.diskWrite') }}</span>
          <span class="resource-value">{{ formatRate(resources.disk_io.write_rate) }}</span>
        </div>
      </div>
      <!-- Network Up -->
      <div class="resource-row">
        <div class="resource-header">
          <CloudUpload :size="13" class="resource-icon net-up" />
          <span class="resource-label">{{ t('systemResources.upload') }}</span>
          <span class="resource-value">{{ formatRate(resources.network.upload_rate) }}</span>
        </div>
      </div>
      <!-- Network Down -->
      <div class="resource-row">
        <div class="resource-header">
          <CloudDownload :size="13" class="resource-icon net-down" />
          <span class="resource-label">{{ t('systemResources.download') }}</span>
          <span class="resource-value">{{ formatRate(resources.network.download_rate) }}</span>
        </div>
      </div>
      <!-- Usage meter, last. The header capsule is a desktop surface; on a
           phone this panel is the only place the same numbers appear, so it
           sits at the tail rather than competing with machine health.
           The whole row is the entry point to the rule's settings: there is
           nothing to interact with here beyond "why is it this number".

           With no rule the row still renders, as a placeholder that says so.
           Hiding it would leave a phone user with no discoverable path to the
           feature at all. -->
      <button
        v-if="showUsageMeter"
        type="button"
        class="resource-row usage-row"
        :class="{ 'usage-row--placeholder': !meterStatus.enabled }"
        :aria-label="meterStatus.enabled ? t('usageMeter.adjust') : t('usageMeter.setupHint')"
        :title="meterStatus.enabled ? t('usageMeter.adjust') : t('usageMeter.setupHint')"
        data-action="usage-settings"
        @click="$emit('open-usage-settings')"
      >
        <span class="resource-header">
          <Gauge :size="13" class="resource-icon" :class="`usage-icon is-${meterState}`" />
          <!-- Placeholder: name the feature and what to do, not a label plus a
               blank value. An empty value slot reads as "no data", which is a
               different (and wrong) message from "not configured". -->
          <template v-if="meterStatus.enabled">
            <span class="resource-label">{{ t(metricLabelKey) }}</span>
            <span class="resource-value" :class="`usage-value is-${meterState}`">
              {{ meterUsed }}<span class="usage-quota"> / {{ hasQuota ? meterQuota : INFINITE }}</span>
            </span>
          </template>
          <span v-else class="resource-label">{{ t('usageMeter.setupHint') }}</span>
          <ChevronRight :size="12" class="usage-chevron" />
        </span>
        <template v-if="meterStatus.enabled">
          <!-- The bar only means something against a budget: without one it
               would be an empty track claiming a measurement that does not
               exist. Same rule as the desktop capsule. -->
          <span v-if="hasQuota" class="progress-bar usage-bar">
            <span class="usage-fill" :class="`is-${meterState}`" :style="{ width: fillPercent + '%' }"></span>
            <span v-if="hasCycle" class="usage-mark" :style="{ left: expectedPercent + '%' }"></span>
          </span>
          <span class="usage-meta">
            <span>{{ periodLabel }}</span>
            <span class="usage-badge" :class="`is-${meterState}`">{{ meterBadge }}</span>
          </span>
        </template>
      </button>
    </template>

    <!-- Switch server sits at the tail: it is a session-level action, not a
         machine-health reading, so it should not interrupt the resource list. -->
    <template v-if="showServerSwitch">
      <div class="header-divider"></div>
      <button class="switch-server-btn" @click="$emit('switch-server')">
        <ArrowLeftRight :size="13" />
        <span class="switch-server-label">{{ t('login.switchServer') }}</span>
      </button>
    </template>
  </div>
</template>

<script setup>
import { Cpu, Activity, MemoryStick, Database, HardDriveDownload, HardDriveUpload, CloudDownload, CloudUpload, Server, ArrowLeftRight, WifiOff, Gauge, ChevronRight } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSystemResources } from '@/composables/useSystemResources'
import { useUsageMeter } from '@/composables/useUsageMeter'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import { formatUsageMeterValue, usageMeterMetricLabelKey } from '@/utils/usageFormat'

const { t } = useI18n()
const { resources, startPolling, stopPolling } = useSystemResources()
const { status: meterStatus, meterState, fillPercent, expectedPercent, hasData, hasQuota, hasCycle } = useUsageMeter()
const { isWideScreen } = useWideScreenLayout()

const props = defineProps({
    showServerSwitch: { type: Boolean, default: false },
    wsStatus: { type: String, default: 'connected' },
})

// Wide screens have the header capsule, so repeating the meter here would show
// the same numbers twice on one screen. Phones have no capsule (deliberately:
// the header is a desktop surface), which is exactly why this row exists.
//
// Gated on `hasData` (the first fetch landed), not on `enabled`: with no rule
// the row renders as a placeholder pointing at the settings page, and hiding it
// would leave a phone user with no way to discover the feature.
const showUsageMeter = computed(
    () => !isWideScreen.value && hasData.value,
)

const meterUsed = computed(() =>
    formatUsageMeterValue(meterStatus.value.metric ?? 'cost', meterStatus.value.actual ?? 0, { compact: true }),
)
/** The row's word names what is counted, same rule as the header capsule. */
const metricLabelKey = computed(() => usageMeterMetricLabelKey(meterStatus.value.metric))
const meterQuota = computed(() =>
    formatUsageMeterValue(meterStatus.value.metric ?? 'cost', meterStatus.value.quota ?? 0, { compact: true }),
)

/** An unset quota / period means infinite, not absent (same rule as the capsule). */
const INFINITE = '\u221e'


const meterBadge = computed(() => {
    if (meterState.value === 'full') return t('usageMeter.badgeFull')
    if (meterState.value === 'over') {
        const expected = meterStatus.value.expected ?? 0
        const actual = meterStatus.value.actual ?? 0
        const pct = expected > 0 ? Math.round(((actual - expected) / expected) * 100) : 0
        return t('usageMeter.badgeOver', { pct })
    }
    // Without a quota there is nothing to be within; without a cycle there is no
    // "by now" to compare against. Either way the badge reports what it can see
    // rather than asserting a budget the user never set.
    if (!hasQuota.value) return t('usageMeter.badgeNoQuota')
    if (!hasCycle.value) return t('usageMeter.badgeNoCycle')
    return t('usageMeter.badgeOk')
})

/**
 * The window line. Same three shapes as the desktop popover: no cycle counts
 * against ∞ (it never rolls over), and no anchor at all means the whole ledger.
 */
const periodLabel = computed(() => {
    const start = meterStatus.value.cycleStart
    if (!hasCycle.value) {
        if (!start) return t('usageMeter.allTime')
        return t('usageMeter.dayOfCycle', { day: meterStatus.value.dayInCycle, days: INFINITE })
    }
    return t('usageMeter.dayOfCycle', {
        day: meterStatus.value.dayInCycle,
        days: meterStatus.value.periodDays ?? 0,
    })
})

const wsDisconnected = computed(() => props.wsStatus === 'disconnected' || props.wsStatus === 'reconnecting')

defineEmits(['switch-server', 'open-usage-settings'])

const serverAddress = window.location.host // e.g. "192.168.1.5:20000"

const cpuPercent = computed(() => {
  const p = resources.value.cpu.percent
  return p < 0 ? '—' : p.toFixed(1)
})

const cpuBarWidth = computed(() => {
  const p = resources.value.cpu.percent
  return p < 0 ? 0 : p
})

// Load average as percentage of CPU capacity (load1 / coreCount * 100)
const loadBarPercent = computed(() => {
  const cores = resources.value.cpu.core_count
  if (cores <= 0) return 0
  const pct = (resources.value.load.load1 / cores) * 100
  return Math.min(pct, 100)
})

function getBarClass(percent) {
  if (percent >= 90) return 'bar-critical'
  if (percent >= 70) return 'bar-warning'
  return 'bar-normal'
}

function formatBytes(bytes) {
  if (bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.max(0, Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1))
  return (bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + units[i]
}

function formatRate(bytesPerSec) {
  if (bytesPerSec <= 0) return '0 B/s'
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  const i = Math.max(0, Math.min(Math.floor(Math.log(bytesPerSec) / Math.log(1024)), units.length - 1))
  return (bytesPerSec / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + units[i]
}

defineExpose({ startPolling, stopPolling })
</script>

<style scoped>
.system-resources-panel {
  padding: var(--space-4) var(--space-6);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.server-info-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
}

.server-info-icon {
  flex-shrink: 0;
  color: var(--accent-color);
}

.server-info-address {
  color: var(--text-primary);
  font-weight: var(--font-weight-medium);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.switch-server-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  width: 100%;
  padding: calc(7 * var(--dp)) 0;
  border: 1px solid color-mix(in srgb, var(--accent-color) 24%, transparent);
  background: color-mix(in srgb, var(--accent-color) 8%, transparent);
  cursor: pointer;
  border-radius: var(--radius-sm, 4px);
  color: var(--accent-color);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  transition: background var(--duration-base), color var(--duration-base);
  flex-shrink: 0;
}

.switch-server-label {
  line-height: 1;
}

@media (hover: hover) {
  .switch-server-btn:hover {
    background: color-mix(in srgb, var(--accent-color) 14%, transparent);
    border-color: color-mix(in srgb, var(--accent-color) 44%, transparent);
  }
}

.header-divider {
  height: 1px;
  background: var(--border-color);
}

.resource-row {
  display: flex;
  flex-direction: column;
  gap: calc(3 * var(--dp));
}

.resource-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-tight);
}

.resource-icon {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.resource-icon.net-up {
  color: var(--color-green, #22c55e);
}

.resource-icon.net-down {
  color: var(--accent-color, #3b82f6);
}

.resource-icon.disk-read {
  color: var(--color-yellow, #eab308);
}

.resource-icon.disk-write {
  color: #f97316;
}

.resource-label {
  color: var(--text-secondary);
  flex-shrink: 0;
  min-width: calc(32 * var(--dp));
}

.resource-value {
  margin-left: auto;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.progress-bar {
  height: calc(4 * var(--dp));
  background: var(--bg-tertiary, #e5e7eb);
  border-radius: var(--radius-xs);
  overflow: hidden;
}

.progress-fill {
  height: 100%;
  border-radius: var(--radius-xs);
  transition: width 0.3s ease;
}

.bar-normal {
  background: var(--color-green, #22c55e);
}

.bar-warning {
  background: var(--color-yellow, #eab308);
}

.bar-critical {
  background: var(--color-red, #ef4444);
}

/* ── Usage meter row (last) ───────────────────────────────────────────────── */
/* Same row rhythm as the machine metrics above; only the bar is different: it
   carries a baseline tick, which is the whole point of the meter (fill against
   "where a linear spend would be"). */
.usage-row {
  /* The row is a <button>: strip the UA chrome but keep the row's own layout.
     text-align must be reset too — a button centres its content, which would
     centre only the meta line while the header stayed flex-aligned. */
  width: 100%;
  padding: var(--space-4) 0 0;
  border: none;
  border-top: 1px solid var(--border-color);
  border-radius: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

@media (hover: hover) {
  .usage-row:hover .usage-chevron {
    color: var(--text-primary);
  }
}

.usage-row:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: calc(2 * var(--dp));
}

.usage-chevron {
  flex-shrink: 0;
  color: var(--text-muted);
  transition: color var(--duration-base);
}

.usage-icon.is-ok { color: var(--color-green); }
.usage-icon.is-over { color: var(--color-orange); }
.usage-icon.is-full { color: var(--color-red); }

/* Placeholder: no rule configured. The label carries the whole message
   ("设置用量提醒"), so it uses the secondary text colour rather than the muted
   label colour — it is an invitation, not a disabled control. */
.usage-row--placeholder .resource-label {
  color: var(--text-secondary);
}

.usage-value.is-over { color: var(--color-orange); }
.usage-value.is-full { color: var(--color-red); }

.usage-quota {
  color: var(--text-muted);
  font-weight: normal;
}

.usage-bar {
  /* The row is a <button>, so every descendant must be a span (buttons may not
     contain block-level flow content in HTML terms). Spans are inline by
     default, which would collapse the 4px track. */
  display: block;
  position: relative;
  margin-top: calc(3 * var(--dp));
}

.usage-fill {
  display: block;
  height: 100%;
  border-radius: var(--radius-xs);
  transition: width 0.3s ease;
}

.usage-fill.is-ok { background: var(--color-green); }
.usage-fill.is-over { background: var(--color-orange); }
.usage-fill.is-full { background: var(--color-red); }

/* Baseline tick: outlined in the popover surface (--bg-secondary, see
   PopupMenu) so it stays visible where it crosses the filled portion. */
.usage-mark {
  position: absolute;
  top: calc(-2 * var(--dp));
  bottom: calc(-2 * var(--dp));
  width: 1px;
  border-radius: 1px;
  background: var(--text-primary);
  box-shadow: 0 0 0 1px var(--bg-secondary);
}

.usage-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-top: calc(2 * var(--dp));
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
}

.usage-badge {
  flex-shrink: 0;
  padding: 0 var(--space-2);
  border-radius: var(--radius-full);
  font-weight: var(--font-weight-medium);
  white-space: nowrap;
}

.usage-badge.is-ok {
  background: color-mix(in srgb, var(--color-green) 16%, transparent);
  color: var(--color-green);
}

.usage-badge.is-over {
  background: color-mix(in srgb, var(--color-orange) 16%, transparent);
  color: var(--color-orange);
}

.usage-badge.is-full {
  background: color-mix(in srgb, var(--color-red) 16%, transparent);
  color: var(--color-red);
}

.connection-status {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-8) 0 var(--space-4);
}

.connection-status-icon {
  flex-shrink: 0;
}

.connection-status-icon.status-disconnected {
  color: var(--color-red, #ef4444);
}

/* Reconnecting uses the shared LoadingIndicator. Only the arc keeps the warning
   yellow the icon carried; the track stays at the component default so this
   reads like every other spinner in the app. */
.connection-status-icon.status-reconnecting {
  --li-color: var(--color-yellow, #eab308);
}

.connection-status-text {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  color: var(--text-secondary);
}
</style>