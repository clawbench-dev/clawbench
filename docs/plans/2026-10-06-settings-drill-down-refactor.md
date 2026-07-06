# Settings Drill-Down Refactor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor 6 settings categories (TTS, Summarization, RAG, Port Forward, FRP, Terminal) from flat immediate-save lists into drill-down sub-pages with unified save, enable/disable toggle inside, and required field validation.

**Architecture:** New `SettingsDrillDown.vue` component replaces `ConfigGroupDialog.vue` and `SettingsGroupPanel.vue` for the 6 drill-down categories. `SettingsPage.vue` routes drill-down categories to `SettingsDrillDown` and flat categories to `SettingsCategory`. Fields are locally staged (snapshot + diff), saved as one batch PATCH on "Save" button press. Enable toggle at top of page disables fields (grayed out, not hidden). Entry selectors (TTS engine, Summarize backend) drive conditional field visibility. Unsaved changes or save failure trigger discard confirmation on back navigation.

**Tech Stack:** Vue 3 Composition API, vue-i18n, existing useSettingsConfig/useSettingsNavigation composables

---

## Design Decisions Summary

| Decision | Choice |
|---|---|
| Drill-down categories | tts, summarization, rag, portForward, frp, terminal |
| Navigation | Index → Drill-down (two-level, skip SettingsCategory) |
| Enable toggle placement | Top row inside drill-down page |
| Enable off behavior | Fields grayed out (disabled), not hidden; entry selector also disabled |
| Save mode | Unified batch save (snapshot + diff) with dual-path flush (server→PATCH, local→setLocalConfig) |
| Save bar | Fixed bottom, highlights on changes |
| Unsaved back behavior | Confirm dialog (discard / continue editing) via useDialog |
| Entry selector | TTS engine, Summarize backend as page header selector |
| RAG (no selector, no enable) | Direct field list |
| requiredFields validation | Empty = red border + save disabled |
| Voice on engine switch | Auto-reset to first option |
| needsRestart hint | Text below save button (client-side flag for hint, server response for actual restart) |
| SettingsPage routing | Direct branch: drill-down → SettingsDrillDown, flat → SettingsCategory |
| Index row appearance | No summary, uniform with flat categories |
| Deleted components | ConfigGroupDialog.vue, SettingsGroupPanel.vue |
| Removed mechanisms | groupConfig triggers on ItemSpec, categoryGroups map |
| Hardware back guard | defineExpose({requestBack}) on SettingsDrillDown |
| Password handling | Empty password fields skipped in diff (preserve server value) |
| Tab switch protection | resetState() checks unsaved changes before clearing navStack |
| BottomSheet integration | useTabDrawer('settings', ...) for entry selector picker |
| Category-specific logic | Extracted into useDrillDownSideEffects composable |
| FRP conditional fields | remote_port/ssh_remote_port in optionSubFields (when auto_port=false) |
| Stale snapshot | Accepted risk for single-user app; re-read on save would add latency |

---

### Task 1: Define drill-down category configuration in settingsFieldMap.ts

**Files:**
- Modify: `web/src/components/settings/settingsFieldMap.ts`

**Step 1: Add DrillDownCategory interface and drillDownCategories map**

Add a `DrillDownCategory` interface and a `drillDownCategories` record that configures each of the 6 drill-down pages. Every field **must** include `descriptionKey` (matching existing categoryItems entries) to avoid UX regression.

```ts
export interface DrillDownCategory {
  categoryId: string
  /** Enable toggle field key (undefined if no enable switch, e.g. TTS/RAG/Summarization) */
  enableKey?: string
  /** i18n key for enable toggle label */
  enableLabelKey?: string
  /** Entry selector field (e.g., tts.engine, summarize.backend) */
  entrySelector?: ItemSpec
  /** Common fields always visible (regardless of entry selector value) */
  commonFields: ItemSpec[]
  /** Per-entry-value conditional fields */
  optionSubFields?: { when: any; fields: ItemSpec[] }[]
  /** Dot-path keys of required fields (empty = red border + block save) */
  requiredFields?: string[]
  /** Side-effect hooks after successful save */
  afterSave?: (changedKeys: string[]) => void
}
```

Define `drillDownCategories` (all fields include descriptionKey from current categoryItems):

```ts
export const drillDownCategories: Record<string, DrillDownCategory> = {
  terminal: {
    categoryId: 'terminal',
    enableKey: 'terminal.enabled',
    enableLabelKey: 'settings.items.terminalEnabled',
    commonFields: [
      { labelKey: 'settings.items.terminalFontSize', descriptionKey: 'settings.items.terminalFontSizeDesc', key: 'terminalFontSize', type: 'slider', source: 'local', min: 10, max: 24, step: 1, defaultValue: 12 },
      { labelKey: 'settings.items.terminalIdleTimeout', descriptionKey: 'settings.items.terminalIdleTimeoutDesc', key: 'terminal.idle_timeout', type: 'text', source: 'server' },
      { labelKey: 'settings.items.terminalMaxSessions', descriptionKey: 'settings.items.terminalMaxSessionsDesc', key: 'terminal.max_sessions', type: 'number', source: 'server' },
      { labelKey: 'settings.items.terminalBufferLines', descriptionKey: 'settings.items.terminalBufferLinesDesc', key: 'terminal.buffer_lines', type: 'number', source: 'server' },
    ],
  },
  tts: {
    categoryId: 'tts',
    entrySelector: { labelKey: 'settings.items.ttsEngine', descriptionKey: 'settings.items.ttsEngineDesc', key: 'tts.engine', type: 'select', source: 'server', options: [
      { labelKey: 'settings.items.ttsEngineEdge', value: 'edge' },
      { labelKey: 'settings.items.ttsEnginePiper', value: 'piper' },
      { labelKey: 'settings.items.ttsEngineKokoro', value: 'kokoro' },
      { labelKey: 'settings.items.ttsEngineMossNano', value: 'moss-nano' },
    ]},
    commonFields: [
      { labelKey: 'settings.items.ttsVoice', descriptionKey: 'settings.items.ttsVoiceDesc', key: 'tts.voice', type: 'select', source: 'server' },
      { labelKey: 'settings.items.ttsSpeed', descriptionKey: 'settings.items.ttsSpeedDesc', key: 'tts.speed', type: 'slider', source: 'server', min: 0.5, max: 3, step: 0.1 },
      { labelKey: 'settings.items.ttsMaxCacheFiles', descriptionKey: 'settings.items.ttsMaxCacheFilesDesc', key: 'tts.max_cache_files', type: 'number', source: 'server' },
    ],
    optionSubFields: [
      { when: 'piper', fields: [
        { labelKey: 'settings.items.piperModelPath', descriptionKey: 'settings.items.piperModelPathDesc', key: 'tts.piper.model_path', type: 'text', source: 'server', sectionHeader: 'settings.items.ttsPiperHeader' },
        { labelKey: 'settings.items.piperNoiseScale', descriptionKey: 'settings.items.piperNoiseScaleDesc', key: 'tts.piper.noise_scale', type: 'number', source: 'server', min: 0, max: 1, step: 0.001 },
        { labelKey: 'settings.items.piperLengthScale', descriptionKey: 'settings.items.piperLengthScaleDesc', key: 'tts.piper.length_scale', type: 'number', source: 'server', min: 0.1, max: 5, step: 0.1 },
        { labelKey: 'settings.items.piperSentenceSilence', descriptionKey: 'settings.items.piperSentenceSilenceDesc', key: 'tts.piper.sentence_silence', type: 'number', source: 'server', min: 0, max: 5, step: 0.1 },
      ]},
      { when: 'kokoro', fields: [
        { labelKey: 'settings.items.kokoroModelPath', descriptionKey: 'settings.items.kokoroModelPathDesc', key: 'tts.kokoro.model_path', type: 'text', source: 'server', sectionHeader: 'settings.items.ttsKokoroHeader' },
        { labelKey: 'settings.items.kokoroVoicesPath', descriptionKey: 'settings.items.kokoroVoicesPathDesc', key: 'tts.kokoro.voices_path', type: 'text', source: 'server' },
        { labelKey: 'settings.items.kokoroLang', descriptionKey: 'settings.items.kokoroLangDesc', key: 'tts.kokoro.lang', type: 'text', source: 'server' },
      ]},
      { when: 'moss-nano', fields: [
        { labelKey: 'settings.items.mossNanoModelDir', descriptionKey: 'settings.items.mossNanoModelDirDesc', key: 'tts.moss_nano.model_dir', type: 'text', source: 'server', sectionHeader: 'settings.items.ttsMossNanoHeader' },
        { labelKey: 'settings.items.mossNanoBackend', descriptionKey: 'settings.items.mossNanoBackendDesc', key: 'tts.moss_nano.backend', type: 'select', source: 'server', options: [
          { labelKey: 'settings.items.mossNanoBackendOnnx', value: 'onnx' },
          { labelKey: 'settings.items.mossNanoBackendPytorch', value: 'pytorch' },
        ]},
      ]},
    ],
    requiredFields: ['tts.piper.model_path', 'tts.kokoro.model_path', 'tts.kokoro.voices_path'],
  },
  summarization: {
    categoryId: 'summarization',
    entrySelector: { labelKey: 'settings.items.summarizeBackend', descriptionKey: 'settings.items.summarizeBackendDesc', key: 'summarize.backend', type: 'select', source: 'server', options: [
      { labelKey: 'settings.items.summarizeDisabled', value: '' },
      { labelKey: 'settings.items.summarizeSimple', value: 'simple' },
      { labelKey: 'settings.items.summarizeApi', value: 'api' },
      { labelKey: 'settings.items.summarizeClaude', value: 'claude' },
      { labelKey: 'settings.items.summarizeCodebuddy', value: 'codebuddy' },
      { labelKey: 'settings.items.summarizeOpencode', value: 'opencode' },
      { labelKey: 'settings.items.summarizeCodex', value: 'codex' },
      { labelKey: 'settings.items.summarizeQoder', value: 'qoder' },
      { labelKey: 'settings.items.summarizeVecli', value: 'vecli' },
      { labelKey: 'settings.items.summarizeDeepseek', value: 'deepseek' },
      { labelKey: 'settings.items.summarizePi', value: 'pi' },
    ]},
    commonFields: [],
    optionSubFields: [
      { when: 'api', fields: [
        { labelKey: 'settings.items.summarizeModel', descriptionKey: 'settings.items.summarizeModelDesc', key: 'summarize.model', type: 'text', source: 'server', sectionHeader: 'settings.items.apiHeader' },
        { labelKey: 'settings.items.apiBaseUrl', descriptionKey: 'settings.items.apiBaseUrlDesc', key: 'summarize.api.base_url', type: 'text', source: 'server' },
        { labelKey: 'settings.items.apiKey', descriptionKey: 'settings.items.apiKeyDesc', key: 'summarize.api.key', type: 'password', source: 'server' },
        { labelKey: 'settings.items.apiFormat', descriptionKey: 'settings.items.apiFormatDesc', key: 'summarize.api.format', type: 'select', source: 'server', options: [
          { labelKey: 'settings.items.apiFormatOpenai', value: 'openai' },
          { labelKey: 'settings.items.apiFormatAnthropic', value: 'anthropic' },
        ]},
      ]},
      ...CLI_BACKENDS.map(b => ({
        when: b,
        fields: [
          { labelKey: 'settings.items.summarizeModel', descriptionKey: 'settings.items.summarizeModelDesc', key: 'summarize.model', type: 'text', source: 'server' },
        ],
      })),
    ],
    requiredFields: ['summarize.api.base_url'],
  },
  rag: {
    categoryId: 'rag',
    commonFields: [
      { labelKey: 'settings.items.ragBaseUrl', descriptionKey: 'settings.items.ragBaseUrlDesc', key: 'rag.base_url', type: 'text', source: 'server' },
      { labelKey: 'settings.items.ragModel', descriptionKey: 'settings.items.ragModelDesc', key: 'rag.model', type: 'text', source: 'server' },
      { labelKey: 'settings.items.ragApiKey', descriptionKey: 'settings.items.ragApiKeyDesc', key: 'rag.api_key', type: 'password', source: 'server' },
      { labelKey: 'settings.items.ragChunkSize', descriptionKey: 'settings.items.ragChunkSizeDesc', key: 'rag.chunk_size', type: 'number', source: 'server' },
      { labelKey: 'settings.items.ragSearchLimit', descriptionKey: 'settings.items.ragSearchLimitDesc', key: 'rag.search_limit', type: 'number', source: 'server' },
      { labelKey: 'settings.items.ragSearchPoolSize', descriptionKey: 'settings.items.ragSearchPoolSizeDesc', key: 'rag.search_pool_size', type: 'number', source: 'server' },
      { labelKey: 'settings.items.ragRetentionDays', descriptionKey: 'settings.items.ragRetentionDaysDesc', key: 'rag.retention_days', type: 'number', source: 'server' },
    ],
    requiredFields: ['rag.base_url'],
  },
  portForward: {
    categoryId: 'portForward',
    enableKey: 'port_forward.enabled',
    enableLabelKey: 'settings.items.portForwardEnabled',
    commonFields: [
      { labelKey: 'settings.items.portForwardPort', descriptionKey: 'settings.items.portForwardPortDesc', key: 'port_forward.port', type: 'number', source: 'server', needsRestart: true, displayTransform: (v: any) => v === 0 ? '__auto__' : v },
    ],
  },
  frp: {
    categoryId: 'frp',
    enableKey: 'frp.enabled',
    enableLabelKey: 'settings.items.frpEnabled',
    commonFields: [
      { labelKey: 'settings.items.frpServerAddr', descriptionKey: 'settings.items.frpServerAddrDesc', key: 'frp.server_addr', type: 'text', source: 'server' },
      { labelKey: 'settings.items.frpServerPort', descriptionKey: 'settings.items.frpServerPortDesc', key: 'frp.server_port', type: 'number', source: 'server' },
      { labelKey: 'settings.items.frpToken', descriptionKey: 'settings.items.frpTokenDesc', key: 'frp.token', type: 'password', source: 'server' },
      { labelKey: 'settings.items.frpAutoPort', descriptionKey: 'settings.items.frpAutoPortDesc', key: 'frp.auto_port', type: 'switch', source: 'server' },
    ],
    // FRP remote_port/ssh_remote_port only visible when auto_port=false
    optionSubFields: [
      { when: false, fields: [  // auto_port === false
        { labelKey: 'settings.items.frpRemotePort', descriptionKey: 'settings.items.frpRemotePortDesc', key: 'frp.remote_port', type: 'number', source: 'server' },
        { labelKey: 'settings.items.frpSSHRemotePort', descriptionKey: 'settings.items.frpSSHRemotePortDesc', key: 'frp.ssh_remote_port', type: 'number', source: 'server' },
      ]},
    ],
    requiredFields: ['frp.server_addr'],
  },
}
```

**Step 2: Remove the 6 categories from categoryItems**

Delete the `tts`, `summarization`, `rag`, `portForward`, `frp`, and `terminal` entries from `categoryItems`. Keep `appearance`, `project`, `chat`, `agents`, `files`, `security`, `android`, `about`.

**Step 3: Remove groupConfig mechanism**

Remove the `groupConfig` property from `ItemSpec` interface. Remove the `GroupConfigTrigger` interface. Remove `categoryGroups` and all helper functions: `getAllGroupFields`, `fieldBelongsToGroup`, `getGroupById`, `getCategoryForGroup`. Update `getServerFieldToLabelKey()` to iterate **both** `categoryItems` and `drillDownCategories` (entry selector + commonFields + optionSubFields) so the restart dialog can translate all field paths:

```ts
export function getServerFieldToLabelKey(): Record<string, string> {
  const map: Record<string, string> = {}
  // Flat category items
  for (const items of Object.values(categoryItems)) {
    for (const item of items) {
      if (item.source === 'server') map[item.key] = item.labelKey
    }
  }
  // Drill-down category items
  for (const dd of Object.values(drillDownCategories)) {
    if (dd.enableKey) map[dd.enableKey] = dd.enableLabelKey ?? ''
    if (dd.entrySelector?.source === 'server') map[dd.entrySelector.key] = dd.entrySelector.labelKey
    for (const f of dd.commonFields) {
      if (f.source === 'server') map[f.key] = f.labelKey
    }
    for (const osf of dd.optionSubFields ?? []) {
      for (const f of osf.fields) {
        if (f.source === 'server') map[f.key] = f.labelKey
      }
    }
  }
  return map
}
```

**Step 4: Add isDrillDownCategory helper**

```ts
const DRILL_DOWN_IDS = new Set(Object.keys(drillDownCategories))

export function isDrillDownCategory(categoryId: string): boolean {
  return DRILL_DOWN_IDS.has(categoryId)
}
```

**Step 5: Commit**

```bash
git add web/src/components/settings/settingsFieldMap.ts
git commit -m "refactor: define drill-down category config, remove groupConfig mechanism"
```

---

### Task 2: Create useDrillDownSideEffects composable

**Files:**
- Create: `web/src/composables/useDrillDownSideEffects.ts`

**Step 1: Extract category-specific side-effects**

This composable handles post-save side-effects and special UI logic per category, avoiding `if (categoryId === 'frp')` sprawl in SettingsDrillDown:

```ts
import { useTerminalStatus } from '@/composables/useTerminalStatus'
import { usePortForward } from '@/composables/usePortForward'
import { useFrp } from '@/composables/useFrp'

export function useDrillDownSideEffects(categoryId: string) {
  const { loadTerminalStatus } = useTerminalStatus()
  const { loadSSHInfo } = usePortForward()
  const { frpState, fetchFrpInfo } = useFrp()

  /** Run after successful save. changedKeys = dot-paths that were PATCHed. */
  function afterSave(changedKeys: string[]) {
    if (categoryId === 'terminal' && changedKeys.includes('terminal.enabled')) {
      loadTerminalStatus()
    }
    if (categoryId === 'portForward' && changedKeys.includes('port_forward.enabled')) {
      loadSSHInfo()
    }
    if (categoryId === 'frp') {
      if (changedKeys.includes('frp.enabled')) fetchFrpInfo()
    }
  }

  /** Whether to show FRP status dot (only for frp category) */
  const showFrpStatusDot = computed(() => categoryId === 'frp')

  /** Get FRP status dot color */
  const frpStatusDot = computed(() => {
    if (categoryId !== 'frp' || !frpState.enabled) return undefined
    if (frpState.state === 'running') return 'green' as const
    if (frpState.state === 'starting') return 'yellow' as const
    if (frpState.state === 'failed') return 'red' as const
    return undefined
  })

  /** Whether to auto-reset TTS voice on engine change */
  const needsVoiceReset = computed(() => categoryId === 'tts')

  /** FRP auto_port info items (injected when auto_port=true && enabled=true) */
  const frpAutoPortInfo = computed(() => {
    if (categoryId !== 'frp') return null
    return { state: frpState.state, remotePort: frpState.remotePort, sshRemotePort: frpState.sshRemotePort }
  })

  /** Fetch initial state on mount */
  function init() {
    if (categoryId === 'frp') fetchFrpInfo()
  }

  return { afterSave, showFrpStatusDot, frpStatusDot, needsVoiceReset, frpAutoPortInfo, init }
}
```

**Step 2: Commit**

```bash
git add web/src/composables/useDrillDownSideEffects.ts
git commit -m "feat: add useDrillDownSideEffects composable for category-specific logic"
```

---

### Task 3: Create SettingsDrillDown.vue component

**Files:**
- Create: `web/src/components/settings/SettingsDrillDown.vue`

**Step 1: Implement SettingsDrillDown.vue**

Key behaviors:

- **Props:** `categoryId: string`
- **Emits:** `restartNeeded(fields: string[])`, `back()`
- **Expose:** `requestBack()` — for hardware back handler from SettingsPage
- **State:** `localValues` (reactive Record), `snapshot` (ref Record), `saving` (ref boolean), `serverError` (ref string), `hasFailedSave` (ref boolean)
- **On mount:** Snapshot current config values for all fields via dual source (server: `getServerValueWithDefault()`, local: `localConfig[]`). Copy into `localValues`. Call `sideEffects.init()`.
- **Enable toggle:** If `config.enableKey`, render as first row (iOS-style switch with `aria-label`). When off, `fieldsDisabled = true` — all fields AND entry selector are disabled (grayed).
- **Entry selector:** If `config.entrySelector`, render as row with current value + chevron. Tap opens BottomSheet picker via `useTabDrawer('settings', ...)`. On change, update `localValues[entrySelector.key]`. If `sideEffects.needsVoiceReset`, auto-reset voice to first option. **Also disabled when enable toggle is off.**
- **Field list:** Render `commonFields` + matching `optionSubFields`. FRP: entry selector is `frp.auto_port` (special — use `optionSubFields` with `when: false` for remote ports). Inject section headers from `sectionHeader`. Use `SettingsItem` bound to `localValues`. Each field gets `:description="entry.descriptionKey ? t(entry.descriptionKey) : ''"`.
- **FRP special:** After `frp.auto_port` field, if `frpAutoPortInfo` is available and `auto_port=true && enabled=true`, inject read-only info items for assigned ports. Show `frpStatusDot` next to enable toggle.
- **Required validation:** For fields in `requiredFields`, if the value is empty/null, show red border and `canSave = false`.
- **needsRestart hint:** Client-side: if any changed field has `needsRestart: true` on its ItemSpec, show hint text. Server-side: after save, emit `restartNeeded` from PATCH response.
- **Diff on save — CRITICAL password handling:** When diffing `localValues` vs `snapshot`, **skip** any `type === 'password'` field where `localValue === '' || localValue === null || localValue === undefined`. This preserves the existing server-side password.
- **Dual-path flush on save:**
  1. Diff `localValues` vs `snapshot` for all **server-source** fields (skip empty passwords). Build nested PATCH payload. Call `patchConfig()`.
  2. Diff `localValues` vs `snapshot` for all **local-source** fields. For each changed local field, call `setLocalConfig(key, value)` for immediate effect.
  3. If PATCH succeeded, emit `restartNeeded` from response, call `sideEffects.afterSave(changedKeys)`, emit `back()`.
  4. If PATCH failed, set `serverError`, set `hasFailedSave = true`, stay on page.
- **Save button (fixed bottom):** `disabled` when `!hasChanges || !canSave || saving`. On click calls `handleSave()`.
- **Discard confirmation on back:** `requestBack()` method: if `hasChanges || hasFailedSave`, show `useDialog().confirm()` with title=`settings.drillDown.unsavedTitle`, message=`settings.drillDown.unsavedMessage`, buttons=[`settings.drillDown.discard`, `settings.drillDown.continueEditing`]. If discard confirmed, `emit('back')`. If no unsaved changes, `emit('back')` directly.
- **`useTabDrawer` for BottomSheet:** Use `useTabDrawer('settings', entryPickerOpen)` with the reset-on-deactivation watch (same pattern as SettingsGroupPanel lines 126-130).

Template structure:

```vue
<template>
  <div class="drill-down">
    <!-- Enable toggle row -->
    <div v-if="config?.enableKey" class="drill-down__enable-row">
      <span class="drill-down__enable-label">{{ t(config.enableLabelKey!) }}</span>
      <span v-if="sideEffects.frpStatusDot.value" class="drill-down__status-dot" :class="'drill-down__status-dot--' + sideEffects.frpStatusDot.value" />
      <label class="drill-down__switch" @click.stop>
        <input type="checkbox" class="drill-down__switch-input" :checked="!!localValues[config.enableKey!]" @change="onEnableToggle" aria-label="Enable" />
        <span class="drill-down__switch-track"></span>
      </label>
    </div>
    <!-- Entry selector row (also disabled when enable off) -->
    <div v-if="config?.entrySelector" class="drill-down__entry-row" :class="{ 'drill-down__entry-row--disabled': fieldsDisabled }" @click="!fieldsDisabled && (entryPickerOpen = true)">
      <span class="drill-down__entry-label">{{ t(config.entrySelector.labelKey) }}</span>
      <div class="drill-down__entry-right">
        <span class="drill-down__entry-value">{{ entryDisplayLabel }}</span>
        <ChevronRight :size="14" class="drill-down__entry-chevron" />
      </div>
    </div>
    <!-- Field list with section headers -->
    <template v-for="entry in renderFields" :key="entry.type === 'header' ? entry.headerKey : entry.field.key">
      <div v-if="entry.type === 'header'" class="drill-down__section-header">{{ entry.label }}</div>
      <SettingsItem
        v-else
        :label="t(entry.field.labelKey)"
        :description="entry.field.descriptionKey ? t(entry.field.descriptionKey) : ''"
        :type="entry.field.type"
        :model-value="getFieldValue(entry.field)"
        :options="resolveOptions(entry.field)"
        :min="entry.field.min"
        :max="entry.field.max"
        :step="entry.field.step"
        :disabled="fieldsDisabled"
        :needs-restart="entry.field.needsRestart"
        :default-value="entry.field.defaultValue"
        :display-format="entry.field.displayFormat"
        @update:model-value="(v: any) => setFieldValue(entry.field.key, v)"
      />
    </template>
    <!-- FRP auto_port info injection -->
    <template v-if="frpInfoItems.length">
      <SettingsItem v-for="info in frpInfoItems" :key="info.key" :label="info.label" :type="'info'" :model-value="info.value" />
    </template>
    <!-- Fixed bottom save bar -->
    <div class="drill-down__save-bar">
      <div v-if="needsRestartHint" class="drill-down__restart-hint">{{ t('settings.drillDown.needsRestartHint') }}</div>
      <button class="drill-down__save-btn" :class="{ 'drill-down__save-btn--active': hasChanges }" :disabled="!canSave" @click="handleSave">
        {{ saving ? t('settings.drillDown.saving') : t('settings.drillDown.save') }}
      </button>
    </div>
    <!-- Server error -->
    <div v-if="serverError" class="drill-down__error">{{ serverError }}</div>
    <!-- Entry selector BottomSheet via useTabDrawer -->
    <BottomSheet v-if="config?.entrySelector" :open="entryPicker.effectiveOpen.value" :title="t(config.entrySelector.labelKey)" compact @close="entryPicker.close()">
      <div v-for="opt in entryOptions" :key="opt.value" class="drill-down__option" :class="{ 'drill-down__option--active': localValues[config!.entrySelector!.key] === opt.value }" @click="selectEntry(opt.value)">
        <span>{{ t(opt.labelKey) }}</span>
        <span v-if="localValues[config!.entrySelector!.key] === opt.value" class="drill-down__option-check">✓</span>
      </div>
    </BottomSheet>
  </div>
</template>
```

**Step 2: Commit**

```bash
git add web/src/components/settings/SettingsDrillDown.vue
git commit -m "feat: add SettingsDrillDown with unified save, enable toggle, entry selector, password skip, dual-path flush"
```

---

### Task 4: Wire SettingsDrillDown into SettingsPage routing

**Files:**
- Modify: `web/src/components/settings/SettingsPage.vue`
- Modify: `web/src/composables/useSettingsNavigation.ts`

**Step 1: Import SettingsDrillDown and add routing branch in SettingsPage.vue**

```vue
<SettingsIndex v-if="navStack.length === 0" @navigate="pushNav" />
<SettingsDrillDown
  v-else-if="isDrillDownCategory(currentCategory!)"
  ref="drillDownRef"
  :category-id="currentCategory!"
  @restart-needed="handleRestartNeeded"
  @back="popNav"
/>
<SettingsCategory
  v-else
  :category-id="currentCategory!"
  @navigate="pushNav"
  @restart-needed="handleRestartNeeded"
  @restart-requested="handleRestart"
/>
```

**Step 2: Implement hardware back handler with unsaved guard**

Add `drillDownRef` template ref. Override `useFeatureBackHandler`:

```ts
const drillDownRef = ref<InstanceType<typeof SettingsDrillDown> | null>(null)

useFeatureBackHandler(
  'settings',
  () => !!props.active && navStack.value.length > 0,
  () => {
    if (isDrillDownCategory(currentCategory.value!) && drillDownRef.value) {
      drillDownRef.value.requestBack()  // SettingsDrillDown handles unsaved check internally
    } else {
      popNav()
    }
  },
  PRIORITY_PAGE,
)
```

Also update the header back button `@click` to use the same logic instead of always `popNav()`.

**Step 3: Protect resetState() on tab re-activation**

In `useSettingsNavigation.ts`, add a `canReset` check or in SettingsPage's watch, check if the current drill-down has unsaved changes before calling `resetState()`. Simplest approach: add an `unsavedGuard` callback that SettingsDrillDown can set:

```ts
// In useSettingsNavigation:
let beforeReset: (() => boolean) | null = null

export function setBeforeResetGuard(fn: (() => boolean) | null) {
  beforeReset = fn
}

// In resetState:
function resetState() {
  if (beforeReset && !beforeReset()) return  // guard says don't reset
  navStack.value = []
  // ... rest of reset
}
```

SettingsDrillDown sets the guard on mount and clears on unmount.

**Step 4: Commit**

```bash
git add web/src/components/settings/SettingsPage.vue web/src/composables/useSettingsNavigation.ts
git commit -m "feat: wire SettingsDrillDown routing with hardware back guard and resetState protection"
```

---

### Task 5: Remove ConfigGroupDialog and SettingsGroupPanel

**Files:**
- Delete: `web/src/components/settings/ConfigGroupDialog.vue`
- Delete: `web/src/components/settings/SettingsGroupPanel.vue`
- Modify: `web/src/components/settings/SettingsCategory.vue`

**Step 1: Remove ConfigGroupDialog from SettingsCategory**

In SettingsCategory.vue:
- Remove import of `ConfigGroupDialog`
- Remove the `<ConfigGroupDialog>` template block
- Remove `groupDialog` reactive state
- Remove `handleGroupDialogClose`, `handleGroupDialogSaved`
- Remove `groupConfig` check in `handleUpdate`
- Remove `GroupConfigTrigger` from imports

**Step 2: Delete ConfigGroupDialog.vue and SettingsGroupPanel.vue**

**Step 3: Delete test file SettingsGroupPanel.test.ts**

**Step 4: Commit**

```bash
git add web/src/components/settings/
git commit -m "refactor: remove ConfigGroupDialog, SettingsGroupPanel, and their tests (replaced by SettingsDrillDown)"
```

---

### Task 6: Clean up SettingsCategory.vue for flat-only categories

**Files:**
- Modify: `web/src/components/settings/SettingsCategory.vue`

**Step 1: Remove drill-down category logic from SettingsCategory**

Since TTS/Summarization/RAG/PortForward/FRP/Terminal are no longer rendered by SettingsCategory, remove:
- The FRP status dot injection logic (moved to SettingsDrillDown via useDrillDownSideEffects)
- The FRP auto_port info injection logic
- The `frpState`/`fetchFrpInfo` import and watch
- The `loadTerminalStatus`/`loadSSHInfo` logic (moved to useDrillDownSideEffects)
- Any `groupConfig` trigger handling (already removed in Task 5)
- `terminal.enabled`, `port_forward.enabled`, `frp.enabled` toggle side-effects (moved to useDrillDownSideEffects)

SettingsCategory now only renders: appearance, project, chat, agents, files, security, android, about.

**Step 2: Commit**

```bash
git add web/src/components/settings/SettingsCategory.vue
git commit -m "refactor: clean SettingsCategory for flat-only categories"
```

---

### Task 7: Clean up SettingsIndex.vue

**Files:**
- Modify: `web/src/components/settings/SettingsIndex.vue`

**Step 1: Remove FRP-specific summary logic**

Remove the `frpSummary` computed, the `<span v-if="cat.id === 'frp' && frpSummary">` template, and the `useFrp` import. All categories now show uniform rows (icon + label + chevron) with no summary text.

**Step 2: Commit**

```bash
git add web/src/components/settings/SettingsIndex.vue
git commit -m "refactor: remove FRP summary from SettingsIndex, uniform row appearance"
```

---

### Task 8: Add i18n keys for drill-down UI

**Files:**
- Modify: `web/src/i18n/locales/zh.ts`
- Modify: `web/src/i18n/locales/en.ts`

**Step 1: Add drill-down i18n keys**

Under `settings`, add:

```ts
drillDown: {
  save: '保存',           // en: 'Save'
  saving: '保存中...',     // en: 'Saving...'
  needsRestartHint: '更改需要重启服务器生效',  // en: 'Changes require server restart to take effect'
  unsavedTitle: '未保存的更改',   // en: 'Unsaved Changes'
  unsavedMessage: '您有未保存的更改，是否丢弃？',  // en: 'You have unsaved changes. Discard them?'
  discard: '丢弃',         // en: 'Discard'
  continueEditing: '继续编辑',  // en: 'Continue Editing'
  saved: '配置已保存',     // en: 'Configuration saved'
  saveFailed: '保存失败',  // en: 'Save failed'
},
```

Remove the `groupConfig` i18n block (`frpTitle`, `summarizeApiTitle`, `ttsPiperTitle`, `ttsKokoroTitle`, `requiredHint`, `confirm`, `saved`).

**Step 2: Commit**

```bash
git add web/src/i18n/locales/
git commit -m "feat: add drill-down i18n keys, remove groupConfig keys"
```

---

### Task 9: Rewrite settingsFieldMap tests

**Files:**
- Modify: `web/src/components/settings/__tests__/settingsFieldMap.test.ts`

**Step 1: Rewrite tests for new structure**

Remove tests for `categoryGroups`, `getGroupById`, `getCategoryForGroup`, `groupConfig` triggers, and `categoryItems['frp']`/`categoryItems['tts']` entries.

Add tests for:
- `drillDownCategories` has all 6 keys
- `isDrillDownCategory()` returns true for drill-down IDs, false for flat IDs
- `getServerFieldToLabelKey()` still maps drill-down fields (tts.engine, frp.server_addr, rag.base_url, terminal.enabled, etc.)
- `drillDownCategories.frp.optionSubFields` has `when: false` entry with remote_port/ssh_remote_port
- `drillDownCategories.tts.requiredFields` contains piper/kokoro required paths
- `drillDownCategories.summarization.requiredFields` contains summarize.api.base_url
- `categoryItems` no longer has the 6 drill-down keys
- All drill-down fields have `descriptionKey` set

**Step 2: Commit**

```bash
git add web/src/components/settings/__tests__/settingsFieldMap.test.ts
git commit -m "test: rewrite settingsFieldMap tests for drill-down structure"
```

---

### Task 10: Create SettingsDrillDown tests

**Files:**
- Create: `web/src/components/settings/__tests__/SettingsDrillDown.test.ts`

**Step 1: Write comprehensive tests**

Cover:
- Snapshot creation on mount (server + local fields)
- Diff detection (`hasChanges` computed)
- Password fields skipped in diff (empty password not PATCHed)
- Dual-path flush: server fields → patchConfig, local fields → setLocalConfig
- Enable toggle disables fields and entry selector
- Entry selector changes show correct optionSubFields
- Required field validation blocks save
- Voice auto-reset on TTS engine switch
- Unsaved back dialog (hasChanges triggers confirm)
- Save failure keeps hasFailedSave=true
- needsRestart hint shows when cold field changed
- FRP status dot renders
- FRP auto_port info injection
- port_forward.port display transform (0 → auto)
- All 6 categories render correct fields

**Step 2: Commit**

```bash
git add web/src/components/settings/__tests__/SettingsDrillDown.test.ts
git commit -m "test: add SettingsDrillDown comprehensive tests"
```

---

### Task 11: Update dependent test files

**Files:**
- Modify: `web/src/components/settings/__tests__/SettingsCategory.test.ts`
- Modify: `web/src/components/settings/__tests__/SettingsPage.test.ts`

**Step 1: Update SettingsCategory.test.ts**

Remove tests referencing terminal.enabled toggling, TTS engine switching, FRP fields, PortForward fields. These are now in SettingsDrillDown tests. Update `categoryItems` assertions to only cover flat categories.

**Step 2: Update SettingsPage.test.ts**

Add tests for the drill-down routing branch (SettingsDrillDown rendered for drill-down categories, SettingsCategory for flat categories).

**Step 3: Commit**

```bash
git add web/src/components/settings/__tests__/
git commit -m "test: update SettingsCategory and SettingsPage tests for drill-down refactor"
```

---

### Task 12: Final cleanup and verification

**Files:**
- Various

**Step 1: Verify all imports are clean**

- Ensure no file imports `ConfigGroupDialog`, `SettingsGroupPanel`, `GroupConfigTrigger`, `categoryGroups`, `getAllGroupFields`, `fieldBelongsToGroup`, `getGroupById`, `getCategoryForGroup`
- Ensure `SettingsCategory.vue` no longer references drill-down categories
- Grep for `settings.groupConfig.` in i18n and component files — should be zero results

**Step 2: Verify all 6 drill-down categories**

- Terminal: enable toggle + 4 fields (fontSize local, idle/max/buffer server)
- TTS: engine selector + voice/speed/cache + per-engine fields (piper 4, kokoro 3, moss-nano 2)
- Summarization: backend selector + per-backend fields (api 4, CLI 1 each)
- RAG: 7 fields, no selector, no enable
- Port Forward: enable toggle + port (with display transform)
- FRP: enable toggle + status dot + 4 common fields + 2 conditional (auto_port=false)

**Step 3: Run tests**

```bash
cd web && npm run test -- --run
```

**Step 4: Run dev server and smoke test**

```bash
cd web && npm run dev
```

Navigate to Settings, tap each drill-down category, verify:
- Fields load correctly with descriptions
- Enable toggle disables fields and entry selector
- Entry selector changes show correct sub-fields
- Save button enables on changes
- Required validation works (red border, save disabled)
- Back with unsaved changes shows confirmation
- Successful save navigates back with toast
- Password fields don't get cleared on save
- FRP remote_port only shows when auto_port=false

**Step 5: Commit**

```bash
git commit -m "chore: final cleanup and verification for settings drill-down refactor"
```
