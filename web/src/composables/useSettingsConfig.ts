import { reactive, ref } from 'vue'
import { apiGet, apiPatch, apiPost } from '@/utils/api'
import i18n, { STORAGE_KEY as LOCALE_KEY, setLocaleCookie } from '@/i18n'
import { useAgents } from '@/composables/useAgents'
import { getNative } from '@/utils/clawbenchNative'

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
    stickyScroll: { key: 'clawbench-sticky-scroll', format: 'raw' },
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

/**
 * Mapping from settings key → legacy localStorage key + write format.
 * Each entry tells setLocalConfig() how to also write to the key that
 * the actual feature reads from, so changes take effect immediately.
 */
const legacyKeys: Record<string, {
  key: string                    // legacy localStorage key
  format: 'raw' | 'json'        // raw = string value, json = JSON.stringify
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- contravariant: specific sideEffect types (string, boolean, number) must be assignable
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
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- vue-i18n locale type mismatch
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
  stickyScroll: {
    key: 'clawbench-sticky-scroll',
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
  terminalTheme: {
    key: '',
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
  preventScreenLock: {
    key: '',
    format: 'raw',
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
function readLocalValue(settingsKey: string, defaultValue: string | boolean | number | null): string | boolean | number | null {
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

const localDefaults: Record<string, string | boolean | number | null> = {
  theme: 'auto',
  terminalTheme: 'auto',
  locale: 'zh',
  autoSpeech: false,
  showHidden: false,
  wordWrap: true,
  lineNumbers: false,
  stickyScroll: true,
  fileView: 'list',
  messageDisplayMode: 'summary',
  terminalFontSize: 12,
  logCapture: false,
  swipeSession: false,
  preventScreenLock: true,
  sortField: null,
  sortDir: 'asc',
  uiScale: 1,
  recentFilesCount: 10,
  headerShortcutTips: true,
}

// Build reactive local config from legacy localStorage + defaults
const localConfig = reactive<Record<string, string | boolean | number | null>>({})
for (const key of Object.keys(localDefaults)) {
  localConfig[key] = readLocalValue(key, localDefaults[key])
}

/** Set a local config value, persisting to both prefixed and legacy localStorage keys. */
export function setLocalConfig(key: string, value: string | boolean | number | null) {
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

export { localConfig, serverConfig }

const serverConfig = ref<Record<string, unknown>>({})

/**
 * Server config defaults mirroring backend ApplyDefaults() in internal/model/defaults.go.
 * Used as fallback when the API hasn't loaded yet, so items always display meaningful values.
 */
const serverDefaults: Record<string, unknown> = {
  'chat.initial_messages': 20,
  'chat.page_size': 20,
  'chat.system_prompt_interval': 10,
  'chat.recommend_enabled': false,
  'chat.recommend_context_messages': 3,
  'session.max_count': 10,
  'session.archive_retention_enabled': false,
  'session.archive_retention_days': 30,
  'acp.idle_reclaim_threshold': 10,
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
  'stt.base_url': 'http://localhost:8000/v1',
  'stt.api_key': '',
  'stt.model': 'openai/whisper-large-v3',
  'stt.language': 'zh',
  'stt.streaming': false,
  'stt.chunk_ms': 1000,
  'stt.shortcut_key': 'F9',
  'rag.base_url': 'http://localhost:11434',
  'rag.model': 'bge-m3',
  'rag.api_key': '',
  'rag.chunk_size': 512,
  'rag.chunk_overlap': 64,
  'rag.search_limit': 5,
  'rag.search_pool_size': 20,
  'rag.retention_days': 90,
  'tts.piper.noise_scale': 0.667,
  'tts.piper.length_scale': 1.0,
  'tts.piper.sentence_silence': 0.2,
  'tts.kokoro.lang': 'cmn',
  'tts.moss_nano.backend': 'onnx',
  'summarize.tts_backend': 'simple',
  'ai_summary.model': '',
  'ai_summary.format': 'openai',
  'port_forward.allowed_ports': '1024-65535',
  'frp.enabled': false,
  'frp.server_port': 7000,
  'frp.auto_port': true,
  'frp.remote_port': 0,
  'frp.ssh_remote_port': 0,
  'push_mode': 'native',
  'file_search.display_limit': 100,
  'tls.cert_dir': '',
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
 * Supports: name, specialty, custom_system_prompt, sort_order,
 * plus the original preferred_model/preferred_thinking_effort/transport.
 */
export async function patchAgentField(agentId: string, field: string, value: string | boolean | number | null): Promise<void> {
  await apiPatch('/api/agents', { id: agentId, [field]: value })
  const { updateAgentField } = useAgents()
  const fieldMap: Record<string, string> = {
    preferred_model: 'preferredModel',
    preferred_thinking_effort: 'preferredThinkingEffort',
    preferred_mode: 'preferredMode',
    transport: 'transport',
    custom_system_prompt: 'customSystemPrompt',
    sort_order: 'sortOrder',
    // name, specialty map to themselves
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
  /** Sync push_mode from server config to Android native push state. */
  function syncPushModeToNative() {
    try {
      const pushMode = serverConfig.value.push_mode as string || 'native'
      getNative()?.setNativePushEnabled?.(pushMode === 'native')
    } catch { /* not in app mode */ }
  }

  async function loadConfig() {
    try {
      const data = await apiGet<Record<string, unknown>>('/api/config')
      serverConfig.value = data
    } catch {
      // Server may be unreachable — keep existing cached values
    }
    // Sync push_mode to Android native after server config loads
    syncPushModeToNative()
  }

  async function patchConfig(changes: Record<string, unknown>): Promise<{ needsRestart: boolean; changedColdFields: string[]; warnings: string[] }> {
    const result = await apiPatch<{ needs_restart?: boolean; changed_cold_fields?: string[]; warnings?: string[] }>('/api/config', changes)
    // Reload config from server to get accurate values (e.g. password fields
    // are masked server-side and must be re-fetched rather than using the
    // plaintext value the user just submitted).
    await loadConfig()
    return {
      needsRestart: result.needs_restart ?? false,
      changedColdFields: result.changed_cold_fields ?? [],
      warnings: result.warnings ?? [],
    }
  }

  async function restartServer() {
    await apiPost('/api/config/restart', {})
  }

  /** Read a server config value by dot-path (e.g. "server.port") */
  function getServerValue(dotPath: string): unknown {
    const parts = dotPath.split('.')
    let current: unknown = serverConfig.value
    for (const p of parts) {
      if (current == null || typeof current !== 'object') return undefined
      current = (current as Record<string, unknown>)[p]
    }
    return current
  }

  /** Read a server config value by dot-path, falling back to built-in defaults */
  function getServerValueWithDefault(dotPath: string): unknown {
    const value = getServerValue(dotPath)
    if (value !== undefined) return value
    return serverDefaults[dotPath]
  }

  /** Write a server config value by dot-path and patch the server */
  async function setServerValue(dotPath: string, value: unknown): Promise<{ needsRestart: boolean; changedColdFields: string[]; warnings: string[] }> {
    const parts = dotPath.split('.')
    const changes: Record<string, unknown> = {}
    // Build nested object for patch (e.g. "server.port" → { server: { port: val } })
    let obj: Record<string, unknown> = changes
    for (let i = 0; i < parts.length - 1; i++) {
      obj[parts[i]] = {}
      obj = obj[parts[i]] as Record<string, unknown>
    }
    obj[parts[parts.length - 1]] = value

    // Save old value for rollback on failure
    const oldValue = getServerValue(dotPath)

    // Optimistic local cache update
    let current: Record<string, unknown> = serverConfig.value
    for (let i = 0; i < parts.length - 1; i++) {
      if (current[parts[i]] == null) current[parts[i]] = {}
      current = current[parts[i]] as Record<string, unknown>
    }
    current[parts[parts.length - 1]] = value

    try {
      return await patchConfig(changes)
    } catch (err: unknown) {
      // Rollback local cache on failure
      let rollbackTarget: unknown = serverConfig.value
      for (let i = 0; i < parts.length - 1; i++) {
        if (typeof rollbackTarget !== 'object' || rollbackTarget === null) break
        if ((rollbackTarget as Record<string, unknown>)[parts[i]] == null) break
        rollbackTarget = (rollbackTarget as Record<string, unknown>)[parts[i]]
      }
      if (rollbackTarget && typeof rollbackTarget === 'object') {
        (rollbackTarget as Record<string, unknown>)[parts[parts.length - 1]] = oldValue
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
