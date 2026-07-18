import { ref, reactive, computed, watch, onUnmounted } from 'vue'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import i18n from '@/i18n'
import type { GroupPanelConfig, ItemSpec } from '@/components/settings/settingsFieldMap'
import { isDependsOnMet } from '@/components/settings/settingsFieldMap'

/**
 * Reusable composable for panel snapshot/diff/save logic.
 * Extracted from SettingsDrillDown for use by SettingsGroupPanel.
 *
 * Handles:
 * - Snapshot initialization from server/local config
 * - Change detection (hasChanges, canSave, needsRestartHint)
 * - Save execution (server PATCH + local config write)
 * - C1 fix: watches serverConfig for externally changed keys and re-syncs
 */
export function usePanelSnapshot(config: GroupPanelConfig) {
  const { patchConfig, getServerValueWithDefault, setLocalConfig, localConfig, serverConfig } = useSettingsConfig()

  const localValues = reactive<Record<string, unknown>>({})
  const snapshot = ref<Record<string, unknown>>({})
  const saving = ref(false)
  const serverError = ref('')
  const hotReloadWarning = ref('')
  const hasFailedSave = ref(false)

  // ── Field key helpers ──

  /** Find field spec by key across all config fields */
  function findFieldSpec(key: string): ItemSpec | undefined {
    if (config.entrySelector?.key === key) return config.entrySelector
    for (const f of config.commonFields) {
      if (f.key === key) return f
    }
    for (const osf of config.optionSubFields ?? []) {
      for (const f of osf.fields) {
        if (f.key === key) return f
      }
    }
    return undefined
  }

  /** Get all field keys in the config */
  function getAllFieldKeys(): string[] {
    const keys: string[] = []
    if (config.enableKey) keys.push(config.enableKey)
    if (config.entrySelector) keys.push(config.entrySelector.key)
    for (const f of config.commonFields) keys.push(f.key)
    for (const osf of config.optionSubFields ?? []) {
      for (const f of osf.fields) keys.push(f.key)
    }
    return keys
  }

  // ── Snapshot on init ──

  function initSnapshot() {
    const snap: Record<string, unknown> = {}

    // Enable key
    if (config.enableKey) {
      snap[config.enableKey] = getServerValueWithDefault(config.enableKey)
    }

    // Entry selector key
    if (config.entrySelector) {
      snap[config.entrySelector.key] = getServerValueWithDefault(config.entrySelector.key)
    }

    // Common fields
    for (const f of config.commonFields) {
      snap[f.key] = f.source === 'server' ? getServerValueWithDefault(f.key) : localConfig[f.key]
    }

    // Option sub-fields (snapshot all possible sub-fields)
    for (const osf of config.optionSubFields ?? []) {
      for (const f of osf.fields) {
        snap[f.key] = f.source === 'server' ? getServerValueWithDefault(f.key) : localConfig[f.key]
      }
    }

    snapshot.value = snap
    Object.assign(localValues, snap)

    // Init side effects (e.g., fetch FRP info)
    config.onInit?.()
  }

  // ── C1 fix: watch serverConfig for externally changed keys ──

  const stopWatch = watch(serverConfig, () => {
    // Skip if snapshot hasn't been initialized yet
    if (Object.keys(snapshot.value).length === 0) return
    for (const key of getAllFieldKeys()) {
      // Only re-sync if user hasn't locally modified this value
      if (localValues[key] === snapshot.value[key]) {
        const serverVal = getServerValueWithDefault(key)
        if (serverVal !== undefined) {
          snapshot.value[key] = serverVal
          localValues[key] = serverVal
        }
      }
    }
  }, { deep: true })

  onUnmounted(() => {
    stopWatch()
  })

  // ── Diff & validation ──

  const hasChanges = computed(() => {
    const snap = snapshot.value
    for (const key of getAllFieldKeys()) {
      if (localValues[key] !== snap[key]) return true
    }
    return false
  })

  /** dependsOn filtering — delegates to shared utility */
  function checkDependsOnMet(dependsOn: ItemSpec['dependsOn']): boolean {
    return isDependsOnMet(dependsOn, (k) => localValues[k])
  }

  /** Get currently visible field keys (respecting dependsOn and optionSubFields) */
  function getVisibleFieldKeys(subFieldValue: unknown): Set<string> {
    const keys = new Set<string>()
    for (const f of config.commonFields) {
      if (checkDependsOnMet(f.dependsOn)) keys.add(f.key)
    }
    const osf = (config.optionSubFields ?? []).find(o => o.when === subFieldValue)
    if (osf) {
      for (const f of osf.fields) keys.add(f.key)
    }
    return keys
  }

  const canSave = computed(() => {
    const required = config.requiredFields ?? []
    if (required.length === 0) return true
    // If the panel is disabled, skip required-field validation —
    // the user is likely toggling the enable key back on.
    if (config.enableKey && !localValues[config.enableKey]) return true
    // Determine sub-field match value
    const subFieldKey = config.optionSubFieldsKey ?? config.entrySelector?.key
    const subFieldValue = subFieldKey ? localValues[subFieldKey] : undefined
    const visibleKeys = getVisibleFieldKeys(subFieldValue)
    for (const key of required) {
      if (!visibleKeys.has(key)) continue
      const val = localValues[key]
      if (val === '' || val === null || val === undefined) return false
    }
    return true
  })

  const needsRestartHint = computed(() => {
    const snap = snapshot.value
    for (const key of getAllFieldKeys()) {
      if (localValues[key] !== snap[key]) {
        const spec = findFieldSpec(key)
        if (spec?.needsRestart) return true
      }
    }
    return false
  })

  // ── Deep set by dot-path ──

  function deepSetByDotPath(obj: Record<string, unknown>, dotPath: string, value: unknown) {
    const parts = dotPath.split('.')
    let current: Record<string, unknown> = obj
    for (let i = 0; i < parts.length - 1; i++) {
      if (current[parts[i]] == null) current[parts[i]] = {}
      current = current[parts[i]] as Record<string, unknown>
    }
    current[parts[parts.length - 1]] = value
  }

  // ── Save ──

  async function handleSave(): Promise<{ needsRestart: boolean; changedColdFields: string[] }> {
    const snap = snapshot.value
    serverError.value = ''
    hotReloadWarning.value = ''

    const allKeys = getAllFieldKeys()
    const serverChanges: Record<string, unknown> = {}
    const localChanges: [string, unknown][] = []
    const changedKeys: string[] = []

    for (const key of allKeys) {
      const localVal = localValues[key]
      const snapVal = snap[key]
      const spec = findFieldSpec(key)

      if (localVal === snapVal) continue

      changedKeys.push(key)
      // enableKey is always a server-side field
      if (spec?.source === 'server' || key === config.enableKey) {
        deepSetByDotPath(serverChanges, key, localVal)
      } else {
        localChanges.push([key, localVal])
      }
    }

    // Flush local changes
    for (const [key, value] of localChanges) {
      setLocalConfig(key, value as string | number | boolean | null)
    }

    let needsRestart = false
    let changedColdFields: string[] = []

    // Flush server changes
    if (Object.keys(serverChanges).length > 0) {
      saving.value = true
      try {
        const result = await patchConfig(serverChanges)
        saving.value = false

        // Update snapshot to committed state
        for (const key of changedKeys) {
          snapshot.value[key] = localValues[key]
        }

        // Show warnings from hot-reload inline
        if (result.warnings.length > 0) {
          hotReloadWarning.value = result.warnings.join('\n')
        }

        // Side effects
        config.afterSave?.(changedKeys, { ...localValues } as Record<string, unknown>)

        if (result.needsRestart && result.changedColdFields.length > 0) {
          needsRestart = true
          changedColdFields = result.changedColdFields
        }

        hasFailedSave.value = false
      } catch (err: unknown) {
        saving.value = false
        serverError.value = (err instanceof Error ? err.message : '') || i18n.global.t('settings.saveFailed')
        hasFailedSave.value = true
      }
    } else {
      // Only local changes — already flushed, just finalize
      for (const key of changedKeys) {
        snapshot.value[key] = localValues[key]
      }
      config.afterSave?.(changedKeys)
      hasFailedSave.value = false
    }

    return { needsRestart, changedColdFields }
  }

  return {
    localValues,
    snapshot,
    saving,
    serverError,
    hotReloadWarning,
    hasFailedSave,
    hasChanges,
    canSave,
    needsRestartHint,
    initSnapshot,
    handleSave,
    getAllFieldKeys,
    findFieldSpec,
  }
}
