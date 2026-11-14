<template>
  <div class="dock-plugins">
    <!-- Presets first: they are the one-step choice; the list below is the
         fine-grained override. Applying a preset resets visibility and order. -->
    <SettingsCard :title="t('settings.dockPlugins.presetTitle')">
      <div class="preset-grid">
        <button
          v-for="p in APPLICABLE_DOCK_PRESETS"
          :key="p"
          type="button"
          class="preset-card"
          :class="{ 'is-active': preset === p }"
          @click="applyPreset(p)"
        >
          <span class="preset-card__name">{{ t(`settings.dockPlugins.presets.${p}.name`) }}</span>
          <span class="preset-card__desc">{{ t(`settings.dockPlugins.presets.${p}.desc`) }}</span>
        </button>
      </div>
      <div class="preset-footer">
        <span class="preset-current">
          {{ preset === 'custom'
            ? `${t('settings.dockPlugins.presetCustom')} · ${t('settings.dockPlugins.presetCustomDesc')}`
            : t(`settings.dockPlugins.presets.${preset}.name`) }}
        </span>
        <button class="fbtn" type="button" @click="reset">
          {{ t('settings.dockPlugins.reset') }}
        </button>
      </div>
    </SettingsCard>

    <SettingsCard :title="t('settings.dockPlugins.listTitle')">
      <div class="plugin-intro">{{ t('settings.dockPlugins.hideHint') }}</div>
      <!-- Only reorderable rows carry a drag handle, so fixed tabs (workspace
           head, notifications, settings) cannot be dragged. A drop that crosses
           a fixed row is normalised back to head / movable tail / settings. -->
      <VueDraggable
        v-model="localPlugins"
        handle=".plugin-drag-handle"
        class="plugin-list"
        @end="onDragEnd"
      >
        <div
          v-for="(plugin, index) in localPlugins"
          :key="plugin.id"
          class="plugin-row"
          :class="{ 'is-hidden': isHidden(plugin.id), 'is-first': index === 0 }"
          :data-dock-plugin="plugin.id"
        >
          <span
            v-if="isReorderable(plugin.id)"
            class="plugin-drag-handle"
            :title="t('settings.dockPlugins.dragHandle')"
            aria-hidden="true"
          >≡</span>
          <span v-else class="plugin-drag-spacer" />

          <div class="plugin-row__main">
            <component :is="iconFor(plugin.id)" :size="16" class="plugin-row__icon" />
            <div class="plugin-row__text">
              <div class="plugin-row__title-line">
                <span class="plugin-row__name">{{ t(plugin.titleKey) }}</span>
                <span class="plugin-chip">{{ t(`settings.dockPlugins.groups.${plugin.group}`) }}</span>
                <span v-if="!plugin.closable" class="plugin-chip plugin-chip--core">
                  {{ t('settings.dockPlugins.core') }}
                </span>
                <span v-if="plugin.devRelated" class="plugin-chip">
                  {{ t('settings.dockPlugins.devRelated') }}
                </span>
                <span v-if="plugin.feature" class="plugin-chip">
                  {{ t('settings.dockPlugins.feature') }}
                </span>
                <span v-if="plugin.capability" class="plugin-chip">
                  {{ t('settings.dockPlugins.capability') }}
                </span>
              </div>
              <div class="plugin-row__desc">{{ t(plugin.descriptionKey) }}</div>
            </div>
          </div>

          <div class="plugin-row__actions">
            <button
              v-if="plugin.settingsCategory && !isHidden(plugin.id)"
              type="button"
              class="plugin-gear"
              :title="t('settings.dockPlugins.openSettings')"
              :aria-label="t('settings.dockPlugins.openSettings')"
              @click="openSettings(plugin.settingsCategory)"
            >
              <SettingsIcon :size="16" />
            </button>
            <label
              class="settings-item__switch plugin-switch"
              :class="{ 'plugin-switch--locked': !plugin.closable }"
              :title="plugin.closable ? t('settings.dockPlugins.visible') : t('settings.dockPlugins.core')"
            >
              <input
                type="checkbox"
                class="settings-item__switch-input"
                :checked="!isHidden(plugin.id)"
                :disabled="!plugin.closable"
                @change="toggle(plugin.id)"
              />
              <span class="settings-item__switch-track"></span>
            </label>
          </div>
        </div>
      </VueDraggable>
      <div class="plugin-hints">
        <p class="plugin-hint">{{ t('settings.dockPlugins.orderHint') }}</p>
        <p class="plugin-hint">{{ t('settings.dockPlugins.unavailableHint') }}</p>
      </div>
    </SettingsCard>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { FolderOpen, Settings as SettingsIcon } from 'lucide-vue-next'
import { VueDraggable } from 'vue-draggable-plus'
import SettingsCard from './SettingsCard.vue'
import {
  DOCK_TABS,
  DOCK_TAB_BY_ID,
  REORDERABLE_DOCK_TABS,
  type DockTabDescriptor,
  type DockTabId,
} from '@/composables/dockTabs'
import { dockTabWithIcon } from '@/composables/dockTabMeta'
import {
  APPLICABLE_DOCK_PRESETS,
  useDockPlugins,
} from '@/composables/useDockPlugins'
import '@/assets/modal-footer-btn.css'

const emit = defineEmits<{ navigate: [categoryId: string] }>()

const { t } = useI18n()
const {
  preset,
  reorderableIds,
  applyPreset,
  reset,
  toggle,
  isHidden,
  isReorderable,
  setOrder,
} = useDockPlugins()

/**
 * The manager's list mirrors the dock: fixed primary tabs first (browse, view,
 * history), then the reorderable tail in the user's order, then settings last.
 * Hidden tabs stay in the list so the user can turn them back on.
 */
const orderedPlugins = computed<DockTabDescriptor[]>(() => {
  const primary = DOCK_TABS.filter((tab) => tab.primary)
  const reorderable = reorderableIds.value
    .map((id) => DOCK_TAB_BY_ID.get(id))
    .filter((tab): tab is DockTabDescriptor => !!tab)
  // Future non-primary tabs that are neither reorderable nor the fixed tail
  // (none today). Mirrors secondaryDockTabs so the manager never silently
  // drops a tab the dock renders.
  const rest = DOCK_TABS.filter(
    (tab) => !tab.primary && tab.id !== 'settings' && !REORDERABLE_DOCK_TABS.includes(tab.id),
  )
  const settings = DOCK_TABS.filter((tab) => tab.id === 'settings')
  return [...primary, ...reorderable, ...rest, ...settings]
})

// Local mirror the draggable mutates in place. Synced from the store order so
// a preset reset (or any other writer) lands here too.
const localPlugins = ref<DockTabDescriptor[]>([...orderedPlugins.value])
watch(
  orderedPlugins,
  (val) => {
    localPlugins.value = [...val]
  },
  { deep: true },
)

function onDragEnd(): void {
  const ids = localPlugins.value.filter((p) => isReorderable(p.id)).map((p) => p.id)
  setOrder(ids)
  // A drop can land above a fixed tab; snap back to the canonical layout
  // (fixed head, movable tail, settings last) instead of leaving the gap.
  localPlugins.value = [...orderedPlugins.value]
}

function iconFor(id: DockTabId) {
  return dockTabWithIcon(id)?.icon ?? FolderOpen
}

function openSettings(category: string | undefined): void {
  if (category) emit('navigate', category)
}
</script>

<style scoped>
.dock-plugins {
  padding: var(--space-4);
  /* Shared page surface (see --panel-page-bg). */
  background: var(--panel-page-bg);
  min-height: 100%;
}

.dock-plugins > :deep(.settings-card:last-child) {
  margin-bottom: 0;
}

/* ── Presets ── */

.preset-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(calc(140 * var(--dp)), 1fr));
  gap: var(--space-3);
  padding: var(--space-4) var(--space-7) var(--space-3);
}

.preset-card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: calc(2 * var(--dp));
  min-height: calc(58 * var(--dp));
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  /* Card step above the page surface (see --panel-page-bg), like every other
     card in the app. */
  background: var(--bg-secondary);
  color: var(--text-primary);
  text-align: left;
  font-family: inherit;
  cursor: pointer;
  transition: border-color var(--duration-base), background var(--duration-base);
}

.preset-card.is-active {
  border-color: var(--accent-color);
  background: var(--bg-tertiary);
}

@media (hover: hover) {
  .preset-card:hover {
    border-color: var(--text-muted);
  }
}

.preset-card:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
}

.preset-card__name {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
}

.preset-card__desc {
  font-size: var(--font-size-sm);
  line-height: 1.35;
  color: var(--text-muted);
}

.preset-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding: var(--space-2) var(--space-7) var(--space-4);
  border-top: 1px solid var(--border-color);
}

.preset-current {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  min-width: 0;
}

/* ── Plugin list ── */

.plugin-list {
  display: flex;
  flex-direction: column;
}

.plugin-intro {
  padding: var(--space-2) var(--space-7);
  font-size: var(--font-size-sm);
  line-height: 1.45;
  color: var(--text-muted);
}

.plugin-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-7);
  position: relative;
}

.plugin-row:not(.is-first) {
  border-top: 0.5px solid var(--border-color);
}

.plugin-drag-handle,
.plugin-drag-spacer {
  flex-shrink: 0;
  /* Fixed width (the icon's own size) so every row's icon stays aligned
     whether or not the row is draggable. */
  width: var(--icon-size-md);
}

.plugin-drag-handle {
  text-align: center;
  cursor: grab;
  color: var(--text-muted);
  font-size: var(--font-size-xl);
  line-height: 1;
  user-select: none;
}

.plugin-drag-handle:active {
  cursor: grabbing;
}

.plugin-row__main {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex: 1 1 auto;
  min-width: 0;
}

.plugin-row__icon {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.plugin-row.is-hidden .plugin-row__icon,
.plugin-row.is-hidden .plugin-row__name {
  opacity: var(--opacity-muted);
}

.plugin-row__text {
  min-width: 0;
}

.plugin-row__title-line {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.plugin-row__name {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
}

.plugin-row__desc {
  margin-top: calc(1 * var(--dp));
  font-size: var(--font-size-sm);
  line-height: 1.35;
  color: var(--text-muted);
}

.plugin-chip {
  flex-shrink: 0;
  padding: 0 var(--space-1);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  line-height: 1.5;
}

.plugin-chip--core {
  color: var(--accent-color);
}

.plugin-row__actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  margin-left: auto;
}

.plugin-gear {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

@media (hover: hover) {
  .plugin-gear:hover {
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }
}

.plugin-gear:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
}

.plugin-switch {
  flex-shrink: 0;
}

/* Core tabs keep the switch for a uniform row shape, but it is inert and
   greyed out: there is no way to hide them. */
.plugin-switch--locked {
  cursor: default;
  opacity: var(--opacity-muted);
}

.plugin-switch--locked .settings-item__switch-input:checked + .settings-item__switch-track {
  background: var(--bg-tertiary);
}

.plugin-hints {
  padding: var(--space-2) var(--space-7) var(--space-3);
  border-top: 0.5px solid var(--border-color);
}

.plugin-hint {
  font-size: var(--font-size-sm);
  line-height: 1.4;
  color: var(--text-muted);
}

.plugin-hint + .plugin-hint {
  margin-top: calc(2 * var(--dp));
}
</style>
