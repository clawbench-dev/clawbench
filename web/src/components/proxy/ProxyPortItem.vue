<template>
  <div class="proxy-port-item" :class="{ disabled: !enabled }">
    <!-- Header row: identity + toggle -->
    <div class="port-row-top">
      <div class="port-badges">
        <span class="port-number">{{ localPort }}</span>
        <span class="port-protocol" :class="protocol">{{ protocol }}</span>
        <span class="port-status" :class="statusClass" :title="statusTitle"></span>
      </div>
      <div class="port-toggle">
        <button
          class="toggle-switch"
          :class="{ on: enabled }"
          :disabled="toggling"
          :title="enabled ? t('proxy.disable') : t('proxy.enable')"
          @click.stop="$emit('toggleEnabled', localPort, !enabled)"
        >
          <span class="toggle-thumb" />
        </button>
      </div>
    </div>

    <!-- Info row: name + target -->
    <div class="port-info">
      <span v-if="name" class="port-name">{{ name }}</span>
      <span v-if="port !== localPort" class="port-target">→ {{ host || 'localhost' }}:{{ port }}</span>
      <span v-else-if="host" class="port-host">{{ host }}</span>
    </div>

    <!-- Actions row -->
    <div class="port-actions">
      <button class="port-action-btn sandbox" :disabled="!enabled" @click.stop="$emit('open', localPort, protocol, host)" :title="t('proxy.openInSandbox')">
        <Box :size="14" />
      </button>
      <button class="port-action-btn open" :disabled="!enabled" @click.stop="$emit('openExternal', localPort, protocol, host)" :title="t('proxy.openInBrowser')">
        <ExternalLink :size="14" />
      </button>
      <button class="port-action-btn reconnect" :class="{ spinning: reconnecting }" :disabled="reconnecting || !enabled" @click.stop="$emit('reconnect', localPort)" :title="t('proxy.reconnectPort')">
        <RotateCcw :size="14" />
      </button>
      <span class="port-actions-spacer" />
      <button class="port-action-btn edit" @click.stop="$emit('edit', localPort)" :title="t('common.edit')">
        <Pencil :size="14" />
      </button>
      <button class="port-action-btn delete" @click.stop="$emit('remove', localPort)" :title="t('common.delete')">
        <Trash2 :size="14" />
      </button>
    </div>
  </div>
</template>

<script setup>
import { Box, ExternalLink, RotateCcw, Pencil, Trash2 } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  port: { type: Number, required: true },
  localPort: { type: Number, required: true },
  host: { type: String, default: '' },
  name: { type: String, default: '' },
  protocol: { type: String, default: 'http' },
  active: { type: Boolean, default: false },
  enabled: { type: Boolean, default: true },
  tunnelDisconnected: { type: Boolean, default: false },
  reconnecting: { type: Boolean, default: false },
  connecting: { type: Boolean, default: false },
  toggling: { type: Boolean, default: false },
})

defineEmits(['open', 'openExternal', 'reconnect', 'edit', 'remove', 'toggleEnabled'])

const statusClass = computed(() => {
  if (!props.enabled) return 'disabled'
  if (props.connecting) return 'connecting'
  if (props.active) return 'active'
  if (props.tunnelDisconnected) return 'tunnel-down'
  return 'inactive'
})

const statusTitle = computed(() => {
  if (!props.enabled) return t('proxy.portItem.disabled')
  if (props.connecting) return t('proxy.portItem.connecting')
  if (props.active) return t('proxy.portItem.active')
  if (props.tunnelDisconnected) return t('proxy.portItem.tunnelDown')
  return t('proxy.portItem.inactive')
})
</script>

<style scoped>
.proxy-port-item {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  border-radius: 0;
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  cursor: pointer;
  transition: all 0.2s ease;
  position: relative;
  overflow: hidden;
}

.proxy-port-item:hover {
  border-color: var(--accent-color, #0066cc);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
}

.proxy-port-item.disabled {
  opacity: 0.55;
}

.proxy-port-item.disabled:hover {
  border-color: var(--border-color, #e5e5e5);
  box-shadow: none;
}

/* Header row: badges left, toggle right */
.port-row-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.port-badges {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.port-number {
  font-size: 18px;
  font-weight: 700;
  font-family: monospace;
  color: var(--text-primary, #1a1a1a);
  line-height: 1;
}

.port-protocol {
  font-size: 10px;
  font-weight: 600;
  padding: 2px 6px;
  border-radius: 0;
  text-transform: uppercase;
  line-height: 1;
}

.port-protocol.http {
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
}

.port-protocol.https {
  background: rgba(59, 130, 246, 0.12);
  color: #2563eb;
}

.port-status {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.port-status.active {
  background: #22c55e;
  box-shadow: 0 0 4px rgba(34, 197, 94, 0.4);
}

.port-status.connecting {
  background: #f59e0b;
  box-shadow: 0 0 4px rgba(245, 158, 11, 0.4);
  animation: pulse-yellow 1.5s ease-in-out infinite;
}

.port-status.inactive {
  background: #9ca3af;
}

.port-status.tunnel-down {
  background: #ef4444;
  box-shadow: 0 0 4px rgba(239, 68, 68, 0.4);
  animation: pulse-red 2s ease-in-out infinite;
}

.port-status.disabled {
  background: #9ca3af;
  opacity: 0.6;
}

@keyframes pulse-red {
  0%, 100% {
    box-shadow: 0 0 4px rgba(239, 68, 68, 0.4);
  }
  50% {
    box-shadow: 0 0 8px rgba(239, 68, 68, 0.7);
  }
}

@keyframes pulse-yellow {
  0%, 100% {
    opacity: 0.5;
    box-shadow: 0 0 4px rgba(245, 158, 11, 0.4);
  }
  50% {
    opacity: 1;
    box-shadow: 0 0 8px rgba(245, 158, 11, 0.7);
  }
}

/* Toggle switch */
.port-toggle {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

.toggle-switch {
  width: 38px;
  height: 20px;
  border-radius: 10px;
  border: none;
  background: var(--bg-tertiary, #e9ecef);
  position: relative;
  cursor: pointer;
  transition: background 0.2s ease;
  padding: 0;
  flex-shrink: 0;
}

.toggle-switch.on {
  background: var(--accent-color, #0066cc);
}

.toggle-switch:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.toggle-thumb {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #fff;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.25);
  transition: transform 0.2s ease;
}

.toggle-switch.on .toggle-thumb {
  transform: translateX(18px);
}

/* Info row */
.port-info {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
  padding-left: 2px;
}

.port-name {
  font-size: 13px;
  color: var(--text-secondary, #666);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
  max-width: 100%;
}

.port-target {
  font-size: 11px;
  font-family: monospace;
  font-weight: 500;
  padding: 1px 6px;
  border-radius: 0;
  background: rgba(59, 130, 246, 0.1);
  color: #3b82f6;
}

.port-host {
  font-size: 11px;
  font-family: monospace;
  font-weight: 500;
  padding: 1px 6px;
  border-radius: 0;
  background: rgba(107, 114, 128, 0.1);
  color: var(--text-secondary, #666);
}

/* Actions row */
.port-actions {
  display: flex;
  gap: 2px;
  align-items: center;
  border-top: 1px solid var(--border-color, #e5e5e5);
  padding-top: 6px;
}

.port-actions-spacer {
  flex: 1;
}

.port-action-btn {
  width: 28px;
  height: 28px;
  border: none;
  background: none;
  color: var(--text-muted, #999);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 0;
  transition: all 0.15s;
}

.port-action-btn:hover:not(:disabled) {
  color: var(--text-secondary, #666);
  background: var(--bg-tertiary, #f0f0f0);
}

.port-action-btn:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}

.port-action-btn.open:hover:not(:disabled) {
  color: var(--accent-color, #0066cc);
  background: var(--bg-tertiary, #f0f0f0);
}

.port-action-btn.sandbox:hover:not(:disabled) {
  color: #8b5cf6;
  background: var(--bg-tertiary, #f0f0f0);
}

.port-action-btn.reconnect:hover:not(:disabled) {
  color: #22c55e;
  background: var(--bg-tertiary, #f0f0f0);
}

.port-action-btn.reconnect.spinning svg {
  animation: spin 1s linear infinite;
}

.port-action-btn.edit:hover:not(:disabled) {
  color: #f59e0b;
  background: var(--bg-tertiary, #f0f0f0);
}

.port-action-btn.delete:hover:not(:disabled) {
  color: #dc3545;
  background: var(--bg-tertiary, #f0f0f0);
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
