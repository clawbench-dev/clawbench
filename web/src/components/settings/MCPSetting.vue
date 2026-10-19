<template>
  <!-- Global MCP server list. One definition shared by every ACP agent; which
       agents may use it is a per-agent policy, configured on the agent page.
       CLI transport is unaffected: it has no mcpServers parameter and reads its
       own config file, so the hint below says so. -->
  <div class="mcp-setting">
    <div class="mcp-hint">{{ t('settings.items.mcpDesc') }}</div>

    <div v-if="servers.length === 0" class="mcp-empty">
      {{ t('settings.items.mcpEmpty') }}
    </div>

    <div v-for="(srv, idx) in servers" :key="srv.name + idx" class="mcp-row">
      <div class="mcp-row-head">
        <span class="mcp-name">{{ srv.name }}</span>
        <span class="mcp-transport">{{ srv.transport }}</span>
        <button class="fbtn" @click="removeServer(idx)">
          <Trash2 :size="13" />
          {{ t('settings.items.mcpRemove') }}
        </button>
      </div>
      <div class="mcp-summary">{{ describe(srv) }}</div>
    </div>

    <div class="mcp-add">
      <div class="mcp-add-grid">
        <input v-model="draft.name" class="mcp-input" :placeholder="t('settings.items.mcpName')" />
        <select v-model="draft.transport" class="mcp-input mcp-select">
          <option value="stdio">stdio</option>
          <option value="http">http</option>
          <option value="sse">sse</option>
        </select>
      </div>

      <template v-if="draft.transport === 'stdio'">
        <input v-model="draft.command" class="mcp-input" :placeholder="t('settings.items.mcpCommand')" />
        <input v-model="draft.args" class="mcp-input" :placeholder="t('settings.items.mcpArgs')" />
        <textarea
          v-model="draft.env"
          class="mcp-input mcp-textarea"
          rows="2"
          :placeholder="t('settings.items.mcpEnv')"
        />
      </template>
      <template v-else>
        <input v-model="draft.url" class="mcp-input" :placeholder="t('settings.items.mcpUrl')" />
        <textarea
          v-model="draft.headers"
          class="mcp-input mcp-textarea"
          rows="2"
          :placeholder="t('settings.items.mcpHeaders')"
        />
      </template>

      <button class="fbtn fbtn-primary" :disabled="!canAdd || saving" @click="addServer">
        {{ t('settings.items.mcpAdd') }}
      </button>
    </div>

    <div v-if="error" class="mcp-error">
      <AlertCircle :size="13" />
      <span>{{ error }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, Trash2 } from 'lucide-vue-next'
import '@/assets/modal-footer-btn.css'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { appLog } from '@/utils/appLog'

interface McpServer {
  name: string
  transport: string
  command?: string
  args?: string[]
  env?: Record<string, string>
  url?: string
  headers?: Record<string, string>
}

const { t } = useI18n()
const { getServerValue, setServerValue } = useSettingsConfig()

const saving = ref(false)
const error = ref('')

const servers = computed<McpServer[]>(() => {
  const raw = getServerValue('mcp.servers')
  return Array.isArray(raw) ? (raw as McpServer[]) : []
})

const draft = reactive({
  name: '',
  transport: 'stdio',
  command: '',
  args: '',
  url: '',
  env: '',
  headers: '',
})

/** parseKV reads "K=V" lines into a map. Blank lines and lines without "=" are
 *  dropped rather than rejected: pasting a multi-line env block often carries a
 *  trailing blank line. */
function parseKV(text: string): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const eq = trimmed.indexOf('=')
    if (eq <= 0) continue
    out[trimmed.slice(0, eq).trim()] = trimmed.slice(eq + 1).trim()
  }
  return Object.keys(out).length > 0 ? out : undefined
}

const canAdd = computed(() => draft.name.trim() !== '')

function describe(srv: McpServer): string {
  if (srv.transport === 'stdio') {
    const args = srv.args?.length ? ' ' + srv.args.join(' ') : ''
    return `${srv.command ?? ''}${args}`
  }
  return srv.url ?? ''
}

async function save(next: McpServer[]) {
  saving.value = true
  error.value = ''
  try {
    // The endpoint replaces the whole array, so every mutation sends all of it.
    await setServerValue('mcp.servers', next)
  } catch (err) {
    // The server validates hard (unknown transport, missing command, a command
    // that is not on PATH, a non-http URL) and returns a specific message. Show
    // it verbatim: it names the offending entry.
    error.value = err instanceof Error ? err.message : String(err)
    appLog.w('MCPSetting', 'save failed', error.value)
  } finally {
    saving.value = false
  }
}

async function addServer() {
  if (!canAdd.value) return
  const srv: McpServer = {
    name: draft.name.trim(),
    transport: draft.transport,
  }
  if (draft.transport === 'stdio') {
    srv.command = draft.command.trim()
    const args = draft.args.trim()
    if (args) srv.args = args.split(/\s+/)
    const env = parseKV(draft.env)
    if (env) srv.env = env
  } else {
    srv.url = draft.url.trim()
    const headers = parseKV(draft.headers)
    if (headers) srv.headers = headers
  }

  await save([...servers.value, srv])
  if (!error.value) {
    draft.name = ''
    draft.command = ''
    draft.args = ''
    draft.url = ''
    draft.env = ''
    draft.headers = ''
  }
}

async function removeServer(idx: number) {
  await save(servers.value.filter((_, i) => i !== idx))
}

onMounted(() => {
  // The list comes from GET /api/config, already loaded by the settings page.
  // Nothing to fetch here; this hook exists so a future lazy load has a home.
})
</script>

<style scoped>
.mcp-setting {
  /* Match the 16px inset used by the SettingsItem rows. */
  padding: var(--space-6) var(--space-7);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.mcp-hint,
.mcp-empty {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
.mcp-row {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
}
.mcp-row-head {
  display: flex;
  align-items: center;
  gap: var(--space-4);
}
.mcp-name {
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
}
.mcp-transport {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0 var(--space-2);
}
.mcp-row-head .fbtn {
  margin-left: auto;
}
.mcp-summary {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  word-break: break-all;
}
.mcp-add {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border-color);
}
.mcp-add-grid {
  display: flex;
  gap: var(--space-3);
}
/* Input geometry follows the settings-panel convention (30px control height).
 *
 * Sizing must be stated per container, because `flex` means different things on
 * each axis and the add form uses both:
 *
 *   .mcp-add        COLUMN  → `flex: 1` would set flex-basis: 0% on the VERTICAL
 *                             axis and override `height`. Measured in the
 *                             browser before this fix: computed height 30px but
 *                             a rendered box of 17px, so the text fields were
 *                             visibly shorter than the select next to them.
 *   .mcp-add-grid   ROW     → `flex: 1` is exactly what makes the name field
 *                             share the row with the fixed-width select.
 *
 * So: never put a bare `flex` on the shared class; set it on the row's children.
 */
.mcp-input {
  box-sizing: border-box;
  height: 30px;
  padding: 0 var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
}
/* Row children share the width: the name field grows, the select is fixed. */
.mcp-add-grid > .mcp-input:not(.mcp-select) {
  flex: 1;
  min-width: 120px;
}
/* Column children must keep their height and fill the width on the cross axis. */
.mcp-add > .mcp-input {
  align-self: stretch;
  flex: none;
}
.mcp-select {
  flex: 0 0 100px;
  padding: 0 var(--space-3);
}
/* A textarea has no intrinsic control height: it keeps height:auto so its rows
 * attribute decides, and must not be stretched on the vertical axis. */
.mcp-textarea {
  height: auto;
  padding: var(--space-2) var(--space-6);
  font-family: var(--font-mono);
  resize: vertical;
}
.mcp-input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 2px var(--focus-ring);
}
.mcp-error {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  color: var(--color-red);
  font-size: var(--font-size-sm);
}
</style>
