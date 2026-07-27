<template>
  <div class="system-resources-panel">
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
    <!-- Network Up -->
    <div class="resource-row">
      <div class="resource-header">
        <ArrowUp :size="13" class="resource-icon net-up" />
        <span class="resource-label">{{ t('systemResources.upload') }}</span>
        <span class="resource-value">{{ formatRate(resources.network.upload_rate) }}</span>
      </div>
    </div>
    <!-- Network Down -->
    <div class="resource-row">
      <div class="resource-header">
        <ArrowDown :size="13" class="resource-icon net-down" />
        <span class="resource-label">{{ t('systemResources.download') }}</span>
        <span class="resource-value">{{ formatRate(resources.network.download_rate) }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { Cpu, MemoryStick, Database, ArrowUp, ArrowDown } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSystemResources } from '@/composables/useSystemResources'

const { t } = useI18n()
const { resources, startPolling, stopPolling } = useSystemResources()

const cpuPercent = computed(() => {
  const p = resources.value.cpu.percent
  return p < 0 ? '0.0' : p.toFixed(1)
})

const cpuBarWidth = computed(() => {
  const p = resources.value.cpu.percent
  return p < 0 ? 0 : p
})

function getBarClass(percent) {
  if (percent >= 90) return 'bar-critical'
  if (percent >= 70) return 'bar-warning'
  return 'bar-normal'
}

function formatBytes(bytes) {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return (bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + units[i]
}

function formatRate(bytesPerSec) {
  if (bytesPerSec <= 0) return '0 B/s'
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  const i = Math.min(Math.floor(Math.log(bytesPerSec) / Math.log(1024)), units.length - 1)
  return (bytesPerSec / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + units[i]
}

defineExpose({ startPolling, stopPolling })
</script>

<style scoped>
.system-resources-panel {
  padding: 8px 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.resource-row {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.resource-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  line-height: 1.2;
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

.resource-label {
  color: var(--text-secondary);
  flex-shrink: 0;
  min-width: 32px;
}

.resource-value {
  margin-left: auto;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.progress-bar {
  height: 4px;
  background: var(--bg-tertiary, #e5e7eb);
  border-radius: 2px;
  overflow: hidden;
}

.progress-fill {
  height: 100%;
  border-radius: 2px;
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
</style>
