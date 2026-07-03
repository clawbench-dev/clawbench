import { reactive, ref } from 'vue'
import { apiGet, apiPatch, apiPost } from '@/utils/api'
import i18n, { STORAGE_KEY as LOCALE_KEY, setLocaleCookie } from '@/i18n'
import { useAgents } from '@/composables/useAgents'

const LOCAL_PREFIX = 'clawbench-settings-'

/** One-time migration: copy legacy localStorage keys to new prefixed keys. */
function migrateLegacyKeys() {
  const migrations: Record<string, { key: string; format: 'raw' | 'json' }> = {
    theme: { key: 'theme', format: 'raw' },
    locale: { key: LOCALE_KEY, format: 'raw' },
    autoSpeech: { key: 'clawbench-auto-speech', format: 'raw' },
    showHidden: { key: 'clawbenchShowHidden', format: 'json' },
    wordWrap: { key: 'clawbench-word-wrap', format: 'raw' },
    lineNumbers: { key: 'clawbench-line-numbers', format: 'raw' },
    fileView: { key: 'clawbench-file-view', format: 'raw' },
    terminalFontSize: { key: 'clawbench-terminal-font-size', format: 'raw' },
  }
  for (const [settingsKey, legacy] of Object.entries(migrations)) {
    const newKey = LOCAL_PREFIX + settingsKey
    // Only migrate if new key doesn't exist yet but legacy key does
    if (localStorage.getItem(newKey) !== null) continue
    const value = localStorage.getItem(legacy.key)
    if (value === null) continue
    try {
      if (legacy.format === 'json') {
        localStorage.setItem(newKey, value) // already JSON
      } else {
        // Convert raw string to JSON for consistency
        const bool = value === 'true' || value === 'false'
        const num = Number(value)
        const parsed = bool ? value === 'true' : (!isNaN(num) && value !== '' ? num : value)
        localStorage.setItem(newKey, JSON.stringify(parsed))
      }
    } catch { /* ignore */ }
  }
}
// Run migration on module load
migrateLegacyKeys()

/** Deep-merge source into target (mutates target). Only overwrites leaf values. */
function deepAssign(target: Record<string, any>, source: Record<string, any>) {
  for (const key of Object.keys(source)) {
    if (
      source[key] !== null &&
      typeof source[key] === 'object' &&
      !Array.isArray(source[key]) &&
      target[key] !== null &&
      typeof target[key] === 'object' &&
      !Array.isArray(target[key])
    ) {
      deepAssign(target[key], source[key])
    } else {
      target[key] = source[key]
    }
  }
}

/**
 * Mapping from settings key → legacy localStorage key + write format.
 * Each entry tells setLocalConfig() how to also write to the key that
 * the actual feature reads from, so changes take effect immediately.
 */
const legacyKeys: Record<string, {
  key: string                    // legacy localStorage key
  format: 'raw' | 'json'        // raw = string value, json = JSON.stringify
  sideEffect?: (value: any) => void  // runtime side-effect for immediate effect
}> = {
  theme: {
    key: 'theme',
    format: 'raw',
    sideEffect(value: string) {
      // Resolve 'auto' to actual theme
      const resolved = value === 'auto'
        ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
        : value
      document.documentElement.setAttribute('data-theme', resolved)
      document.documentElement.setAttribute('data-hljs-theme', resolved)
      // Notify App.vue to sync its `theme` ref (used by provide/inject for chat rendering)
      window.dispatchEvent(new CustomEvent('clawbench-theme-change', { detail: resolved }))
    },
  },
  locale: {
    key: LOCALE_KEY,  // 'clawbench-locale'
    format: 'raw',
    sideEffect(value: string) {
      i18n.global.locale.value = value as any
      setLocaleCookie(value)
    },
  },
  autoSpeech: {
    key: 'clawbench-auto-speech',
    format: 'raw',
    sideEffect(value: boolean) {
      // Notify useAutoSpeech singleton to sync its `enabled` ref
      window.dispatchEvent(new CustomEvent('clawbench-autospeech-change', { detail: value }))
    },
  },
  showHidden: {
    key: 'clawbenchShowHidden',
    format: 'json',
    sideEffect(value: boolean) {
      // Notify App.vue to sync its `showHidden` ref
      window.dispatchEvent(new CustomEvent('clawbench-showhidden-change', { detail: value }))
    },
  },
  wordWrap: {
    key: 'clawbench-word-wrap',
    format: 'raw',
  },
  lineNumbers: {
    key: 'clawbench-line-numbers',
    format: 'raw',
  },
  fileView: {
    key: 'clawbench-file-view',
    format: 'raw',
  },
  terminalFontSize: {
    key: 'clawbench-terminal-font-size',
    format: 'raw',
  },
  androidLogCapture: {
    key: '',
    format: 'raw',
  },
  swipeSession: {
    key: '',
    format: 'raw',
  },
  stickyScroll: {
    key: '',
    format: 'raw',
  },
  preventScreenLock: {
    key: '',
    format: 'raw',
  },
  pushPersistentNotification: {
    key: '',
    format: 'raw',
    sideEffect(value: boolean) {
      try {
        const native = (window as any).AndroidNative
        if (native?.setPushPersistentNotification) native.setPushPersistentNotification(value)
      } catch { /* not in app mode */ }
    },
  },
  sortField: {
    key: '',
    format: 'raw',
    sideEffect(value: string | null) {
      window.dispatchEvent(new CustomEvent('clawbench-sort-change', { detail: { field: value } }))
      // Reset sortDir when sort is cleared
      if (value === null && localConfig.sortDir !== 'asc') {
        localConfig.sortDir = 'asc'
        try { localStorage.setItem(LOCAL_PREFIX + 'sortDir', JSON.stringify('asc')) } catch { /* ignore */ }
        window.dispatchEvent(new CustomEvent('clawbench-sort-change', { detail: { dir: 'asc' } }))
      }
    },
  },
  sortDir: {
    key: '',
    format: 'raw',
    sideEffect(value: string) {
      window.dispatchEvent(new CustomEvent('clawbench-sort-change', { detail: { dir: value } }))
    },
  },
  uiScale: {
    key: '',
    format: 'raw',
    sideEffect(value: number) {
      applyUIScale(value)
    },
  },
}

/** Read initial value from prefixed key (falls back to legacy key, then default) */
function readLocalValue(settingsKey: string, defaultValue: any): any {
  // Try our own prefixed key first (canonical location after migration)
  try {
    const saved = localStorage.getItem(LOCAL_PREFIX + settingsKey)
    if (saved !== null) return JSON.parse(saved)
  } catch { /* ignore */ }
  // Fallback: try legacy key (for values not yet migrated)
  const legacy = legacyKeys[settingsKey]
  if (legacy?.key) {
    try {
      const saved = localStorage.getItem(legacy.key)
      if (saved !== null) {
        if (legacy.format === 'json') {
          return JSON.parse(saved)
        }
        // Raw format: may need type coercion
        if (defaultValue === true || defaultValue === false) {
          return saved === 'true'
        }
        if (typeof defaultValue === 'number') {
          const n = Number(saved)
          if (!isNaN(n)) return n
        }
        return saved
      }
    } catch { /* ignore */ }
  }
  return defaultValue
}

/** Apply global UI scale via CSS zoom — true browser-zoom behavior, no position:fixed breakage. */
export function applyUIScale(scale: number) {
  const el = document.documentElement
  const s = Math.max(0.5, Math.min(2, scale))
  if (s === 1) {
    el.style.zoom = ''
  } else {
    el.style.zoom = String(s)
  }
}

/** Read the current CSS zoom factor applied to <html>. Returns 1 if not set. */
export function getUIScale(): number {
  const z = document.documentElement.style.zoom
  if (!z) return 1
  const n = Number(z)
  return isNaN(n) ? 1 : n
}

/**
 * Convert a value from getBoundingClientRect() / window.innerWidth coordinate
 * space to position:fixed CSS pixel value.
 *
 * Under CSS zoom on <html>, getBoundingClientRect() returns zoom-scaled
 * coordinates and window.innerWidth/innerHeight are NOT scaled, while
 * position:fixed CSS values are in the pre-zoom layout space:
 *   - fixed left:100px under zoom:2 visually appears at 200px
 *   - getBoundingClientRect().left of that element returns 200
 *
 * Therefore: fixedCSS = viewportCoord / zoom
 *
 * Example: to right-align a fixed popup to an anchor:
 *   right: toFixedCSS(innerWidth - anchorRect.right) + 'px'
 */
export function toFixedCSS(viewportCoord: number): number {
  const z = getUIScale()
  return viewportCoord / z
}

/**
 * Get viewport dimensions for position:fixed calculations under CSS zoom.
 *
 * Returns window.innerWidth/innerHeight (NOT affected by CSS zoom),
 * in the same coordinate space as getBoundingClientRect().
 *
 * IMPORTANT: Values from this function are in getBoundingClientRect() space.
 * To use them as position:fixed CSS values, pass through toFixedCSS().
 */
export function getZoomedViewport(): { width: number; height: number } {
  return {
    width: window.innerWidth,
    height: window.innerHeight,
  }
}

const localDefaults: Record<string, any> = {
  theme: 'auto',
  locale: 'zh',
  autoSpeech: false,
  showHidden: false,
  wordWrap: false,
  lineNumbers: true,
  stickyScroll: true,
  fileView: 'list',
  terminalFontSize: 12,
  androidLogCapture: false,
  swipeSession: false,
  preventScreenLock: true,
  pushPersistentNotification: true,
  sortField: null,
  sortDir: 'asc',
  uiScale: 1,
}

// Build reactive local config from legacy localStorage + defaults
const localConfig = reactive<Record<string, any>>({})
for (const key of Object.keys(localDefaults)) {
  localConfig[key] = readLocalValue(key, localDefaults[key])
}

/** Set a local config value, persisting to both prefixed and legacy localStorage keys. */
export function setLocalConfig(key: string, value: any) {
  localConfig[key] = value

  // Write to our own prefixed key (for persistence)
  try {
    localStorage.setItem(LOCAL_PREFIX + key, JSON.stringify(value))
  } catch { /* ignore */ }

  // Write to the legacy key that the actual feature reads from
  const legacy = legacyKeys[key]
  if (legacy?.key) {
    try {
      if (legacy.format === 'json') {
        localStorage.setItem(legacy.key, JSON.stringify(value))
      } else {
        localStorage.setItem(legacy.key, String(value))
      }
    } catch { /* ignore */ }
  }

  // Run side-effect for immediate runtime change
  if (legacy?.sideEffect) {
    legacy.sideEffect(value)
  }
}

export { localConfig }

const serverConfig = ref<Record<string, any>>({})

/**
 * Server config defaults mirroring backend ApplyDefaults() in internal/model/defaults.go.
 * Used as fallback when the API hasn't loaded yet, so items always display meaningful values.
 */
const serverDefaults: Record<string, any> = {
  'chat.initial_messages': 20,
  'chat.page_size': 20,
  'chat.system_prompt_interval': 10,
  'session.max_count': 10,
  'recent_projects.max_count': 10,
  'upload.max_size_mb': 100,
  'upload.max_files': 20,
  'terminal.enabled': true,
  'terminal.idle_timeout': '10m',
  'terminal.max_sessions': 10,
  'terminal.buffer_lines': 2000,
  'default_agent': '',
  'localhost_auth_exempt': true,
  'tts.engine': 'edge',
  'tts.format': '',
  'tts.speed': 1.0,
  'tts.max_cache_files': 100,
  'rag.base_url': 'http://localhost:11434',
  'rag.model': 'bge-m3',
  'rag.api_key': '',
  'rag.chunk_size': 512,
  'rag.search_limit': 5,
  'rag.search_pool_size': 20,
  'rag.retention_days': 90,
  'tts.piper.noise_scale': 0.667,
  'tts.piper.length_scale': 1.0,
  'tts.piper.sentence_silence': 0.2,
  'tts.kokoro.lang': 'cmn',
  'tts.moss_nano.backend': 'onnx',
  'summarize.backend': 'simple',
  'summarize.model': '',
  'summarize.api.format': 'openai',
  'port_forward.allowed_ports': '1024-65535',
}

// ── Agent preference helpers ──────────────────────────────
// Agent model and thinking effort preferences are stored server-side
// in agent YAML files via PATCH /api/agents.

/** Patch an agent's preferred_model or preferred_thinking_effort on the server. */
export async function patchAgentPref(agentId: string, field: 'preferred_model' | 'preferred_thinking_effort' | 'preferred_mode' | 'transport', value: string): Promise<void> {
  await apiPatch('/api/agents', { id: agentId, [field]: value })
  // Also update the agent object in useAgents so the UI reflects immediately
  const { updateAgentField } = useAgents()
  const fieldMap: Record<string, string> = {
    preferred_model: 'preferredModel',
    preferred_thinking_effort: 'preferredThinkingEffort',
    preferred_mode: 'preferredMode',
    transport: 'transport',
  }
  updateAgentField(agentId, fieldMap[field] || field, value)
}

/**
 * Patch an agent's settings-panel configurable field on the server.
 * Supports: name, icon, specialty, custom_system_prompt, sort_order,
 * plus the original preferred_model/preferred_thinking_effort/transport.
 */
export async function patchAgentField(agentId: string, field: string, value: any): Promise<void> {
  await apiPatch('/api/agents', { id: agentId, [field]: value })
  const { updateAgentField } = useAgents()
  const fieldMap: Record<string, string> = {
    preferred_model: 'preferredModel',
    preferred_thinking_effort: 'preferredThinkingEffort',
    preferred_mode: 'preferredMode',
    transport: 'transport',
    custom_system_prompt: 'customSystemPrompt',
    sort_order: 'sortOrder',
    // name, icon, specialty map to themselves
  }
  updateAgentField(agentId, fieldMap[field] || field, value)
  // When custom_system_prompt changes, reload agents to get the server-composed
  // systemPrompt (commonPrompt + customSystemPrompt) into the reactive store.
  if (field === 'custom_system_prompt') {
    const { loadAgents } = useAgents()
    await loadAgents(true)
  }
}

/** Read the preferred model ID for an agent from the server-side agent data. */
function getAgentModelPref(agentId: string): string | null {
  const { getAgent } = useAgents()
  const agent = getAgent(agentId)
  return agent?.preferredModel || null
}

/** Read the preferred thinking effort for an agent from the server-side agent data. */
function getAgentThinkingPref(agentId: string): string | null {
  const { getAgent } = useAgents()
  const agent = getAgent(agentId)
  return agent?.preferredThinkingEffort || null
}

export function useSettingsConfig() {
  /** Sync local-only settings from Android native to keep WebView and native state in sync. */
  function syncNativeSettings() {
    try {
      const native = (window as any).AndroidNative
      if (native?.isPushPersistentNotification) {
        const nativeValue = native.isPushPersistentNotification()
        if (localConfig.pushPersistentNotification !== nativeValue) {
          localConfig.pushPersistentNotification = nativeValue
          try {
            localStorage.setItem(LOCAL_PREFIX + 'pushPersistentNotification', JSON.stringify(nativeValue))
          } catch { /* ignore */ }
        }
      }
    } catch { /* not in app mode */ }
  }

  async function loadConfig() {
    try {
      const data = await apiGet<Record<string, any>>('/api/config')
      serverConfig.value = data
    } catch {
      // Server may be unreachable — keep existing cached values
    }
    // Sync native state after server config loads (app mode only)
    syncNativeSettings()
  }

  async function patchConfig(changes: Record<string, any>): Promise<{ needsRestart: boolean; changedColdFields: string[] }> {
    const result = await apiPatch<{ needs_restart?: boolean; changed_cold_fields?: string[] }>('/api/config', changes)
    // Deep-merge patched values into local cache after successful response.
    // Using Object.assign would overwrite nested objects (e.g. {chat: {page_size: 50}}
    // would lose the existing initial_messages), so we deep-merge instead.
    deepAssign(serverConfig.value, changes)
    return {
      needsRestart: result.needs_restart ?? false,
      changedColdFields: result.changed_cold_fields ?? [],
    }
  }

  async function restartServer() {
    await apiPost('/api/config/restart', {})
  }

  /** Read a server config value by dot-path (e.g. "server.port") */
  function getServerValue(dotPath: string): any {
    const parts = dotPath.split('.')
    let current: any = serverConfig.value
    for (const p of parts) {
      if (current == null || typeof current !== 'object') return undefined
      current = current[p]
    }
    return current
  }

  /** Read a server config value by dot-path, falling back to built-in defaults */
  function getServerValueWithDefault(dotPath: string): any {
    const value = getServerValue(dotPath)
    if (value !== undefined) return value
    return serverDefaults[dotPath]
  }

  /** Write a server config value by dot-path and patch the server */
  async function setServerValue(dotPath: string, value: any): Promise<{ needsRestart: boolean; changedColdFields: string[] }> {
    const parts = dotPath.split('.')
    const changes: Record<string, any> = {}
    // Build nested object for patch (e.g. "server.port" → { server: { port: val } })
    let obj: any = changes
    for (let i = 0; i < parts.length - 1; i++) {
      obj[parts[i]] = {}
      obj = obj[parts[i]]
    }
    obj[parts[parts.length - 1]] = value

    // Save old value for rollback on failure
    const oldValue = getServerValue(dotPath)

    // Optimistic local cache update
    let current: any = serverConfig.value
    for (let i = 0; i < parts.length - 1; i++) {
      if (current[parts[i]] == null) current[parts[i]] = {}
      current = current[parts[i]]
    }
    current[parts[parts.length - 1]] = value

    try {
      return await patchConfig(changes)
    } catch (err) {
      // Rollback local cache on failure
      let rollbackTarget: any = serverConfig.value
      for (let i = 0; i < parts.length - 1; i++) {
        if (rollbackTarget[parts[i]] == null) break
        rollbackTarget = rollbackTarget[parts[i]]
      }
      if (rollbackTarget && typeof rollbackTarget === 'object') {
        rollbackTarget[parts[parts.length - 1]] = oldValue
      }
      throw err
    }
  }

  return {
    serverConfig,
    localConfig,
    loadConfig,
    patchConfig,
    restartServer,
    setLocalConfig,
    getServerValue,
    getServerValueWithDefault,
    setServerValue,
    patchAgentPref,
    patchAgentField,
    getAgentModelPref,
    getAgentThinkingPref,
  }
}
