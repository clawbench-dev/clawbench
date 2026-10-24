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
        <!-- Test runs the real MCP handshake (initialize + tools/list), which
             is the only check that distinguishes a working server from one
             that merely starts. Saving a definition does not prove it works. -->
        <button
          class="fbtn mcp-test-btn"
          :disabled="testingName !== null"
          :title="t('settings.items.mcpTestHint')"
          @click="testServer(srv)"
        >
          <component :is="testingName === srv.name ? Loader2 : PlugZap" :size="13" :class="{ 'mcp-spin': testingName === srv.name }" />
          {{ testingName === srv.name ? t('settings.items.mcpTesting') : t('settings.items.mcpTest') }}
        </button>
        <button class="fbtn" @click="removeServer(idx)">
          <Trash2 :size="13" />
          {{ t('settings.items.mcpRemove') }}
        </button>
      </div>
      <div class="mcp-summary">{{ describe(srv) }}</div>
      <div
        v-if="testResults[srv.name]"
        class="mcp-test-result"
        :class="testResults[srv.name].success ? 'mcp-test-result--ok' : 'mcp-test-result--fail'"
      >
        <component :is="testResults[srv.name].success ? CheckCircle2 : AlertCircle" :size="13" />
        <span>{{ testResults[srv.name].message }}</span>
      </div>
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
import { AlertCircle, CheckCircle2, Loader2, PlugZap, Trash2 } from 'lucide-vue-next'
import '@/assets/modal-footer-btn.css'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useConnectivityTest } from '@/composables/useConnectivityTest'
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
const { getServerValue, setServerValue, loadConfig } = useSettingsConfig()
const { testResults: testResultsList, runTests } = useConnectivityTest()

const saving = ref(false)
const error = ref('')
/** Name of the server currently being probed, or null. */
const testingName = ref<string | null>(null)

/** Per-server result, keyed by name so it survives a re-render of the row. */
const testResults = ref<Record<string, { success: boolean; message: string }>>({})

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
  const removed = servers.value[idx]?.name
  await save(servers.value.filter((_, i) => i !== idx))
  if (removed) delete testResults.value[removed]
}

/**
 * Probe one server by performing the real MCP handshake.
 *
 * The whole current list is sent (not just the target) because the backend
 * resolves the definition from it — that is what makes "test an unsaved edit"
 * work: the user is deciding whether to save, so the saved definition is the
 * wrong thing to check.
 *
 * A longer timeout than the other categories: a stdio server may need to start
 * a runtime (node/python) before it answers, and a launcher like `npx -y` or
 * `uvx` may download the package on the first run — a cold start on a loaded
 * machine easily exceeds the default 15s. It must stay above the server's own
 * MCP probe budget (45s) so the server's specific error message wins the race
 * instead of a generic client-side timeout.
 */
async function testServer(srv: McpServer) {
  if (testingName.value !== null) return
  testingName.value = srv.name
  delete testResults.value[srv.name]
  try {
    await runTests([{
      category: 'mcp',
      values: { 'mcp.name': srv.name, 'mcp.servers': servers.value },
      timeoutMs: 60_000,
    }])
    const r = testResultsList.value[0]
    testResults.value = {
      ...testResults.value,
      [srv.name]: r ?? { success: false, message: 'No result returned' },
    }
  } catch (err) {
    testResults.value = {
      ...testResults.value,
      [srv.name]: { success: false, message: err instanceof Error ? err.message : String(err) },
    }
    appLog.w('MCPSetting', 'mcp test failed', err)
  } finally {
    testingName.value = null
  }
}

onMounted(() => {
  // Refetch on every entry into this page.
  //
  // The list is served from the shared serverConfig snapshot, which is loaded
  // once at app startup and on settings-tab activation. Neither fires when the
  // user drills into this category while already inside Settings, so a server
  // added in the meantime (another device, the AI editing config.yaml, a CLI)
  // stayed invisible until a full page reload. This component is remounted each
  // time the category is opened, which is exactly the moment the list must be
  // current.
  void loadConfig()
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
  /* The first button after the name/transport takes the slack, so the action
   * pair sits flush right. */
  margin-left: auto;
}
/* The second button in the pair must not re-apply the auto margin. */
.mcp-row-head .fbtn + .fbtn {
  margin-left: 0;
}
.mcp-test-result {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  word-break: break-word;
}
.mcp-test-result--ok {
  color: var(--color-green);
}
.mcp-test-result--fail {
  color: var(--color-red);
}
.mcp-spin {
  animation: mcp-spin 1s linear infinite;
}
@keyframes mcp-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (prefers-reduced-motion: reduce) {
  .mcp-spin {
    animation-duration: 3s;
  }
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
  height: calc(30 * var(--dp));
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
  min-width: calc(120 * var(--dp));
}
/* Column children must keep their height and fill the width on the cross axis. */
.mcp-add > .mcp-input {
  align-self: stretch;
  flex: none;
}
.mcp-select {
  flex: 0 0 calc(100 * var(--dp));
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
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--focus-ring);
}
.mcp-error {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  color: var(--color-red);
  font-size: var(--font-size-sm);
}
</style>
