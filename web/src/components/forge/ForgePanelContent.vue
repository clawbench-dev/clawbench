<template>
  <div class="forge-panel">
    <!-- Unbound / no-project states use a question-card layout (same visual
         language as the AskUserQuestion card): one prompt plus actionable
         options. -->
    <div v-if="!projectPath" class="forge-state">
      <div class="forge-card">
        <div class="forge-card-header">{{ t('forge.empty.noProjectHeader') }}</div>
        <div class="forge-card-body">{{ t('forge.empty.noProjectBody') }}</div>
        <div class="forge-card-options">
          <button class="forge-option" @click="emit('request-project')">
            {{ t('forge.empty.chooseProject') }}
          </button>
        </div>
      </div>
    </div>

    <div v-else-if="!items.isBound.value && !items.loading.value" class="forge-state">
      <div class="forge-card">
        <div class="forge-card-header">{{ t('forge.empty.noBindingHeader') }}</div>
        <div class="forge-card-body">{{ t('forge.empty.noBindingBody') }}</div>
        <div class="forge-card-options">
          <button class="forge-option primary" @click="openBindDialog">
            {{ t('forge.empty.bindRepo') }}
          </button>
          <button
            v-if="items.suggested.value"
            class="forge-option"
            @click="acceptSuggestion"
          >
            {{ t('forge.empty.useSuggestion', { slug: items.suggested.value.slug }) }}
          </button>
        </div>
      </div>
    </div>

    <template v-else>
      <!-- Detail view replaces the list in place (currentView pattern). -->
      <ForgeDetail
        v-if="detailOpen"
        :type="items.type.value"
        :number="detailNumber"
        @back="closeDetail"
        @analyze="onAnalyze"
      />

      <template v-else>
        <!-- Filter row 1: type + state -->
        <div class="forge-filters">
          <div class="forge-chips">
            <button
              class="forge-chip"
              :class="{ active: items.type.value === 'issue' }"
              @click="items.setType('issue')"
            >{{ t('forge.type.issues') }}</button>
            <button
              class="forge-chip"
              :class="{ active: items.type.value === 'pr' }"
              @click="items.setType('pr')"
            >{{ t('forge.type.prs') }}</button>
          </div>
          <div class="forge-chips">
            <button
              v-for="s in stateOptions"
              :key="s"
              class="forge-chip"
              :class="{ active: items.state.value === s }"
              @click="items.setState(s)"
            >{{ t(`forge.state.${s}`) }}</button>
          </div>
        </div>

        <!-- Filter row 2: related-to-me -->
        <div class="forge-filters forge-filters-second">
          <div class="forge-chips forge-chips-scroll">
            <button
              v-for="f in mineOptions"
              :key="f"
              class="forge-chip"
              :class="{ active: items.mineFilter.value === f }"
              @click="items.setMineFilter(f)"
            >{{ t(`forge.mine.${f}`) }}</button>
          </div>
          <RefreshButton :loading="items.loading.value" :title="t('nav.refresh')" @click="refresh" />
        </div>

        <!-- Search -->
        <div class="forge-search">
          <input
            v-model="searchInput"
            class="forge-search-input"
            type="search"
            :placeholder="t('forge.searchPlaceholder')"
            @keyup.enter="items.setQuery(searchInput)"
          />
        </div>

        <!-- Error card: distinguishes auth / rate-limit / network so the user
             knows what to do. -->
        <div v-if="items.error.value" class="forge-error-card">
          <div class="forge-error-title">{{ errorTitle(items.error.value.code) }}</div>
          <div class="forge-error-body">{{ items.error.value.message }}</div>
          <button class="forge-option" @click="refresh">{{ t('forge.retry') }}</button>
        </div>

        <div v-else-if="items.loading.value" class="forge-loading">
          <LoadingIndicator size="md" :label="t('forge.loading')" />
        </div>

        <div v-else-if="items.items.value.length === 0" class="forge-state">
          <div class="forge-empty-text">{{ t('forge.emptyList') }}</div>
        </div>

        <div v-else class="forge-list" @scroll="onListScroll">
          <div
            v-for="it in items.items.value"
            :key="`${it.type}-${it.number}`"
            class="forge-row"
            @click="openDetail(it)"
          >
            <div class="forge-row-title">
              <span class="forge-state-dot" :class="`state-${it.state}`"></span>
              <span class="forge-row-number">#{{ it.number }}</span>
              <span class="forge-row-text">{{ it.title }}</span>
            </div>
            <div class="forge-row-meta">
              <span class="forge-row-author">{{ it.author }}</span>
              <span class="forge-row-time">{{ formatTime(it.updatedAt) }}</span>
              <span v-if="it.commentCount" class="forge-row-comments">{{ it.commentCount }}</span>
            </div>
          </div>
          <div v-if="items.loadingMore.value" class="forge-loading-more">
            <LoadingIndicator size="sm" />
          </div>
        </div>
      </template>
    </template>

    <!-- Binding dialog -->
    <ModalDialog v-if="bindDialogOpen" :title="t('forge.bind.title')" @close="bindDialogOpen = false">
      <div class="forge-bind-form">
        <div v-if="remotes.length" class="forge-bind-remotes">
          <div class="forge-bind-label">{{ t('forge.bind.fromRemote') }}</div>
          <button
            v-for="r in remotes"
            :key="r.name + r.url"
            class="forge-remote-row"
            :disabled="!r.slug"
            @click="bindFromRemote(r)"
          >
            <span class="forge-remote-name">{{ r.name }}</span>
            <span class="forge-remote-url">{{ r.slug || r.url }}</span>
          </button>
        </div>
        <div class="forge-bind-manual">
          <div class="forge-bind-label">{{ t('forge.bind.manual') }}</div>
          <input v-model="manualUrl" class="forge-search-input" :placeholder="t('forge.bind.urlPlaceholder')" />
          <button class="forge-option primary" :disabled="!manualUrl" @click="bindFromUrl">
            {{ t('forge.bind.submit') }}
          </button>
        </div>
        <div v-if="bindError" class="forge-error-body">{{ bindError }}</div>
      </div>
    </ModalDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import ModalDialog from '@/components/common/ModalDialog.vue'
import ForgeDetail from '@/components/forge/ForgeDetail.vue'
import { useForgeItems } from '@/composables/useForge'
import { fetchForgeRemotes, setForgeBinding, type ForgeRemote, ForgeApiError } from '@/utils/forgeApi'
import { appLog } from '@/utils/appLog'

const TAG = 'ForgePanel'
const props = defineProps<{
  active: boolean
  projectPath: string
}>()
const emit = defineEmits<{
  (e: 'request-project'): void
  (e: 'analyze', payload: { item: { type: 'issue' | 'pr'; number: number; title: string; url: string; slug: string; body: string } }): void
}>()

const { t } = useI18n()
const items = useForgeItems(() => props.projectPath)

const stateOptions: Array<'open' | 'closed' | 'all'> = ['open', 'closed', 'all']
const mineOptions: Array<'all' | 'assigned' | 'created' | 'review'> = ['all', 'assigned', 'created', 'review']

const searchInput = ref('')
const detailOpen = ref(false)
const detailNumber = ref(0)
const bindDialogOpen = ref(false)
const remotes = ref<ForgeRemote[]>([])
const manualUrl = ref('')
const bindError = ref('')

async function refresh() {
  await items.loadBinding()
  if (items.isBound.value) await items.load()
}

onMounted(refresh)

// Reload when the project changes or the panel becomes active.
watch(() => props.projectPath, () => {
  closeDetail()
  void refresh()
})
watch(() => props.active, (isActive) => {
  if (isActive && items.isBound.value && items.items.value.length === 0 && !items.loading.value) {
    void items.load()
  }
})

function openDetail(it: { type: 'issue' | 'pr'; number: number }) {
  items.type.value = it.type
  detailNumber.value = it.number
  detailOpen.value = true
}
function closeDetail() {
  detailOpen.value = false
  detailNumber.value = 0
}

function onListScroll(e: Event) {
  const el = e.target as HTMLElement
  if (!el) return
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 120) {
    void items.loadMore()
  }
}

async function openBindDialog() {
  bindDialogOpen.value = true
  bindError.value = ''
  try {
    const res = await fetchForgeRemotes()
    remotes.value = res.remotes ?? []
  } catch (err) {
    appLog.w(TAG, 'load remotes failed', err)
    remotes.value = []
  }
}

async function bindFromRemote(r: ForgeRemote) {
  if (!r.slug) return
  await submitBinding({ platform: r.platform, host: r.host, owner: r.owner, repo: r.repo })
}

async function bindFromUrl() {
  await submitBinding({ url: manualUrl.value })
}

async function acceptSuggestion() {
  const s = items.suggested.value
  if (!s) return
  await submitBinding({ platform: s.platform, host: s.host, owner: s.owner, repo: s.repo })
}

async function submitBinding(input: { url?: string; platform?: string; host?: string; owner?: string; repo?: string }) {
  bindError.value = ''
  try {
    await setForgeBinding(input)
    bindDialogOpen.value = false
    manualUrl.value = ''
    await refresh()
  } catch (err) {
    if (err instanceof ForgeApiError) {
      bindError.value = err.code === 'UnsafeHost' ? t('forge.bind.unsafeHost') : err.message
    } else {
      bindError.value = String(err)
    }
  }
}

function onAnalyze(payload: { item: { type: 'issue' | 'pr'; number: number; title: string; url: string; slug: string; body: string } }) {
  emit('analyze', payload)
}

function errorTitle(code: string): string {
  switch (code) {
    case 'ForgeAuthFailed': return t('forge.error.auth')
    case 'ForgeRateLimited': return t('forge.error.rateLimit')
    case 'ForgeNetworkError': return t('forge.error.network')
    case 'NoForgeBinding': return t('forge.empty.noBindingHeader')
    default: return t('forge.error.generic')
  }
}

function formatTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString()
}
</script>

<style scoped>
.forge-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.forge-state {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px 20px;
  flex: 1;
}
.forge-card {
  max-width: 420px;
  width: 100%;
  border: 1px solid var(--border-color);
  border-radius: 12px;
  background: var(--bg-secondary);
  overflow: hidden;
}
.forge-card-header {
  padding: 14px 16px;
  font-weight: 600;
  border-bottom: 1px solid var(--border-color);
}
.forge-card-body {
  padding: 14px 16px;
  color: var(--text-secondary);
  font-size: 14px;
}
.forge-card-options {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 0 16px 16px;
}
.forge-option {
  padding: 10px 14px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: transparent;
  color: var(--text-primary);
  cursor: pointer;
  text-align: left;
}
.forge-option.primary {
  border-color: var(--accent-color);
  color: var(--accent-color);
}
.forge-option:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.forge-filters {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px 0;
  flex-wrap: wrap;
}
.forge-filters-second {
  padding-top: 4px;
}
.forge-chips {
  display: flex;
  gap: 6px;
}
.forge-chips-scroll {
  overflow-x: auto;
  flex: 1;
  scrollbar-width: none;
}
.forge-chips-scroll::-webkit-scrollbar { display: none; }
.forge-chip {
  padding: 4px 12px;
  border-radius: 999px;
  border: 1px solid var(--border-color);
  background: transparent;
  color: var(--text-secondary);
  font-size: 13px;
  white-space: nowrap;
  cursor: pointer;
}
.forge-chip.active {
  background: var(--accent-color);
  border-color: var(--accent-color);
  color: #fff;
}
.forge-search {
  padding: 8px 12px;
}
.forge-search-input {
  width: 100%;
  padding: 8px 10px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-primary);
  color: var(--text-primary);
}
.forge-error-card {
  margin: 12px;
  padding: 14px;
  border: 1px solid var(--border-color);
  border-radius: 10px;
  background: var(--bg-secondary);
}
.forge-error-title {
  font-weight: 600;
  margin-bottom: 6px;
}
.forge-error-body {
  color: var(--text-secondary);
  font-size: 13px;
  margin-bottom: 10px;
}
.forge-loading {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}
.forge-empty-text {
  color: var(--text-muted);
}
.forge-list {
  flex: 1;
  overflow-y: auto;
}
.forge-row {
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-color);
  cursor: pointer;
}
.forge-row:active {
  background: var(--bg-secondary);
}
.forge-row-title {
  display: flex;
  align-items: center;
  gap: 8px;
}
.forge-row-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.forge-row-number {
  color: var(--text-muted);
  font-size: 13px;
}
.forge-row-meta {
  display: flex;
  gap: 10px;
  margin-top: 4px;
  padding-left: 16px;
  color: var(--text-muted);
  font-size: 12px;
}
.forge-state-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.forge-state-dot.state-open { background: #2da44e; }
.forge-state-dot.state-closed { background: #cf222e; }
.forge-state-dot.state-merged { background: #8250df; }
.forge-loading-more {
  padding: 12px;
  display: flex;
  justify-content: center;
}
.forge-bind-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 4px;
}
.forge-bind-label {
  font-size: 13px;
  color: var(--text-muted);
  margin-bottom: 6px;
}
.forge-remote-row {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  width: 100%;
  padding: 8px 10px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: transparent;
  color: var(--text-primary);
  cursor: pointer;
  margin-bottom: 6px;
}
.forge-remote-row:disabled { opacity: 0.5; cursor: not-allowed; }
.forge-remote-name { font-weight: 600; }
.forge-remote-url { font-size: 12px; color: var(--text-muted); }
.forge-bind-manual { display: flex; flex-direction: column; gap: 8px; }
</style>
