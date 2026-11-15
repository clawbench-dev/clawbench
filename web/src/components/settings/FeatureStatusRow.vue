<template>
  <!-- Read-only feature status for a module whose master switch lives in the
       Dock feature manager (功能编排).

       Why read-only: one switch in the manager controls BOTH the dock tab and
       the background feature, and turning some of them off is destructive
       (Forge clears every repository binding, share revokes every issued link).
       A second switch on the module's own settings page would be a second
       source of truth for the same flag, and the two would drift apart the
       moment one of them was saved from a stale page. -->
  <div class="feature-status" :data-settings-key="enableKey">
    <div class="feature-status__main">
      <div class="feature-status__text">
        <span class="feature-status__label">{{ label }}</span>
        <span
          class="feature-status__pill"
          :class="enabled ? 'feature-status__pill--on' : 'feature-status__pill--off'"
        >
          <Check v-if="enabled" :size="12" />
          <PowerOff v-else :size="12" />
          {{ t(enabled ? 'settings.items.featureStatusEnabled' : 'settings.items.featureStatusDisabled') }}
        </span>
      </div>
      <button
        type="button"
        class="feature-status__manage"
        data-action="feature-status-manage"
        @click="$emit('navigate')"
      >
        {{ t('settings.items.featureStatusGoManage') }}
        <ChevronRight :size="14" />
      </button>
    </div>
    <div v-if="description" class="feature-status__desc">{{ description }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, ChevronRight, PowerOff } from 'lucide-vue-next'
import { useSettingsConfig } from '@/composables/useSettingsConfig'

const props = defineProps<{
  /** Server enable key, e.g. `forge.enabled`. */
  enableKey: string
  label: string
  description?: string
}>()

defineEmits<{
  navigate: []
}>()

const { t } = useI18n()
// Read through the composable rather than taking a value prop: `serverConfig`
// is a reactive ref, so the pill flips as soon as the manager saves — without
// this row having to listen to anything.
const { getServerValueWithDefault } = useSettingsConfig()

const enabled = computed(() => !!getServerValueWithDefault(props.enableKey))
</script>

<style scoped>
.feature-status {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-6) var(--space-7);
  background: var(--bg-primary);
  position: relative;
}

.feature-status::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.feature-status__main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  width: 100%;
  min-width: 0;
}

.feature-status__text {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  min-width: 0;
}

.feature-status__label {
  font-size: var(--font-size-md);
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Same pill geometry as the summary-model status pill in SettingsItem, tinted
   from the theme's success / muted tokens so it reads correctly in all 36
   themes (no fixed background step). */
.feature-status__pill {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  padding: 1px var(--space-4);
  border-radius: var(--radius-full);
  border: 1px solid transparent;
  white-space: nowrap;
}

.feature-status__pill--on {
  color: var(--color-success);
  background: color-mix(in srgb, var(--color-success) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-success) 35%, transparent);
}

.feature-status__pill--off {
  color: var(--text-muted);
  background: color-mix(in srgb, var(--text-primary) 6%, transparent);
  border-color: color-mix(in srgb, var(--text-primary) 18%, transparent);
}

.feature-status__manage {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
  padding: var(--space-2) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}

.feature-status__manage:hover {
  color: var(--text-primary);
  border-color: var(--border-color-strong, var(--border-color));
}

.feature-status__desc {
  width: 100%;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  line-height: var(--line-height-normal);
  word-break: break-word;
}
</style>
