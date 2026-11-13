<template>
  <div class="kb-panel" :class="{ 'is-narrow': !isWideScreen }">
    <!-- Header -->
    <div class="kb-header">
      <div class="kb-title-line">
        <BookOpen :size="16" class="kb-title-icon" />
        <span class="kb-title">{{ t('kb.title') }}</span>
        <span v-if="collections.length" class="kb-count">{{ t('kb.count', { count: collections.length }) }}</span>
      </div>
      <ActionBar class="kb-header-actions" mode="menu" align="end" size="header">
        <RefreshButton
          class="header-btn"
          :loading="loadingCollections"
          :disabled="loadingCollections"
          :title="t('kb.refresh')"
          @click="refresh(isWideScreen)"
        />
        <button class="header-btn" type="button" data-action="kb-new" :title="t('kb.newCollection')" @click="openCreate">
          <Plus :size="15" />
          <span class="action-label">{{ t('kb.newCollection') }}</span>
        </button>
      </ActionBar>
    </div>

    <div v-if="!available" class="kb-empty kb-empty--blocked">
      <Database :size="28" class="kb-empty-icon" />
      <span>{{ t('kb.loadFailed') }}</span>
      <span class="kb-empty-desc">{{ t('kb.emptyCollectionsDesc') }}</span>
    </div>

    <div v-else class="kb-body">
      <!-- Collection rail -->
      <aside v-show="isWideScreen || selectedId == null" class="kb-rail">
        <PanelSkeleton v-if="loadingCollections && collections.length === 0" variant="list" :rows="6" :meta="true" :label="t('common.loading')" />

        <div v-else-if="error" class="kb-hint kb-hint--error">
          <AlertTriangle :size="16" />
          <span>{{ error }}</span>
          <button class="kb-link" type="button" @click="refresh(isWideScreen)">{{ t('common.retry') }}</button>
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
            <div class="kb-group-title">{{ t(group.labelKey) }}</div>
            <button
              v-for="col in group.items"
              :key="col.id"
              type="button"
              class="kb-col"
              :class="{ 'is-active': col.id === selectedId }"
              :data-kb-collection="col.id"
              @click="selectCollection(col.id)"
            >
              <span class="kb-col-icon">
                <BookOpen :size="15" />
              </span>
              <span class="kb-col-text">
                <span class="kb-col-name">{{ col.name }}</span>
                <span class="kb-col-meta">
                  {{ t('kb.documents', { count: col.doc_count }) }}
                  <span v-if="!col.auto_inject" class="kb-col-off">{{ t('kb.autoInjectOff') }}</span>
                </span>
              </span>
            </button>
          </div>
        </template>
      </aside>

      <!-- Main area -->
      <section v-show="isWideScreen || selectedId != null" class="kb-main">
        <template v-if="selected">
          <div class="kb-main-head">
            <div class="kb-main-title-line">
              <button
                v-if="!isWideScreen"
                class="kb-back"
                type="button"
                :title="t('common.back')"
                @click="backToCollections"
              >
                <ChevronLeft :size="18" />
              </button>
              <div class="kb-main-title-text">
                <div class="kb-main-name">
                  {{ selected.name }}
                  <span class="kb-scope" :class="`kb-scope--${selected.scope}`">
                    {{ selected.scope === 'global' ? t('kb.scopeGlobal') : t('kb.scopeProject') }}
                  </span>
                </div>
                <div v-if="selected.description" class="kb-main-desc">{{ selected.description }}</div>
              </div>
            </div>

            <ActionBar class="kb-main-actions" mode="menu" align="end" size="bar">
              <button
                class="header-btn"
                type="button"
                data-action="kb-toggle-auto-inject"
                :title="selected.auto_inject ? t('kb.autoInjectOn') : t('kb.autoInjectOff')"
                :class="{ 'is-off': !selected.auto_inject }"
                @click="toggleAutoInject"
              >
                <Sparkles :size="14" />
                <span class="action-label">{{ selected.auto_inject ? t('kb.autoInjectOn') : t('kb.autoInjectOff') }}</span>
              </button>
              <button class="header-btn" type="button" data-action="kb-add" :title="t('kb.addFile')" @click="openAdd">
                <Plus :size="15" />
                <span class="action-label">{{ t('kb.addFile') }}</span>
              </button>
              <button class="header-btn" type="button" data-action="kb-reindex-collection" :title="t('kb.reindex')" @click="reindexCurrent">
                <RotateCw :size="14" />
                <span class="action-label">{{ t('kb.reindex') }}</span>
              </button>
              <button class="header-btn is-danger" type="button" data-action="kb-delete-collection" :title="t('kb.deleteCollection')" @click="removeCurrent">
                <Trash2 :size="14" />
              </button>
            </ActionBar>
          </div>

          <!-- Search -->
          <div class="kb-search-row">
            <SearchInput
              v-model="searchQuery"
              class="kb-search"
              :placeholder="t('kb.searchPlaceholder')"
              @enter="runSearch"
            />
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

          <!-- Search results -->
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
              <div v-for="hit in searchResult?.results ?? []" :key="hit.chunk_id" class="kb-result" :data-kb-hit="hit.chunk_id">
                <div class="kb-result-head">
                  <span class="kb-result-title">{{ hit.document_title }}</span>
                  <span v-if="hit.location" class="kb-result-loc">{{ locationText(hit.location) }}</span>
                  <span class="kb-result-col">{{ t('kb.inCollection', { name: hit.collection_name }) }}</span>
                </div>
                <div class="kb-result-text">
                  <template v-for="(part, i) in snippetPartsOf(hit)" :key="i">
                    <mark v-if="part.hit" class="kb-mark">{{ part.text }}</mark>
                    <span v-else>{{ part.text }}</span>
                  </template>
                </div>
              </div>
            </div>
          </div>

          <!-- Documents -->
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

    <!-- Add file -->
    <BottomSheet :open="addOpen" :title="t('kb.pickServerFile')" maximized @close="closeAdd">
      <div class="kb-add">
        <div class="kb-add-toolbar">
          <span class="kb-add-hint">{{ t('kb.pickFileHint') }}</span>
          <button class="header-btn" type="button" @click="pickUpload">
            <Upload :size="14" />
            <span class="action-label">{{ t('kb.uploadFile') }}</span>
          </button>
          <input ref="fileInput" type="file" class="kb-file-input" @change="onUploadPicked" />
        </div>
        <div class="kb-picker-wrap">
          <FilePicker
            :open="addOpen"
            :start-dir="store.state.projectRoot"
            :current-file="store.state.currentFile?.path || null"
            :selected="draft"
            :allow-dir="false"
            @update:selected="draft = $event"
          />
        </div>
        <div class="kb-add-actions">
          <button class="fbtn" type="button" @click="closeAdd">{{ t('common.cancel') }}</button>
          <button class="fbtn fbtn-primary" type="button" :disabled="adding || draft.length === 0" @click="submitAdd">
            {{ adding ? t('common.loading') : t('kb.addFile') }}
          </button>
        </div>
      </div>
    </BottomSheet>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertTriangle,
  BookOpen,
  ChevronLeft,
  Database,
  FileText,
  Plus,
  RotateCw,
  Search,
  Sparkles,
  Trash2,
  Upload,
} from 'lucide-vue-next'
import ActionBar from '@/components/common/ActionBar.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ModalDialog from '@/components/common/ModalDialog.vue'
import BottomSheet from '@/components/common/BottomSheet.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import FilePicker from '@/components/file/FilePicker.vue'
import { useDialog } from '@/composables/useDialog'
import { useKnowledgeBase, type KBCollection, type KBDocument, type KBSearchHit } from '@/composables/useKnowledgeBase'
import { useToast } from '@/composables/useToast'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import { store } from '@/stores/app'
import type { FileEntry } from '@/utils/fileAttachmentUtils'
import {
  formatLocation,
  formatSize,
  snippetParts,
  statusKind,
  statusLabel,
  type SnippetPart,
  type TFn,
} from '@/utils/kbFormat'
import { appLog } from '@/utils/appLog'

const props = defineProps<{ active?: boolean }>()

const { t } = useI18n()
const { isWideScreen } = useWideScreenLayout()
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
  uploadFile,
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
      void refresh(isWideScreen.value)
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

onBeforeUnmount(() => stopPolling())

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

// ── Add file ──
const addOpen = ref(false)
const draft = ref<FileEntry[]>([])
const adding = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

function openAdd(): void {
  draft.value = []
  addOpen.value = true
}

function closeAdd(): void {
  addOpen.value = false
  draft.value = []
}

async function submitAdd(): Promise<void> {
  const entry = draft.value.find((e) => !e.isDir)
  if (!entry) return
  adding.value = true
  try {
    await addFile(entry.path)
    closeAdd()
    toast.show(t('kb.added'), { type: 'success', duration: 1800 })
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3200 })
  } finally {
    adding.value = false
  }
}

function pickUpload(): void {
  fileInput.value?.click()
}

async function onUploadPicked(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  adding.value = true
  try {
    await uploadFile(file)
    closeAdd()
    toast.show(t('kb.uploaded'), { type: 'success', duration: 1800 })
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3200 })
  } finally {
    adding.value = false
  }
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

function snippetPartsOf(hit: KBSearchHit): SnippetPart[] {
  return snippetParts(hit)
}

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
  snippetParts: snippetPartsOf,
  statusKind: statusKindOf,
  statusLabel: statusLabelOf,
})
</script>

<style scoped>
.kb-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
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
  gap: calc(6 * var(--dp));
  min-width: 0;
}

.kb-title-icon {
  color: var(--text-secondary);
  flex: none;
}

.kb-title {
  font-size: var(--font-size-md);
  font-weight: 600;
  color: var(--text-primary);
  white-space: nowrap;
}

.kb-count {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  white-space: nowrap;
}

.kb-header-actions {
  flex: 1 1 auto;
}

/* Only what the shared ActionBar row rules do NOT set. */
.header-btn {
  border: none;
  cursor: pointer;
}

.kb-body {
  display: flex;
  flex: 1;
  min-height: 0;
}

.kb-rail {
  width: calc(220 * var(--dp));
  flex: none;
  border-right: 1px solid var(--border-color);
  overflow-y: auto;
  padding: calc(6 * var(--dp));
}

.kb-group + .kb-group {
  margin-top: calc(10 * var(--dp));
}

.kb-group-title {
  padding: calc(4 * var(--dp)) calc(8 * var(--dp));
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.kb-col {
  display: flex;
  align-items: flex-start;
  gap: calc(8 * var(--dp));
  width: 100%;
  padding: calc(7 * var(--dp)) calc(8 * var(--dp));
  border: 0;
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
}

.kb-col:hover {
  background: var(--bg-hover);
}

.kb-col.is-active {
  background: var(--bg-active);
}

.kb-col-icon {
  color: var(--text-secondary);
  margin-top: 1px;
  flex: none;
}

.kb-col-text {
  display: flex;
  flex-direction: column;
  gap: calc(2 * var(--dp));
  min-width: 0;
}

.kb-col-name {
  font-size: var(--font-size-sm);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-col-meta {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  display: flex;
  gap: calc(6 * var(--dp));
  align-items: center;
}

.kb-col-off {
  color: var(--warning, #b7791f);
}

.kb-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.kb-main-head {
  display: flex;
  align-items: flex-start;
  gap: calc(8 * var(--dp));
  padding: calc(10 * var(--dp)) calc(12 * var(--dp)) calc(6 * var(--dp));
  flex: none;
}

.kb-main-title-line {
  display: flex;
  align-items: flex-start;
  gap: calc(6 * var(--dp));
  min-width: 0;
  flex: 1;
}

.kb-main-title-text {
  min-width: 0;
}

.kb-main-name {
  display: flex;
  align-items: center;
  gap: calc(6 * var(--dp));
  font-size: var(--font-size-md);
  font-weight: 600;
  color: var(--text-primary);
}

.kb-main-desc {
  margin-top: calc(2 * var(--dp));
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.kb-scope {
  font-size: 10px;
  font-weight: 500;
  padding: 1px calc(5 * var(--dp));
  border-radius: var(--radius-full);
  border: 1px solid var(--border-color);
  color: var(--text-muted);
}

.kb-scope--global {
  color: var(--accent-color);
  border-color: color-mix(in srgb, var(--accent-color) 40%, transparent);
}

.kb-main-actions {
  flex: 0 0 auto;
}

.kb-back {
  border: 0;
  background: transparent;
  color: var(--text-secondary);
  padding: calc(2 * var(--dp));
  cursor: pointer;
}

.kb-search-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: calc(6 * var(--dp));
  row-gap: var(--space-2);
  padding: 0 calc(12 * var(--dp)) calc(8 * var(--dp));
  flex: none;
}

/* The input must keep a usable width. Without a flex basis it shrank to zero
   when the rail left the main column ~280px wide (the two selects took the
   rest), making the box invisible and unfocusable. With a basis the row wraps
   the selects onto their own line instead. */
.kb-search {
  flex: 1 1 calc(180 * var(--dp));
  min-width: calc(140 * var(--dp));
}

.kb-select {
  height: calc(30 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-xs);
  padding: 0 calc(4 * var(--dp));
}

.kb-results,
.kb-docs {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 0 calc(12 * var(--dp)) calc(12 * var(--dp));
}

.kb-results-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: calc(4 * var(--dp)) 0 calc(6 * var(--dp));
}

.kb-results-count {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.kb-result {
  padding: calc(8 * var(--dp)) 0;
  border-bottom: 1px solid var(--border-color);
}

.kb-result-head {
  display: flex;
  align-items: baseline;
  gap: calc(6 * var(--dp));
  flex-wrap: wrap;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.kb-result-title {
  font-size: var(--font-size-sm);
  font-weight: 600;
  color: var(--text-primary);
}

.kb-result-loc {
  color: var(--accent-color);
}

.kb-result-text {
  margin-top: calc(4 * var(--dp));
  font-size: var(--font-size-sm);
  line-height: 1.55;
  color: var(--text-secondary);
  white-space: pre-wrap;
  word-break: break-word;
}

.kb-mark {
  background: color-mix(in srgb, var(--accent-color) 24%, transparent);
  color: var(--text-primary);
  border-radius: 2px;
  padding: 0 1px;
}

.kb-doc {
  display: flex;
  align-items: flex-start;
  gap: calc(8 * var(--dp));
  padding: calc(8 * var(--dp)) 0;
  border-bottom: 1px solid var(--border-color);
}

.kb-doc-icon {
  color: var(--text-secondary);
  margin-top: 1px;
  flex: none;
}

.kb-doc-main {
  flex: 1;
  min-width: 0;
}

.kb-doc-name-line {
  display: flex;
  align-items: center;
  gap: calc(6 * var(--dp));
  min-width: 0;
}

.kb-doc-name {
  font-size: var(--font-size-sm);
  font-weight: 500;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-status {
  font-size: 10px;
  padding: 1px calc(5 * var(--dp));
  border-radius: var(--radius-full);
  white-space: nowrap;
  border: 1px solid var(--border-color);
  color: var(--text-muted);
}

.kb-status--ready {
  color: var(--success, #2f855a);
  border-color: color-mix(in srgb, var(--success, #2f855a) 40%, transparent);
}

.kb-status--fts {
  color: var(--warning, #b7791f);
  border-color: color-mix(in srgb, var(--warning, #b7791f) 40%, transparent);
}

.kb-status--failed {
  color: var(--color-red);
  border-color: color-mix(in srgb, var(--color-red) 40%, transparent);
}

.kb-status--working,
.kb-status--pending {
  color: var(--accent-color);
  border-color: color-mix(in srgb, var(--accent-color) 40%, transparent);
}

.kb-doc-meta {
  display: flex;
  align-items: center;
  gap: calc(4 * var(--dp));
  flex-wrap: wrap;
  margin-top: calc(2 * var(--dp));
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.kb-sep {
  opacity: 0.6;
}

.kb-muted {
  color: var(--text-muted);
}

.kb-warn {
  color: var(--warning, #b7791f);
}

.kb-doc-error {
  margin-top: calc(3 * var(--dp));
  font-size: var(--font-size-xs);
  color: var(--color-red);
  word-break: break-word;
}

.kb-doc-actions {
  display: flex;
  gap: calc(2 * var(--dp));
  flex: none;
}

.kb-icon-btn {
  border: 0;
  background: transparent;
  color: var(--text-muted);
  padding: calc(3 * var(--dp));
  border-radius: var(--radius-sm);
  cursor: pointer;
}

.kb-icon-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.kb-icon-btn.is-danger:hover {
  color: var(--color-red);
}

.kb-hint {
  display: flex;
  align-items: center;
  gap: calc(6 * var(--dp));
  padding: calc(10 * var(--dp));
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}

.kb-hint--error {
  color: var(--color-red);
}

.kb-link {
  border: 0;
  background: transparent;
  color: var(--accent-color);
  cursor: pointer;
  font-size: var(--font-size-xs);
}

.kb-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: calc(6 * var(--dp));
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}

.kb-empty-icon {
  opacity: 0.6;
}

.kb-empty-desc {
  font-size: var(--font-size-xs);
  max-width: calc(320 * var(--dp));
  text-align: center;
}

.kb-form {
  display: flex;
  flex-direction: column;
  gap: calc(12 * var(--dp));
  padding: calc(4 * var(--dp)) calc(2 * var(--dp)) calc(2 * var(--dp));
}

.kb-field {
  display: flex;
  flex-direction: column;
  gap: calc(4 * var(--dp));
}

.kb-label {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.kb-input {
  width: 100%;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  padding: calc(7 * var(--dp)) calc(8 * var(--dp));
  box-sizing: border-box;
}

.kb-textarea {
  resize: vertical;
  font-family: inherit;
}

.kb-scope-options {
  display: flex;
  flex-direction: column;
  gap: calc(6 * var(--dp));
}

.kb-radio {
  display: flex;
  align-items: flex-start;
  gap: calc(8 * var(--dp));
  padding: calc(7 * var(--dp)) calc(8 * var(--dp));
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
  font-weight: 500;
}

.kb-radio em {
  font-size: var(--font-size-xs);
  font-style: normal;
  color: var(--text-muted);
}

.kb-form-error {
  font-size: var(--font-size-xs);
  color: var(--color-red);
}

.kb-form-actions {
  display: flex;
  justify-content: flex-end;
  gap: calc(8 * var(--dp));
}

.kb-add {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.kb-add-toolbar {
  display: flex;
  align-items: center;
  gap: calc(8 * var(--dp));
  padding: calc(6 * var(--dp)) calc(12 * var(--dp));
  border-bottom: 1px solid var(--border-color);
  flex: none;
}

.kb-add-hint {
  flex: 1;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.kb-file-input {
  display: none;
}

.kb-picker-wrap {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.kb-picker-wrap :deep(.fp) {
  height: 100%;
  min-height: 0;
}

.kb-add-actions {
  display: flex;
  justify-content: flex-end;
  gap: calc(8 * var(--dp));
  padding: calc(8 * var(--dp)) calc(12 * var(--dp));
  border-top: 1px solid var(--border-color);
  flex: none;
}

/* Narrow screens: one column at a time. The rail fills the panel until a
   collection is picked; the main area then takes over with a back button. */
.kb-panel.is-narrow .kb-body {
  flex-direction: column;
}

.kb-panel.is-narrow .kb-rail {
  width: 100%;
  border-right: 0;
}

.kb-panel.is-narrow .kb-main {
  width: 100%;
}
</style>
