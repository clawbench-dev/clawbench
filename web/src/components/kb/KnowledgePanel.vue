<template>
  <div ref="rootEl" class="kb-panel">
    <div class="kb-header">
      <div class="kb-title-line">
        <BookOpen :size="14" class="panel-header-icon" />
        <span class="kb-title">{{ t('kb.title') }}</span>
        <span v-if="collections.length" class="kb-count">{{ t('kb.count', { count: collections.length }) }}</span>
      </div>
      <ActionBar class="kb-header-actions" mode="menu" align="end" size="header">
        <button class="header-btn is-primary" type="button" data-action="kb-new" :title="t('kb.newCollection')" @click="openCreate">
          <Plus :size="15" />
          <span class="action-label">{{ t('kb.newCollection') }}</span>
        </button>
        <RefreshButton class="header-btn" :loading="loadingCollections" :disabled="loadingCollections" :title="t('kb.refresh')" @click="refresh(split)">
          <span class="action-label">{{ t('kb.refresh') }}</span>
        </RefreshButton>
      </ActionBar>
    </div>

    <div v-if="!available" class="kb-empty kb-empty--blocked">
      <Database :size="28" class="kb-empty-icon" />
      <span>{{ t('kb.loadFailed') }}</span>
      <span class="kb-empty-desc">{{ t('kb.emptyCollectionsDesc') }}</span>
    </div>

    <div v-else class="kb-body">
      <SplitView
        class="kb-split"
        orientation="horizontal"
        :enabled="split"
        :ratio="splitRatio"
        :min-left="SPLIT_MIN_LIST"
        :min-right="SPLIT_MIN_DETAIL"
        @update:ratio="onSplitRatioChange"
      >
        <!-- Collections: cards on the left. On a phone this pane is the whole
             panel; picking one drills into the detail pane. -->
        <template #left>
          <aside v-show="split || selectedId == null" class="kb-rail">
            <PanelSkeleton v-if="loadingCollections && collections.length === 0" variant="list" :rows="6" :meta="true" :label="t('common.loading')" />

            <div v-else-if="error" class="kb-hint kb-hint--error">
              <AlertTriangle :size="16" />
              <span>{{ error }}</span>
              <button class="kb-link" type="button" @click="refresh(split)">{{ t('common.retry') }}</button>
            </div>

            <EmptyState
              v-else-if="collections.length === 0"
              :icon="BookOpen"
              :title="t('kb.emptyCollections')"
              :desc="t('kb.emptyCollectionsDesc')"
            >
              <template #action>
                <button class="fbtn fbtn-primary" type="button" @click="openCreate">
                  <Plus :size="15" />
                  <span>{{ t('kb.newCollection') }}</span>
                </button>
              </template>
            </EmptyState>

            <template v-else>
              <div v-for="group in groupedCollections" :key="group.key" class="kb-group">
                <div class="kb-group-title">
                  <span>{{ t(group.labelKey) }}</span>
                  <span class="kb-group-count">{{ group.items.length }}</span>
                </div>
                <button
                  v-for="col in group.items"
                  :key="col.id"
                  type="button"
                  class="kb-col"
                  :class="{ 'is-active': col.id === selectedId }"
                  :data-kb-collection="col.id"
                  @click="selectCollection(col.id)"
                >
                  <span class="kb-col-icon"><BookOpen :size="15" /></span>
                  <span class="kb-col-text">
                    <span class="kb-col-name">{{ col.name }}</span>
                    <span class="kb-col-meta">
                      {{ t('kb.documents', { count: col.doc_count }) }}
                      <span v-if="!col.auto_inject" class="kb-col-off">{{ t('kb.autoInjectOff') }}</span>
                    </span>
                  </span>
                  <ChevronRight v-if="!split" :size="16" class="kb-col-chevron" />
                </button>
              </div>
            </template>
          </aside>
        </template>

        <template #right>
          <section v-show="split || selectedId != null" class="kb-main">
            <template v-if="selected">
              <!-- Detail header: identity first, actions second. They never
                   compete for the same line, so a narrow pane cannot squeeze
                   the name into a vertical column. -->
              <header class="kb-main-head">
                <div class="kb-main-title-line">
                  <button v-if="!split" class="kb-back" type="button" :title="t('common.back')" @click="backToCollections">
                    <ChevronLeft :size="18" />
                  </button>
                  <span class="kb-main-icon"><BookOpen :size="18" /></span>
                  <div class="kb-main-title-text">
                    <div class="kb-main-name">
                      <span class="kb-main-name-text">{{ selected.name }}</span>
                      <span class="kb-scope" :class="`kb-scope--${selected.scope}`">
                        {{ selected.scope === 'global' ? t('kb.scopeGlobal') : t('kb.scopeProject') }}
                      </span>
                      <button
                        type="button"
                        class="kb-inject-chip"
                        :class="{ 'is-off': !selected.auto_inject }"
                        data-action="kb-toggle-auto-inject"
                        :title="selected.auto_inject ? t('kb.autoInjectOn') : t('kb.autoInjectOff')"
                        @click="toggleAutoInject"
                      >
                        <Sparkles :size="11" />
                        <span>{{ t('kb.autoInjectShort') }}</span>
                        <span class="kb-inject-state">{{ selected.auto_inject ? t('kb.on') : t('kb.off') }}</span>
                      </button>
                    </div>
                    <div v-if="selected.description" class="kb-main-desc">{{ selected.description }}</div>
                  </div>
                </div>

                <ActionBar class="kb-main-actions" mode="menu" align="start" size="bar">
                  <button class="header-btn is-primary" type="button" data-action="kb-add" :title="t('kb.addFile')" @click="openAdd">
                    <Plus :size="15" />
                    <span class="action-label">{{ t('kb.addFile') }}</span>
                  </button>
                  <button class="header-btn" type="button" data-action="kb-reindex-collection" :title="t('kb.reindex')" @click="reindexCurrent">
                    <RotateCw :size="14" />
                  </button>
                  <button class="header-btn is-danger" type="button" data-action="kb-delete-collection" :title="t('kb.deleteCollection')" @click="removeCurrent">
                    <Trash2 :size="14" />
                  </button>
                </ActionBar>
              </header>

              <div class="kb-search-row">
                <SearchInput
                  v-model="searchQuery"
                  class="kb-search"
                  :placeholder="t('kb.searchPlaceholder')"
                  @enter="runSearch"
                />
                <!-- Filters + submit travel as one block: if the row wraps they
                     move together instead of leaving the button alone. -->
                <div class="kb-search-controls">
                  <select v-model="searchScope" class="kb-select" :title="t('kb.searchAll')" @change="searchResult && runSearch()">
                    <option value="current">{{ t('kb.searchThis') }}</option>
                    <option value="all">{{ t('kb.searchAll') }}</option>
                  </select>
                  <select v-model="searchMode" class="kb-select" @change="searchResult && runSearch()">
                    <option value="hybrid">{{ t('kb.modeHybrid') }}</option>
                    <option value="fts">{{ t('kb.modeFts') }}</option>
                  </select>
                  <button class="header-btn" type="button" data-action="kb-search" :disabled="searching || !searchQuery.trim()" @click="runSearch">
                    <Search :size="14" />
                  </button>
                </div>
              </div>

              <div v-if="searchResult || searching || searchError" class="kb-results">
                <div class="kb-results-head">
                  <span v-if="searching" class="kb-results-count">{{ t('common.loading') }}</span>
                  <span v-else-if="searchResult" class="kb-results-count">{{ t('kb.results', { count: searchResult.total }) }}</span>
                  <button v-if="searchResult || searchError" class="kb-link" type="button" @click="clearSearch">
                    {{ t('common.close') }}
                  </button>
                </div>

                <div v-if="searchError" class="kb-hint kb-hint--error">
                  <AlertTriangle :size="15" />
                  <span>{{ searchError }}</span>
                </div>
                <EmptyState
                  v-else-if="!searching && searchResult && searchResult.results.length === 0"
                  :icon="Search"
                  :title="t('kb.searchEmpty')"
                  :desc="t('kb.searchEmptyDesc')"
                />
                <div v-else class="kb-result-list">
                  <article v-for="hit in searchResult?.results ?? []" :key="hit.chunk_id" class="kb-result" :data-kb-hit="hit.chunk_id">
                    <div class="kb-result-head">
                      <span class="kb-result-title">{{ hit.document_title }}</span>
                      <span v-if="hit.location" class="kb-result-loc">{{ locationText(hit.location) }}</span>
                      <span class="kb-result-col">{{ t('kb.inCollection', { name: hit.collection_name }) }}</span>
                    </div>
                    <!-- Rendered as markdown (same as the session search
                         drawer), with rune positions re-applied as <mark>. -->
                    <div
                      class="kb-result-text markdown-body"
                      :ref="(el) => setHitRef(hit.chunk_id, el)"
                      v-html="renderedHits[hit.chunk_id] || ''"
                    />
                  </article>
                </div>
              </div>

              <div v-else class="kb-docs">
                <PanelSkeleton v-if="loadingDocuments && documents.length === 0" variant="list" :rows="6" :meta="true" :label="t('common.loading')" />

                <EmptyState
                  v-else-if="documents.length === 0"
                  :icon="FileText"
                  :title="t('kb.emptyDocuments')"
                  :desc="t('kb.emptyDocumentsDesc')"
                >
                  <template #action>
                    <button class="fbtn fbtn-primary" type="button" @click="openAdd">
                      <Plus :size="15" />
                      <span>{{ t('kb.addFile') }}</span>
                    </button>
                  </template>
                </EmptyState>

                <div v-else class="kb-doc-list">
                  <div v-for="doc in documents" :key="doc.id" class="kb-doc" :data-kb-doc="doc.id">
                    <span class="kb-doc-icon" :class="`kb-doc-icon--${doc.ext?.replace('.', '') || 'file'}`">
                      <FileText :size="16" />
                    </span>
                    <div class="kb-doc-main">
                      <div class="kb-doc-name-line">
                        <span class="kb-doc-name" :title="doc.title">{{ doc.title }}</span>
                        <span class="kb-status" :class="`kb-status--${statusKindOf(doc)}`">{{ statusLabelOf(doc) }}</span>
                      </div>
                      <div class="kb-doc-meta">
                        <span>{{ formatSize(doc.size_bytes) }}</span>
                        <template v-if="doc.chunk_count > 0">
                          <span class="kb-sep">·</span>
                          <span>{{ t('kb.chunks', { count: doc.chunk_count }) }}</span>
                        </template>
                        <template v-if="doc.status === 'ready'">
                          <span class="kb-sep">·</span>
                          <span :class="{ 'kb-muted': !doc.vector_ready }">
                            {{ doc.vector_ready ? t('kb.vectorReady') : t('kb.vectorPending') }}
                          </span>
                        </template>
                        <template v-if="doc.warning === 'truncated'">
                          <span class="kb-sep">·</span>
                          <span class="kb-warn">{{ t('kb.warningTruncated') }}</span>
                        </template>
                      </div>
                      <div v-if="doc.error" class="kb-doc-error">{{ doc.error }}</div>
                    </div>
                    <div class="kb-doc-actions">
                      <button class="kb-icon-btn" type="button" data-action="kb-reindex-doc" :title="t('kb.reindex')" @click="reindexDoc(doc)">
                        <RotateCw :size="14" />
                      </button>
                      <button class="kb-icon-btn is-danger" type="button" data-action="kb-delete-doc" :title="t('common.delete')" @click="removeDoc(doc)">
                        <Trash2 :size="14" />
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            </template>

            <EmptyState
              v-else
              :icon="BookOpen"
              :title="t('kb.selectCollection')"
              :desc="t('kb.selectCollectionDesc')"
            />
          </section>
        </template>
      </SplitView>
    </div>

    <!-- Create collection -->
    <ModalDialog :open="createOpen" :title="t('kb.createTitle')" @close="createOpen = false">
      <div class="kb-form">
        <label class="kb-field">
          <span class="kb-label">{{ t('kb.nameLabel') }}</span>
          <input v-model="createName" class="kb-input" :placeholder="t('kb.namePlaceholder')" maxlength="100" @keydown.enter="submitCreate" />
        </label>
        <label class="kb-field">
          <span class="kb-label">{{ t('kb.descriptionLabel') }}</span>
          <textarea v-model="createDescription" class="kb-input kb-textarea" :placeholder="t('kb.descriptionPlaceholder')" rows="3" />
        </label>
        <div class="kb-field">
          <span class="kb-label">{{ t('kb.scopeLabel') }}</span>
          <div class="kb-scope-options">
            <label class="kb-radio" :class="{ 'is-disabled': !hasProject }">
              <input v-model="createScope" type="radio" value="project" :disabled="!hasProject" />
              <span>
                <strong>{{ t('kb.scopeProject') }}</strong>
                <em>{{ t('kb.scopeProjectHint') }}</em>
              </span>
            </label>
            <label class="kb-radio">
              <input v-model="createScope" type="radio" value="global" />
              <span>
                <strong>{{ t('kb.scopeGlobal') }}</strong>
                <em>{{ t('kb.scopeGlobalHint') }}</em>
              </span>
            </label>
          </div>
        </div>
        <div v-if="createError" class="kb-form-error">{{ createError }}</div>
        <div class="kb-form-actions">
          <button class="fbtn" type="button" @click="createOpen = false">{{ t('common.cancel') }}</button>
          <button class="fbtn fbtn-primary" type="button" :disabled="creating" @click="submitCreate">
            {{ t('kb.create') }}
          </button>
        </div>
      </div>
    </ModalDialog>

    <!-- Add documents. This is the SAME picker the chat composer opens: browse
         the project, upload from the device (with progress + terminate), or
         reuse a recent upload. Re-implementing a bare file browser here lost
         multi-select, upload progress and the mobile upload entry. -->
    <AttachmentPicker
      :open="addOpen"
      variant="kb"
      selection-only
      :allow-dir="false"
      :title="t('kb.addFile')"
      :confirm-label="adding ? t('common.loading') : t('kb.addFile')"
      :busy="adding"
      :current-file="store.state.currentFile?.path || null"
      :current-dir="store.state.projectRoot"
      @close="closeAdd"
      @confirm="submitAdd"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUpdate, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertTriangle,
  BookOpen,
  ChevronLeft,
  ChevronRight,
  Database,
  FileText,
  Plus,
  RotateCw,
  Search,
  Sparkles,
  Trash2,
} from 'lucide-vue-next'
import ActionBar from '@/components/common/ActionBar.vue'
import SplitView from '@/components/common/SplitView.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ModalDialog from '@/components/common/ModalDialog.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import AttachmentPicker from '@/components/attach/AttachmentPicker.vue'
import { useDialog } from '@/composables/useDialog'
import { useKnowledgeBase, type KBCollection, type KBDocument } from '@/composables/useKnowledgeBase'
import { useToast } from '@/composables/useToast'
import { useUiScale } from '@/composables/useUiScale'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import { store } from '@/stores/app'
import type { FileEntry } from '@/utils/fileAttachmentUtils'
import { baseName } from '@/utils/path'
import {
  formatLocation,
  formatSize,
  statusKind,
  statusLabel,
  type TFn,
} from '@/utils/kbFormat'
import { appLog } from '@/utils/appLog'
import { DEFAULT_KB_SPLIT_RATIO, layoutEpoch, readPanelState, writePanelState } from '@/utils/panelState'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer.ts'
import { highlightTermsInElement, termsFromRunePositions } from '@/utils/searchHighlight'

const props = defineProps<{ active?: boolean }>()

const { t } = useI18n()
const { isWideScreen } = useWideScreenLayout()
const uiScale = useUiScale()
const dialog = useDialog()
const toast = useToast()
const kb = useKnowledgeBase()

const {
  collections,
  selectedId,
  documents,
  loadingCollections,
  loadingDocuments,
  error,
  available,
  searchQuery,
  searchScope,
  searchMode,
  searching,
  searchResult,
  searchError,
  selectCollection,
  refresh,
  createCollection,
  deleteCollection,
  setAutoInject,
  addFile,
  deleteDocument,
  reindexDocument,
  reindexCollection,
  clearSearch,
  runSearch,
  startPolling,
  stopPolling,
} = kb

const selected = computed<KBCollection | null>(
  () => collections.value.find((c) => c.id === selectedId.value) ?? null,
)
const hasProject = computed(() => !!store.state.projectRoot)

// ── Layout: split by the PANEL's width, not the window's ──
// The dock pane is 40% of the window, so the same window can hold a wide or a
// narrow panel. Mail does the same: split only when both panes fit, otherwise
// drill down. `clientWidth` is CSS px, so thresholds are scaled.
// At the default 1280px window the dock pane is ~492px. 190 + 280 leaves the
// detail pane usable while still fitting that default, so wide screens get the
// side-by-side layout without the user having to drag the divider first.
const SPLIT_MIN_LIST = 190
const SPLIT_MIN_DETAIL = 280
const SPLIT_MIN_PANEL = SPLIT_MIN_LIST + SPLIT_MIN_DETAIL + 1

const rootEl = ref<HTMLElement | null>(null)
const panelWidth = ref(0)
const split = computed(() => {
  // jsdom / first paint has no measurement yet; fall back to the window class
  // so tests and the first desktop frame behave like a wide panel.
  const width = panelWidth.value || (isWideScreen.value ? 900 : 0)
  return width >= SPLIT_MIN_PANEL * uiScale.value
})

const splitRatio = ref(clampSplitRatio(readPanelState('kbSplitRatio')))

function clampSplitRatio(ratio: number): number {
  return ratio > 0 && ratio < 1 ? ratio : DEFAULT_KB_SPLIT_RATIO
}

function onSplitRatioChange(ratio: number): void {
  splitRatio.value = ratio
  writePanelState('kbSplitRatio', ratio)
}

watch(layoutEpoch, () => {
  splitRatio.value = clampSplitRatio(readPanelState('kbSplitRatio'))
})

function measureWidth(): void {
  const el = rootEl.value
  if (el) panelWidth.value = el.getBoundingClientRect().width
}

let widthObserver: ResizeObserver | null = null
onMounted(() => {
  measureWidth()
  if (typeof ResizeObserver !== 'undefined' && rootEl.value) {
    widthObserver = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width
      if (typeof width === 'number') panelWidth.value = width
    })
    widthObserver.observe(rootEl.value)
  }
})

// When the panel grows from drill-down to split, the list has no selection
// (that is the mobile entry state). Pick the project collection, then the
// first one, so the right pane is not empty.
watch(split, (on) => {
  if (!on || selectedId.value != null || collections.value.length === 0) return
  const target = collections.value.find((c) => c.scope === 'project') ?? collections.value[0]
  if (target) void selectCollection(target.id)
})

const groupedCollections = computed(() => {
  const global = collections.value.filter((c) => c.scope === 'global')
  const project = collections.value.filter((c) => c.scope === 'project')
  const groups: { key: string; labelKey: string; items: KBCollection[] }[] = []
  if (project.length) groups.push({ key: 'project', labelKey: 'kb.projectGroup', items: project })
  if (global.length) groups.push({ key: 'global', labelKey: 'kb.globalGroup', items: global })
  return groups
})

// ── Activation / polling ──
watch(
  () => props.active,
  (on) => {
    if (on) {
      void refresh(split.value)
    } else {
      stopPolling()
    }
  },
  { immediate: true },
)

watch(documents, () => {
  if (documents.value.some((d) => d.status === 'pending' || d.status === 'extracting' || (d.status === 'ready' && !d.vector_ready))) {
    startPolling()
  }
})

onBeforeUnmount(() => {
  stopPolling()
  widthObserver?.disconnect()
  widthObserver = null
})

// ── Create ──
const createOpen = ref(false)
const createName = ref('')
const createDescription = ref('')
const createScope = ref<'project' | 'global'>('project')
const createError = ref('')
const creating = ref(false)

function openCreate(): void {
  createName.value = ''
  createDescription.value = ''
  createScope.value = hasProject.value ? 'project' : 'global'
  createError.value = ''
  createOpen.value = true
}

async function submitCreate(): Promise<void> {
  const name = createName.value.trim()
  if (!name) {
    createError.value = t('kb.nameRequired')
    return
  }
  creating.value = true
  createError.value = ''
  try {
    await createCollection(name, createDescription.value.trim(), createScope.value)
    createOpen.value = false
    toast.show(t('kb.added'), { type: 'success', duration: 1800 })
  } catch (e) {
    createError.value = e instanceof Error ? e.message : String(e)
  } finally {
    creating.value = false
  }
}

// ── Collection actions ──
async function toggleAutoInject(): Promise<void> {
  if (!selected.value) return
  try {
    await setAutoInject(selected.value.id, !selected.value.auto_inject)
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

async function removeCurrent(): Promise<void> {
  const col = selected.value
  if (!col) return
  const ok = await dialog.confirm(
    t('kb.deleteCollectionConfirm', { name: col.name, count: col.doc_count }),
    { dangerous: true },
  )
  if (!ok) return
  try {
    await deleteCollection(col.id)
    toast.show(t('kb.deleted'), { type: 'success', duration: 1800 })
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

async function reindexCurrent(): Promise<void> {
  const col = selected.value
  if (!col) return
  const ok = await dialog.confirm(t('kb.reindexCollectionConfirm', { name: col.name }))
  if (!ok) return
  try {
    await reindexCollection(col.id)
    toast.show(t('kb.reindexQueued'), { type: 'success', duration: 1800 })
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

function backToCollections(): void {
  selectedId.value = null
  clearSearch()
}

// ── Add documents ──
// The selection is owned by AttachmentPicker; this panel only applies it. A
// batch is added one file at a time (the API takes one path), so a single
// unsupported file does not throw away the rest of the batch.
const addOpen = ref(false)
const adding = ref(false)

function openAdd(): void {
  addOpen.value = true
}

function closeAdd(): void {
  addOpen.value = false
}

async function submitAdd(entries: FileEntry[]): Promise<void> {
  if (adding.value) return
  const files = entries.filter((e) => !e.isDir)
  if (files.length === 0) return
  adding.value = true
  const failed: string[] = []
  try {
    for (const entry of files) {
      try {
        await addFile(entry.path)
      } catch (e) {
        failed.push(`${baseName(entry.path)}: ${e instanceof Error ? e.message : String(e)}`)
      }
    }
  } finally {
    adding.value = false
  }
  if (failed.length === 0) {
    closeAdd()
    toast.show(t('kb.added'), { type: 'success', duration: 1800 })
    return
  }
  // Keep the sheet open on failure: the selection is still there, so the user
  // can drop the rejected file and retry without browsing again.
  const first = failed[0]
  toast.show(
    failed.length === 1 ? first : t('kb.addPartial', { first, count: failed.length - 1 }),
    { icon: 'warning', type: 'error', duration: 4000 },
  )
}

// ── Document actions ──
async function removeDoc(doc: KBDocument): Promise<void> {
  const ok = await dialog.confirm(t('kb.deleteDocumentConfirm', { name: doc.title }), { dangerous: true })
  if (!ok) return
  try {
    await deleteDocument(doc.id)
    toast.show(t('kb.deleted'), { type: 'success', duration: 1800 })
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

async function reindexDoc(doc: KBDocument): Promise<void> {
  try {
    await reindexDocument(doc.id)
    toast.show(t('kb.reindexDocQueued'), { type: 'success', duration: 1800 })
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

// ── Presentation helpers ──
// The neutral label → localized wording, highlight splitting and status
// mapping live in utils/kbFormat.ts so they can be unit-tested without mounting
// the panel (which pulls in the file picker and the drawer stack).
const tf: TFn = (key, named) => t(key, named ?? {})

function locationText(loc?: string): string {
  return formatLocation(loc, tf)
}

// ── Search result rendering ──
// The chunk text is markdown (the document IS markdown), so plain-text
// snippets showed raw `#` headings. Render the same way the session search
// drawer does, then re-apply the server's match positions as <mark> terms.
const renderedHits = computed<Record<number, string>>(() => {
  const map: Record<number, string> = {}
  for (const hit of searchResult.value?.results ?? []) {
    map[hit.chunk_id] = renderMarkdownHtml(hit.chunk_text, {
      skipEnhancements: true,
      wrapTables: false,
    })
  }
  return map
})

const hitRefs = new Map<number, HTMLElement>()
function setHitRef(id: number, el: unknown): void {
  if (el instanceof HTMLElement) hitRefs.set(id, el)
  else hitRefs.delete(id)
}

onBeforeUpdate(() => hitRefs.clear())

function applyHitHighlights(): void {
  for (const hit of searchResult.value?.results ?? []) {
    const el = hitRefs.get(hit.chunk_id)
    if (!el) continue
    highlightTermsInElement(el, termsFromRunePositions(hit.chunk_text, hit.match_positions), 'kb-mark')
  }
}

watch(searchResult, () => nextTick(applyHitHighlights))

function statusKindOf(doc: KBDocument): string {
  return statusKind(doc)
}

function statusLabelOf(doc: KBDocument): string {
  return statusLabel(doc, tf)
}

// A failed status refresh must not hide the panel; the collections call is the
// authoritative "is this usable" signal.
watch(available, (on) => {
  if (!on) appLog.w('KnowledgeBase', 'knowledge base is not available')
})

// Expose for tests.
defineExpose({
  formatLocation: locationText,
  statusKind: statusKindOf,
  statusLabel: statusLabelOf,
})
</script>

<style scoped>
/* ── Panel shell ── */
.kb-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  background: var(--bg-primary);
}

.kb-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  height: var(--header-height);
  padding: 0 var(--space-2) 0 var(--space-6);
  background: var(--bg-primary);
  border-bottom: 1px solid var(--border-color);
  flex: none;
}

.kb-title-line {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.kb-title {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  white-space: nowrap;
}

.kb-count {
  flex: none;
  padding: 1px var(--space-2);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
}

.kb-header-actions {
  flex: 1 1 auto;
}

/* Only what the shared ActionBar row rules do NOT set. `display: flex` is one
   of them and it is not optional: a <button> defaults to inline-block, so the
   icon and the label line up on the text baseline and the icon sits visibly
   high next to the word once the bar is wide enough to show labels. */
.header-btn {
  border: none;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}

.kb-body {
  flex: 1;
  min-height: 0;
}

.kb-split {
  height: 100%;
}

/* ── Collection rail ── */
.kb-rail {
  height: 100%;
  overflow-y: auto;
  padding: var(--panel-body-pad);
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  background: var(--bg-primary);
}

.kb-group {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.kb-group-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 var(--space-2);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.kb-group-count {
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}

.kb-col {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  padding: var(--space-3) var(--space-4);
  border: 1px solid transparent;
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: background var(--duration-fast), border-color var(--duration-fast);
}

@media (hover: hover) {
  .kb-col:hover {
    border-color: var(--border-color);
    background: color-mix(in srgb, var(--text-primary) 4%, var(--bg-secondary));
  }
}

.kb-col.is-active {
  border-color: color-mix(in srgb, var(--accent-color) 45%, transparent);
  background: color-mix(in srgb, var(--accent-color) 10%, var(--bg-secondary));
}

.kb-col-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: calc(30 * var(--dp));
  height: calc(30 * var(--dp));
  border-radius: var(--radius-sm);
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}

.kb-col-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1;
}

.kb-col-name {
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-col-meta {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}

.kb-col-off {
  color: var(--color-orange);
}

.kb-col-chevron {
  flex: none;
  color: var(--text-muted);
}

/* ── Detail pane ── */
.kb-main {
  height: 100%;
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: var(--bg-primary);
}

.kb-main-head {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-5) var(--space-5) var(--space-3);
  border-bottom: 1px solid var(--border-color);
  flex: none;
}

.kb-main-title-line {
  display: flex;
  align-items: flex-start;
  gap: var(--space-2);
  min-width: 0;
}

.kb-main-title-text {
  min-width: 0;
  flex: 1;
}

.kb-main-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: calc(34 * var(--dp));
  height: calc(34 * var(--dp));
  border-radius: var(--radius-md);
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}

.kb-main-name {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
  color: var(--text-primary);
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
}

.kb-main-name-text {
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-main-desc {
  margin-top: var(--space-1);
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  line-height: 1.45;
}

.kb-scope {
  flex: none;
  padding: 1px var(--space-2);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-full);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-medium);
}

.kb-scope--global {
  color: var(--accent-color);
  border-color: color-mix(in srgb, var(--accent-color) 40%, transparent);
}

/* Whether chat retrieval includes this collection. A property of the
   collection, so it sits beside the scope tag rather than in the action bar. */
.kb-inject-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  flex: none;
  padding: 1px var(--space-2);
  border: 1px solid color-mix(in srgb, var(--accent-color) 35%, transparent);
  border-radius: var(--radius-full);
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
  color: var(--accent-color);
  font-size: var(--font-size-2xs);
  font-family: inherit;
  cursor: pointer;
}

.kb-inject-chip.is-off {
  border-color: var(--border-color);
  background: transparent;
  color: var(--text-muted);
}

.kb-inject-state {
  padding-left: var(--space-1);
  border-left: 1px solid currentColor;
  opacity: 0.75;
}

.kb-main-actions {
  width: 100%;
}

.kb-back {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  padding: var(--space-1);
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}

.kb-back:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

/* ── Search ── */
.kb-search-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-5);
  border-bottom: 1px solid var(--border-color);
  flex: none;
}

.kb-search {
  flex: 1 1 calc(170 * var(--dp));
  min-width: calc(130 * var(--dp));
}

.kb-search-controls {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 0 0 auto;
}

.kb-select {
  height: calc(30 * var(--dp));
  padding: 0 var(--space-2);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-xs);
}

/* ── Result / document bodies ── */
.kb-results,
.kb-docs {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: var(--panel-body-pad);
}

.kb-results-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  margin-bottom: var(--space-3);
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}

.kb-results-count {
  font-variant-numeric: tabular-nums;
}

.kb-result-list,
.kb-doc-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

/* ── Search result card ── */
.kb-result {
  padding: var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
}

.kb-result-head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.kb-result-title {
  min-width: 0;
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-result-loc {
  flex: none;
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--font-size-2xs);
}

.kb-result-col {
  min-width: 0;
  margin-left: auto;
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-result-text {
  margin-top: var(--space-2);
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  line-height: 1.55;
  overflow-wrap: anywhere;
  /* A chunk can be a whole document section; cap the card and scroll inside
     instead of letting one result push the others (and the pane) down. */
  max-height: calc(320 * var(--dp));
  overflow-y: auto;
  overscroll-behavior: contain;
}

/* The global .markdown-body rules set reading-pane rhythm; this card is
   compact, so pull block margins in and drop the heading rule line. */
.kb-result-text :deep(h1),
.kb-result-text :deep(h2),
.kb-result-text :deep(h3),
.kb-result-text :deep(h4),
.kb-result-text :deep(p),
.kb-result-text :deep(ul),
.kb-result-text :deep(ol) {
  margin: calc(4 * var(--dp)) 0;
  font-size: inherit;
  line-height: inherit;
  color: inherit;
}

.kb-result-text :deep(h1),
.kb-result-text :deep(h2),
.kb-result-text :deep(h3),
.kb-result-text :deep(h4) {
  font-weight: var(--font-weight-semibold);
  border: 0;
  padding: 0;
}

/* The <mark> elements are injected into rendered HTML, so a scoped rule
   without :deep() would never match them. */
.kb-result-text :deep(mark.kb-mark) {
  background: color-mix(in srgb, var(--accent-color) 24%, transparent);
  color: inherit;
  border-radius: 2px;
  padding: 0 1px;
}

/* ── Document rows ── */
.kb-doc-list {
  gap: 0;
}

.kb-doc {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-2);
  border-bottom: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  transition: background var(--duration-fast);
}

.kb-doc:last-child {
  border-bottom: 0;
}

@media (hover: hover) {
  .kb-doc:hover {
    background: color-mix(in srgb, var(--text-primary) 3%, transparent);
  }
}

.kb-doc-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: calc(32 * var(--dp));
  height: calc(32 * var(--dp));
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-secondary);
}

.kb-doc-icon--pdf { color: var(--color-red); background: color-mix(in srgb, var(--color-red) 10%, transparent); }
.kb-doc-icon--docx { color: var(--color-info); background: color-mix(in srgb, var(--color-info) 10%, transparent); }
.kb-doc-icon--xlsx { color: var(--color-green); background: color-mix(in srgb, var(--color-green) 10%, transparent); }
.kb-doc-icon--pptx { color: var(--color-orange); background: color-mix(in srgb, var(--color-orange) 10%, transparent); }
.kb-doc-icon--md { color: var(--color-purple); background: color-mix(in srgb, var(--color-purple) 10%, transparent); }

.kb-doc-main {
  flex: 1;
  min-width: 0;
}

.kb-doc-name-line {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.kb-doc-name {
  min-width: 0;
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-status {
  flex: none;
  padding: 1px var(--space-2);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  white-space: nowrap;
}

.kb-status--ready {
  background: color-mix(in srgb, var(--color-green) 12%, transparent);
  color: var(--color-green);
}

.kb-status--fts {
  background: color-mix(in srgb, var(--color-orange) 12%, transparent);
  color: var(--color-orange);
}

.kb-status--failed {
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
  color: var(--color-red);
}

.kb-status--working,
.kb-status--pending {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  color: var(--accent-color);
}

.kb-doc-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-2);
  margin-top: var(--space-1);
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}

.kb-sep {
  opacity: 0.6;
}

.kb-muted {
  color: var(--text-muted);
}

.kb-warn {
  color: var(--color-orange);
}

.kb-doc-error {
  margin-top: var(--space-1);
  color: var(--color-red);
  font-size: var(--font-size-xs);
  overflow-wrap: anywhere;
}

.kb-doc-actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex: none;
  opacity: 0;
  transition: opacity var(--duration-fast);
}

.kb-doc:hover .kb-doc-actions,
.kb-doc:focus-within .kb-doc-actions {
  opacity: 1;
}

@media (hover: none) {
  .kb-doc-actions { opacity: 1; }
}

.kb-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  padding: 0;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.kb-icon-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.kb-icon-btn.is-danger:hover {
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  color: var(--color-red);
}

/* ── Small helpers ── */
.kb-hint {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-5);
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
}

.kb-hint--error {
  color: var(--color-red);
}

.kb-link {
  border: 0;
  background: transparent;
  color: var(--accent-color);
  font-size: var(--font-size-xs);
  cursor: pointer;
}

.kb-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-8);
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  text-align: center;
}

.kb-empty-icon {
  opacity: 0.6;
}

.kb-empty-desc {
  max-width: calc(320 * var(--dp));
  font-size: var(--font-size-xs);
}

/* ── Create form ── */
.kb-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  padding: var(--space-1) 0 0;
}

.kb-field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.kb-label {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}

.kb-input {
  width: 100%;
  box-sizing: border-box;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
}

.kb-textarea {
  resize: vertical;
  font-family: inherit;
}

.kb-scope-options {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.kb-radio {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  cursor: pointer;
}

.kb-radio.is-disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.kb-radio span {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.kb-radio strong {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
}

.kb-radio em {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  font-style: normal;
}

.kb-form-error {
  color: var(--color-red);
  font-size: var(--font-size-xs);
}

.kb-form-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-3);
}

</style>
