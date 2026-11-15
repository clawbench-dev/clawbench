<template>
  <div class="dock-plugins">
    <!-- Presets first: they are the one-step choice; the list below is the
         fine-grained override. A preset sets visibility, ORDER and the server
         switches together, in one request. -->
    <SettingsCard :title="t('settings.dockPlugins.presetTitle')">
      <div class="preset-grid">
        <button
          v-for="p in APPLICABLE_DOCK_PRESETS"
          :key="p"
          type="button"
          class="preset-card"
          :class="{ 'is-active': preset === p }"
          :disabled="presetBusy"
          @click="onPreset(p)"
        >
          <span class="preset-card__head">
            <component :is="PRESET_ICONS[p]" :size="16" class="preset-card__icon" />
            <span class="preset-card__name">{{ t(`settings.dockPlugins.presets.${p}.name`) }}</span>
          </span>
          <span class="preset-card__desc">{{ t(`settings.dockPlugins.presets.${p}.desc`) }}</span>
        </button>
      </div>
      <div class="preset-footer">
        <span class="preset-current">{{ presetStatusText }}</span>
        <button class="fbtn" type="button" :disabled="presetBusy" @click="onPreset('developer')">
          {{ t('settings.dockPlugins.reset') }}
        </button>
      </div>
    </SettingsCard>

    <SettingsCard :title="t('settings.dockPlugins.listTitle')">
      <div class="plugin-intro">{{ t('settings.dockPlugins.description') }}</div>
      <!-- EVERY row carries a drag handle, permanent tabs included: order is
           independent of whether an entry can be hidden. -->
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
          :class="{ 'is-hidden': !isEnabled(plugin), 'is-first': index === 0, 'is-busy': isSaving(plugin.id) }"
          :data-dock-plugin="plugin.id"
        >
          <span
            class="plugin-drag-handle"
            :title="t('settings.dockPlugins.dragHandle')"
            aria-hidden="true"
          >≡</span>

          <div class="plugin-row__main">
            <div class="plugin-row__text">
              <!-- Icon and title share the first line; the description is a
                   separate line underneath, so the two never interleave. -->
              <div class="plugin-row__title-line">
                <component :is="iconFor(plugin.id)" :size="16" class="plugin-row__icon" />
                <span class="plugin-row__name">{{ t(plugin.titleKey) }}</span>
                <span class="plugin-chip">{{ t(`settings.dockPlugins.groups.${plugin.group}`) }}</span>
              </div>
              <div class="plugin-row__desc">{{ t(plugin.descriptionKey) }}</div>
              <div v-if="blockedReason(plugin)" class="plugin-row__reason">
                {{ blockedReason(plugin) }}
              </div>
            </div>
          </div>

          <div class="plugin-row__actions">
            <button
              v-if="plugin.settingsCategory && isEnabled(plugin)"
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
              :class="{ 'plugin-switch--locked': !plugin.closable || !!blockedReason(plugin) }"
              :title="switchLabel(plugin)"
            >
              <input
                type="checkbox"
                class="settings-item__switch-input"
                :checked="isEnabled(plugin)"
                :disabled="!plugin.closable || !!blockedReason(plugin) || isSaving(plugin.id) || presetBusy"
                @change="togglePlugin(plugin)"
              />
              <span class="settings-item__switch-track"></span>
            </label>
          </div>
        </div>
      </VueDraggable>
    </SettingsCard>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Briefcase,
  CodeXml,
  Feather,
  FolderOpen,
  Settings as SettingsIcon,
} from 'lucide-vue-next'
import { VueDraggable } from 'vue-draggable-plus'
import SettingsCard from './SettingsCard.vue'
import { DOCK_TAB_BY_ID, type DockTabDescriptor, type DockTabId, type DockFeature } from '@/composables/dockTabs'
import { dockTabWithIcon } from '@/composables/dockTabMeta'
import { dockUnavailableReasonKey, isDockPluginAvailable } from '@/composables/dockPluginRuntime'
import {
  APPLICABLE_DOCK_PRESETS,
  DOCK_PRESET_PLANS,
  PRESETS_NEEDING_CONFIRM,
  useDockPlugins,
  type DockFeatureChange,
  type DockPreset,
} from '@/composables/useDockPlugins'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { isFeatureEnabled } from '@/composables/useFeatureFlags'
import { useTerminalStatus } from '@/composables/useTerminalStatus'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import { appLog } from '@/utils/appLog'
import '@/assets/modal-footer-btn.css'

const TAG = 'DockPluginSetting'

/**
 * Switches that stop server-side work, so turning them off needs a
 * confirmation. Mail / knowledge base are deliberately absent: off stops
 * background work but deletes nothing, so the switch is trivially reversible.
 */
const DESTRUCTIVE_FEATURES: readonly DockFeature[] = [
  'forge.enabled',
  'terminal.enabled',
  'tasks.enabled',
  'share.enabled',
]

/**
 * One icon per preset card. Kept next to the manager rather than in
 * `useDockPlugins` so the composable stays free of lucide imports: presets are
 * data there, presentation here.
 */
const PRESET_ICONS: Record<Exclude<DockPreset, 'custom'>, Component> = {
  developer: CodeXml,
  standard: Briefcase,
  minimal: Feather,
}

const emit = defineEmits<{ navigate: [categoryId: string] }>()

const { t } = useI18n()
const { confirm } = useDialog()
const toast = useToast()
const { setServerValue, patchConfig } = useSettingsConfig()
// The PTY platform flag is separate from the terminal's runtime availability:
// on an unsupported OS the server can still report the feature as enabled, and
// the dock deliberately keeps the tab so its panel can explain why. The switch
// must not claim the feature works there, though.
const { platformSupported } = useTerminalStatus()
const {
  preset,
  order,
  applyPreset,
  setVisible,
  isHidden,
  setOrder,
  markCustom,
} = useDockPlugins()

/** Every tab, in the user's order. Hidden and switched-off rows stay listed. */
const orderedPlugins = computed<DockTabDescriptor[]>(() =>
  order.value
    .map((id) => DOCK_TAB_BY_ID.get(id))
    .filter((tab): tab is DockTabDescriptor => !!tab),
)

// Local mirror the draggable mutates in place. Synced from the store order so
// a preset (or any other writer) lands here too.
const localPlugins = ref<DockTabDescriptor[]>([...orderedPlugins.value])
watch(
  orderedPlugins,
  (val) => {
    localPlugins.value = [...val]
  },
  { deep: true },
)

/** Rows with a write in flight. Keyed by tab id so one save cannot block another. */
const saving = ref<Set<DockTabId>>(new Set())
/** Presets write several keys in one request; the whole list waits. */
const presetBusy = ref(false)

function isSaving(id: DockTabId): boolean {
  return saving.value.has(id)
}

function featureKey(plugin: DockTabDescriptor): DockFeature | undefined {
  return plugin.feature
}

/** Server master switch state. Unset = on (mirrors the server default). */
function featureOn(plugin: DockTabDescriptor): boolean {
  const key = featureKey(plugin)
  return key ? isFeatureEnabled(key) : true
}

/** The one switch per row: the server feature AND the local visibility. */
function isEnabled(plugin: DockTabDescriptor): boolean {
  return featureOn(plugin) && !isHidden(plugin.id)
}

/**
 * Tooltip for the switch. The row shows no visible switch label any more —
 * "启用" / "在 Dock 中显示" next to every row was noise, and a permanent row's
 * inert switch still needs its reason on hover.
 */
function switchLabel(plugin: DockTabDescriptor): string {
  if (!plugin.closable) return t('settings.dockPlugins.switchPermanent')
  if (plugin.feature) return t('settings.dockPlugins.switchEnable')
  return t('settings.dockPlugins.switchVisible')
}

/**
 * Why the row cannot be toggled right now.
 *
 * A feature that is switched OFF is not "blocked" — the switch is exactly how
 * the user turns it back on. Only an environmental failure greys the switch
 * out, and then the reason is spelled out under the description.
 *
 * Terminal has two independent ways to be unusable here: the runtime may be
 * down (`isDockPluginAvailable`) or the OS may not support a PTY at all while
 * the server still reports the feature as enabled. Both grey the switch; the
 * dock's own visibility gate is deliberately narrower (it keeps the tab on an
 * unsupported OS so the panel can show its empty state).
 */
function blockedReason(plugin: DockTabDescriptor): string {
  if (!plugin.capability) return ''
  if (plugin.feature && !featureOn(plugin)) return ''
  const unsupportedPlatform =
    plugin.capability === 'terminal' && platformSupported.value === false
  if (!unsupportedPlatform && isDockPluginAvailable(plugin.id)) return ''
  const key = dockUnavailableReasonKey(plugin.id)
  return key ? t(key) : t('settings.dockPlugins.unavailable.generic')
}

function iconFor(id: DockTabId) {
  return dockTabWithIcon(id)?.icon ?? FolderOpen
}

function openSettings(category: string | undefined): void {
  if (category) emit('navigate', category)
}

function onDragEnd(): void {
  setOrder(localPlugins.value.map((p) => p.id))
  // Re-sync from the store so the rendered order is the merged one (the
  // draggable's local array is authoritative only for this gesture).
  localPlugins.value = [...orderedPlugins.value]
}

/** One consequence line per destructive feature, or '' when it is harmless. */
function consequence(plugin: DockTabDescriptor): string {
  const key = featureKey(plugin)
  if (!key) return ''
  const id = key.split('.')[0]
  return t(`settings.dockPlugins.consequences.${id}`)
}

/**
 * Toggle one row.
 *
 * Feature rows write the server switch (one PATCH); display-only rows just hide
 * locally. A destructive feature asks first — the consequences are identical
 * whether they come from a preset or from this single switch, so the
 * confirmation cannot live only on the preset path.
 */
async function togglePlugin(plugin: DockTabDescriptor): Promise<void> {
  if (!plugin.closable || blockedReason(plugin) || isSaving(plugin.id) || presetBusy.value) return
  const turnOn = !isEnabled(plugin)

  if (!plugin.feature) {
    setVisible(plugin.id, turnOn)
    return
  }

  if (turnOn) {
    if (!featureOn(plugin)) await writeFeature(plugin, true)
    if (isHidden(plugin.id)) setVisible(plugin.id, true)
    return
  }

  if (DESTRUCTIVE_FEATURES.includes(plugin.feature)) {
    const ok = await confirm(consequence(plugin), {
      title: t('settings.dockPlugins.switchConfirmTitle'),
      confirmText: t('settings.dockPlugins.switchConfirmOk'),
      dangerous: true,
    })
    if (!ok) return
  }
  await writeFeature(plugin, false)
}

/**
 * Write one server switch. `setServerValue` optimistically updates the cached
 * config and restores it when the PATCH fails, so the switch snaps back on its
 * own; the toast is what tells the user why.
 */
async function writeFeature(plugin: DockTabDescriptor, value: boolean): Promise<void> {
  const key = featureKey(plugin)
  if (!key) return
  saving.value = new Set([...saving.value, plugin.id])
  try {
    await setServerValue(key, value)
    markCustom()
  } catch (err) {
    appLog.e(TAG, `failed to write ${key}`, err)
    toast.show(t('settings.dockPlugins.saveFailed'), { icon: 'warning', type: 'error' })
  } finally {
    const next = new Set(saving.value)
    next.delete(plugin.id)
    saving.value = next
  }
}

/** `[{key:'forge.enabled',value:false}]` → `{ forge: { enabled: false } }`. */
function toConfigChanges(changes: readonly DockFeatureChange[]): Record<string, unknown> {
  const out: Record<string, Record<string, boolean>> = {}
  for (const { key, value } of changes) {
    const [section, field] = key.split('.')
    if (!out[section]) out[section] = {}
    out[section][field] = value
  }
  return out
}

/** Which preset is in force — or that a preset write is still in flight. */
const presetStatusText = computed(() => {
  if (presetBusy.value) return t('settings.dockPlugins.saving')
  return preset.value === 'custom'
    ? `${t('settings.dockPlugins.presetCustom')} · ${t('settings.dockPlugins.presetCustomDesc')}`
    : t(`settings.dockPlugins.presets.${preset.value}.name`)
})

/**
 * Apply a preset.
 *
 * Standard / minimal stop server work, so they ask first (with the exact list of
 * what will be stopped); cancelling applies nothing at all. The server write is
 * ONE request and runs before the local visibility/order change, so a failure
 * leaves the dock exactly as it was — no half-applied preset.
 */
async function onPreset(next: Exclude<DockPreset, 'custom'>): Promise<void> {
  if (presetBusy.value) return
  const plan = DOCK_PRESET_PLANS[next]

  if (PRESETS_NEEDING_CONFIRM.includes(next)) {
    const lines = plan.features
      .filter((f) => !f.value && DESTRUCTIVE_FEATURES.includes(f.key))
      .map((f) => t(`settings.dockPlugins.consequences.${f.key.split('.')[0]}`))
    const ok = await confirm(
      [t('settings.dockPlugins.presetConfirmIntro'), ...lines].join('\n'),
      {
        title: t('settings.dockPlugins.presetConfirmTitle', {
          name: t(`settings.dockPlugins.presets.${next}.name`),
        }),
        confirmText: t('settings.dockPlugins.presetConfirmOk'),
        dangerous: true,
      },
    )
    if (!ok) return
  }

  presetBusy.value = true
  try {
    await applyPreset(next, async (changes) => {
      await patchConfig(toConfigChanges(changes))
    })
    toast.show(t('settings.dockPlugins.presetApplied', {
      name: t(`settings.dockPlugins.presets.${next}.name`),
    }), { icon: 'success', type: 'success' })
  } catch (err) {
    appLog.e(TAG, `failed to apply preset ${next}`, err)
    toast.show(t('settings.dockPlugins.saveFailed'), { icon: 'warning', type: 'error' })
  } finally {
    presetBusy.value = false
  }
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

.preset-card:disabled {
  cursor: default;
  opacity: var(--opacity-muted);
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

.preset-card__head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.preset-card__icon {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.preset-card.is-active .preset-card__icon {
  color: var(--accent-color);
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

.plugin-row.is-busy {
  opacity: var(--opacity-muted);
}

.plugin-row:not(.is-first) {
  border-top: 0.5px solid var(--border-color);
}

.plugin-drag-handle {
  flex-shrink: 0;
  /* Fixed width (the icon's own size) so every row's icon stays aligned. */
  width: var(--icon-size-md);
  text-align: center;
  cursor: grab;
  color: var(--text-muted);
  /* 2xl, matching the agents list's handle: this is a glyph, not row text, so
     it must not sit on the row-label step (see settingsRowTypography sweep). */
  font-size: var(--font-size-2xl);
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

/* Why this row's switch is inert. Same muted tone as the description: it is
   an explanation, not an error. */
.plugin-row__reason {
  margin-top: calc(2 * var(--dp));
  font-size: var(--font-size-sm);
  line-height: 1.35;
  color: var(--text-secondary);
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

/* Permanent tabs keep the switch for a uniform row shape, but it is inert and
   greyed out: there is no way to hide them. Same for a row whose capability is
   missing on this host. */
.plugin-switch--locked {
  cursor: default;
  opacity: var(--opacity-muted);
}

.plugin-switch--locked .settings-item__switch-input:checked + .settings-item__switch-track {
  background: var(--bg-tertiary);
}
</style>
