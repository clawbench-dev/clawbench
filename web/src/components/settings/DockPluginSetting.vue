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

    <SettingsCard :title="t('settings.dockPlugins.title')">
      <div class="plugin-intro">{{ t('settings.dockPlugins.hideHint') }}</div>
      <div class="plugin-list">
        <div
          v-for="plugin in orderedPlugins"
          :key="plugin.id"
          class="plugin-row"
          :class="{ 'is-hidden': isHidden(plugin.id) }"
          :data-dock-plugin="plugin.id"
        >
          <div class="plugin-row__main">
            <component :is="iconFor(plugin.id)" :size="18" class="plugin-row__icon" />
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
              v-if="plugin.settingsCategory"
              type="button"
              class="plugin-link"
              @click="openSettings(plugin.settingsCategory)"
            >
              {{ t('settings.dockPlugins.openSettings') }}
            </button>
            <div v-if="isReorderable(plugin.id)" class="order-buttons">
              <button
                type="button"
                class="plugin-icon-btn"
                :disabled="isFirst(plugin.id)"
                :title="t('settings.dockPlugins.moveUp')"
                :aria-label="t('settings.dockPlugins.moveUp')"
                @click="move(plugin.id, -1)"
              >
                <ChevronUp :size="15" />
              </button>
              <button
                type="button"
                class="plugin-icon-btn"
                :disabled="isLast(plugin.id)"
                :title="t('settings.dockPlugins.moveDown')"
                :aria-label="t('settings.dockPlugins.moveDown')"
                @click="move(plugin.id, 1)"
              >
                <ChevronDown :size="15" />
              </button>
            </div>
            <label
              v-if="plugin.closable"
              class="settings-item__switch plugin-switch"
              :title="t('settings.dockPlugins.visible')"
            >
              <input
                type="checkbox"
                class="settings-item__switch-input"
                :checked="!isHidden(plugin.id)"
                @change="toggle(plugin.id)"
              />
              <span class="settings-item__switch-track"></span>
            </label>
            <span v-else class="plugin-locked">{{ t('settings.dockPlugins.core') }}</span>
          </div>
        </div>
      </div>
      <div class="plugin-hint">{{ t('settings.dockPlugins.orderHint') }}</div>
      <div class="plugin-hint">{{ t('settings.dockPlugins.chatNote') }}</div>
      <div class="plugin-hint">{{ t('settings.dockPlugins.unavailableHint') }}</div>
    </SettingsCard>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ChevronUp, FolderOpen } from 'lucide-vue-next'
import SettingsCard from './SettingsCard.vue'
import {
  DOCK_TABS,
  DOCK_TAB_BY_ID,
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
  move,
  isHidden,
  isReorderable,
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
  const settings = DOCK_TABS.filter((tab) => tab.id === 'settings')
  return [...primary, ...reorderable, ...settings]
})

function iconFor(id: DockTabId) {
  return dockTabWithIcon(id)?.icon ?? FolderOpen
}

function isFirst(id: DockTabId): boolean {
  return reorderableIds.value[0] === id
}

function isLast(id: DockTabId): boolean {
  return reorderableIds.value[reorderableIds.value.length - 1] === id
}

function openSettings(category: string | undefined): void {
  if (category) emit('navigate', category)
}
</script>

<style scoped>
.dock-plugins {
  padding: var(--space-4);
  background: var(--bg-secondary);
  min-height: 100%;
}

.dock-plugins > :deep(.settings-card:last-child) {
  margin-bottom: 0;
}

/* ── Presets ── */

.preset-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(calc(150 * var(--dp)), 1fr));
  gap: var(--space-4);
  padding: var(--space-6) var(--space-7);
}

.preset-card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--space-2);
  min-height: calc(74 * var(--dp));
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-primary);
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
  line-height: 1.45;
  color: var(--text-muted);
}

.preset-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding: var(--space-3) var(--space-7) var(--space-6);
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
  padding: var(--space-3) var(--space-7) 0;
  font-size: var(--font-size-sm);
  line-height: 1.5;
  color: var(--text-muted);
}

.plugin-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
  padding: var(--space-4) var(--space-7);
  position: relative;
}

.plugin-row:not(:last-child)::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: calc(48 * var(--dp));
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.plugin-row__main {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex: 1 1 calc(180 * var(--dp));
  min-width: 0;
}

.plugin-row__icon {
  flex-shrink: 0;
  margin-top: calc(2 * var(--dp));
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
  margin-top: calc(2 * var(--dp));
  font-size: var(--font-size-sm);
  line-height: 1.45;
  color: var(--text-muted);
}

.plugin-chip {
  flex-shrink: 0;
  padding: 0 var(--space-2);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  line-height: 1.6;
}

.plugin-chip--core {
  color: var(--accent-color);
}

.plugin-row__actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-shrink: 0;
  margin-left: auto;
}

.plugin-link {
  padding: 0 var(--space-2);
  border: 0;
  background: transparent;
  color: var(--accent-color);
  font-family: inherit;
  font-size: var(--font-size-sm);
  cursor: pointer;
}

.order-buttons {
  display: flex;
  gap: var(--space-1);
}

.plugin-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-secondary);
  cursor: pointer;
}

.plugin-icon-btn:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

@media (hover: hover) {
  .plugin-icon-btn:hover:not(:disabled) {
    border-color: var(--text-muted);
    color: var(--text-primary);
  }
}

.plugin-switch {
  flex-shrink: 0;
}

.plugin-locked {
  flex-shrink: 0;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}

.plugin-hint {
  padding: 0 var(--space-7) var(--space-3);
  font-size: var(--font-size-sm);
  line-height: 1.5;
  color: var(--text-muted);
}

.plugin-hint:first-of-type {
  padding-top: var(--space-3);
  border-top: 1px solid var(--border-color);
}
</style>
