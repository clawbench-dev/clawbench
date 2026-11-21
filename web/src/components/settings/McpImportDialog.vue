<template>
  <!-- Paste-to-import for people moving MCP configs in from another tool.
       The list is not saved here: the parent merges it with what is already
       configured and does the single whole-array save, so a rejected entry
       leaves the dialog open with the server's message intact. -->
  <ModalDialog
    :open="open"
    :title="t('settings.items.mcpImportTitle')"
    :z-index="2500"
    size="wide"
    @close="handleClose"
  >
    <div class="mcp-import">
      <div class="mcp-import__hint">{{ t('settings.items.mcpImportHint') }}</div>
      <textarea
        v-model="text"
        class="mcp-import__text"
        rows="8"
        spellcheck="false"
        placeholder='{"mcpServers": {"github": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-github"]}}}'
      />

      <div v-if="parseError" class="mcp-import__error">
        <AlertCircle :size="13" />
        <span>{{ parseError }}</span>
      </div>

      <template v-else-if="text.trim()">
        <div v-if="entries.length === 0" class="mcp-import__empty">
          {{ t('settings.items.mcpImportEmpty') }}
        </div>

        <template v-else>
          <div class="mcp-import__meta">
            <span>{{ t('settings.items.mcpImportCount', { count: entries.length }) }}</span>
            <span v-if="skipped > 0">
              {{ t('settings.items.mcpImportSkipped', { count: skipped }) }}
            </span>
          </div>

          <div class="mcp-import__list">
            <div
              v-for="(entry, i) in entries"
              :key="i"
              class="mcp-import__row"
              :class="{ 'mcp-import__row--blocked': rowIssue(i) !== undefined }"
            >
              <input v-model="selected[i]" type="checkbox" class="mcp-import__check" />
              <input v-model="names[i]" class="mcp-import__name" :aria-label="t('settings.items.mcpName')" />
              <span class="mcp-import__transport">{{ entry.server.transport }}</span>
              <span class="mcp-import__summary">{{ describe(entry) }}</span>
              <span v-if="rowIssue(i)" class="mcp-import__issue">{{ issueText(rowIssue(i)) }}</span>
              <span v-else-if="isExisting(i)" class="mcp-import__tag">
                {{ t('settings.items.mcpImportExisting') }}
              </span>
            </div>
          </div>
        </template>
      </template>

      <div v-if="errorMessage" class="mcp-import__error">
        <AlertCircle :size="13" />
        <span>{{ errorMessage }}</span>
      </div>
    </div>

    <template #footer>
      <button class="fbtn" @click="handleClose">{{ t('common.cancel') }}</button>
      <button class="fbtn fbtn-primary" :disabled="selectedCount === 0" @click="submit">
        {{ t('settings.items.mcpImportConfirm', { count: selectedCount }) }}
      </button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import {
  isValidMcpName,
  parseMcpImport,
  type McpImportEntry,
  type McpImportIssue,
  type McpImportServer,
} from '@/utils/mcpImport'

const props = defineProps<{
  open: boolean
  /** Names already configured. Same-name rows start unselected (importing one
   *  replaces the stored definition, which may hold a key the user edited). */
  existingNames: string[]
  /** Server-side rejection text, owned by the parent so the dialog can stay
   *  open while the user drops or fixes the offending entry. */
  errorMessage?: string
}>()

const emit = defineEmits<{
  close: []
  import: [servers: McpImportServer[]]
}>()

const { t } = useI18n()

const text = ref('')
/** Per-row name, editable so "my.server" can be fixed instead of silently
 *  rewritten — the name is a stable identifier several agents cache by. */
const names = ref<string[]>([])
const selected = ref<boolean[]>([])

const parsed = computed(() => parseMcpImport(text.value))
const entries = computed<McpImportEntry[]>(() => (parsed.value.ok ? parsed.value.entries : []))
const skipped = computed(() => (parsed.value.ok ? parsed.value.skipped : 0))

const parseError = computed(() => {
  if (!text.value.trim()) return ''
  if (parsed.value.ok) return ''
  return parsed.value.error === 'invalid-json'
    ? t('settings.items.mcpImportInvalidJson')
    : t('settings.items.mcpImportUnsupported')
})

/** Fresh text means fresh rows: drop previous selections and renames. */
watch(entries, (list) => {
  names.value = list.map((entry) => entry.server.name)
  // Everything is preselected; rows with problems are excluded from the count
  // and shown with their reason, so a fixable name becomes importable the
  // moment it is corrected.
  selected.value = list.map((entry) => !props.existingNames.includes(entry.server.name))
}, { immediate: true })

watch(() => props.open, (open) => {
  if (!open) return
  text.value = ''
  names.value = []
  selected.value = []
})

function isExisting(i: number): boolean {
  return props.existingNames.includes(names.value[i]?.trim() ?? '')
}

/**
 * Name rules are re-checked against the edited value; the remaining issues
 * (missing command, bad URL) come from the parser and cannot be fixed here.
 */
function rowIssue(i: number): McpImportIssue | 'duplicate-name' | undefined {
  const name = names.value[i]?.trim() ?? ''
  if (!name) return 'empty-name'
  if (!isValidMcpName(name)) return 'invalid-name'
  if (names.value.filter((other) => other.trim() === name).length > 1) return 'duplicate-name'
  const base = entries.value[i]?.issue
  // Name problems were re-checked above against the edited value; keeping the
  // parser's stale verdict would leave the row blocked after a rename.
  if (base === 'empty-name' || base === 'invalid-name') return undefined
  return base
}

function issueText(issue: McpImportIssue | 'duplicate-name' | undefined): string {
  switch (issue) {
    case 'empty-name': return t('settings.items.mcpImportIssueEmptyName')
    case 'invalid-name': return t('settings.items.mcpImportIssueInvalidName')
    case 'duplicate-name': return t('settings.items.mcpImportIssueDuplicateName')
    case 'missing-command': return t('settings.items.mcpImportIssueMissingCommand')
    case 'missing-url': return t('settings.items.mcpImportIssueMissingUrl')
    case 'bad-url': return t('settings.items.mcpImportIssueBadUrl')
    case 'bad-transport': return t('settings.items.mcpImportIssueBadTransport')
    default: return ''
  }
}

const selectedCount = computed(() =>
  entries.value.reduce((n, _, i) => (selected.value[i] && rowIssue(i) === undefined ? n + 1 : n), 0),
)

function describe(entry: McpImportEntry): string {
  const { server } = entry
  if (server.transport === 'stdio') {
    return [server.command, ...(server.args ?? [])].filter(Boolean).join(' ')
  }
  return server.url ?? ''
}

function submit() {
  const servers: McpImportServer[] = []
  entries.value.forEach((entry, i) => {
    if (!selected.value[i] || rowIssue(i) !== undefined) return
    servers.push({ ...entry.server, name: names.value[i].trim() })
  })
  if (servers.length === 0) return
  emit('import', servers)
}

function handleClose() {
  emit('close')
}
</script>

<style scoped>
.mcp-import {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  /* ModalDialog's body has no inset of its own. */
  padding: var(--space-5) var(--space-7) var(--space-6);
}
.mcp-import__hint {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
.mcp-import__text {
  box-sizing: border-box;
  width: 100%;
  padding: var(--space-3) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  resize: vertical;
}
.mcp-import__text:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--focus-ring);
}
.mcp-import__meta {
  display: flex;
  gap: var(--space-4);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}
.mcp-import__empty {
  font-size: var(--font-size-md);
  color: var(--text-muted);
}
.mcp-import__list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  max-height: calc(260 * var(--dp));
  overflow-y: auto;
}
.mcp-import__row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
}
.mcp-import__row--blocked {
  border-color: var(--color-red);
}
.mcp-import__check {
  flex: none;
}
/* The name is editable in place: migration configs often use dots or spaces,
 * which the server rejects. Keep it narrow so the summary still fits. */
.mcp-import__name {
  flex: 0 1 calc(160 * var(--dp));
  min-width: calc(90 * var(--dp));
  height: calc(26 * var(--dp));
  padding: 0 var(--space-3);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
}
.mcp-import__name:focus {
  outline: none;
  border-color: var(--accent-color);
}
.mcp-import__transport {
  flex: none;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0 var(--space-2);
}
.mcp-import__summary {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}
/* On a phone the row cannot hold name + transport + command side by side; the
 * summary would collapse to zero width and the user would be confirming a
 * command they cannot see. Give it its own line instead. */
@media (max-width: 560px) {
  .mcp-import__summary {
    order: 1;
    flex: 1 0 100%;
  }
}
.mcp-import__issue {
  /* Long translations shrink (and wrap) instead of pushing the row wider than
   * its border. */
  flex: 0 1 auto;
  min-width: 0;
  text-align: right;
  font-size: var(--font-size-sm);
  color: var(--color-red);
}
.mcp-import__tag {
  flex: none;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}
.mcp-import__error {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-5);
  border-radius: var(--radius-sm);
  background: rgba(231, 76, 60, 0.1);
  color: var(--color-red);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  word-break: break-word;
}
</style>
