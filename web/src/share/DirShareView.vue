<template>
  <!--
    A file inside the shared folder: hand off to the normal file view. It owns
    its own topbar, in-document link history and preview branches, so this view
    only has to provide the Back target (the containing folder).
  -->
  <ShareView
    v-if="mode === 'file'"
    :initial-path="filePath"
    can-back-out
    @back-out="backToFolder"
  />

  <div v-else class="share-view dir-share">
    <div class="share-topbar">
      <img class="share-logo" src="/logo-64.png" alt="ClawBench" />
      <button
        v-if="canGoUp"
        class="share-btn dir-up-btn"
        type="button"
        :title="t('share.dir.up')"
        :aria-label="t('share.dir.up')"
        @click="goUp"
      >
        <ArrowLeft :size="16" />
      </button>
      <span class="share-file-name" :title="title">{{ title }}</span>
      <span v-if="loading" class="share-status">{{ t('share.loading') }}</span>
      <span v-else-if="error" class="share-status share-error">{{ error }}</span>
      <span class="share-spacer" />
      <!-- Same fixed right-most slot as the file view: the palette picker and
           the fit switch are the controls a reader needs even when the listing
           failed. -->
      <ShareFitButton />
      <ShareThemeButton />
    </div>

    <div class="share-body">
      <div class="share-content dir-share-content">
        <nav v-if="crumbs.length > 1" class="dir-crumbs" :aria-label="t('share.dir.title')">
          <template v-for="(crumb, i) in crumbs" :key="crumb.path">
            <span v-if="i > 0" class="dir-crumb-sep" aria-hidden="true">/</span>
            <button
              v-if="i < crumbs.length - 1"
              class="dir-crumb"
              type="button"
              @click="goToDir(crumb.path)"
            >{{ crumb.name }}</button>
            <span v-else class="dir-crumb dir-crumb--current">{{ crumb.name }}</span>
          </template>
        </nav>

        <div v-if="loading" class="dir-skeleton">
          <PanelSkeleton variant="text" :rows="8" :label="t('common.loading')" />
        </div>

        <div v-else-if="error" class="share-error-state">
          <FileX2 :size="40" />
          <div class="share-error-title">{{ t('share.invalidTitle') }}</div>
          <div class="share-error-desc">{{ error }}</div>
          <!-- A deep link into a folder/file that has since been removed leaves
               the reader with no listing to navigate from. The root of the
               share is always a valid target, so offer it instead of a dead end. -->
          <button v-if="showErrorBack" class="share-btn dir-error-back" type="button" @click="backToRoot">
            <ArrowLeft :size="14" />
            {{ t('share.dir.backToRoot') }}
          </button>
        </div>

        <div v-else-if="!entries.length" class="dir-empty">
          <FolderOpen :size="40" />
          <div class="dir-empty-text">{{ t('share.dir.empty') }}</div>
        </div>

        <div v-else class="dir-list">
          <div
            v-for="entry in entries"
            :key="entry.name"
            class="dir-row"
            :class="{ 'dir-row--folder': entry.type === 'dir' }"
            role="button"
            tabindex="0"
            :data-entry-type="entry.type"
            :data-entry-name="entry.name"
            @click="openEntry(entry)"
            @keydown.enter.self.prevent="openEntry(entry)"
            @keydown.space.self.prevent="openEntry(entry)"
          >
            <FileIcon
              :path="entry.name"
              :is-dir="entry.type === 'dir'"
              :size="20"
              class="dir-row-icon"
            />
            <span class="dir-row-name" :title="entry.name">{{ entry.name }}</span>
            <span class="dir-row-size">{{ entry.type === 'dir' ? '' : formatFileSize(entry.size || 0) }}</span>
            <span class="dir-row-date">{{ formatModified(entry.modified) }}</span>
            <button
              v-if="entry.type !== 'dir'"
              class="share-btn dir-row-download"
              type="button"
              :title="t('share.dir.download')"
              :aria-label="t('share.dir.download')"
              @click.stop="downloadEntry(entry)"
            >
              <Download :size="16" />
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, Download, FileX2, FolderOpen } from 'lucide-vue-next'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import ShareThemeButton from './ShareThemeButton.vue'
import ShareFitButton from './ShareFitButton.vue'
import ShareView from './ShareView.vue'
import { setShareToken, shareApiUrl } from './shareMode'
import { shareDeepLink } from './shareLinks'
import { formatFileSize } from '@/utils/fileType'
import { joinPath } from '@/utils/path'
import { downloadUrlWithProgress } from '@/utils/download'

/** One row of a shared folder's listing (mirrors shareListEntry in Go). */
interface ShareListEntry {
  name: string
  type: 'dir' | 'file'
  size?: number
  modified?: string
}

/** One breadcrumb step, root first (mirrors shareBreadcrumb in Go). */
interface ShareBreadcrumb {
  name: string
  path: string
}

/** Payload of GET /api/share/{token}/list (mirrors shareListResponse in Go). */
interface ShareListing {
  kind: 'dir' | 'file'
  name: string
  path: string
  root: string
  parent: string | null
  atRoot: boolean
  crumbs?: ShareBreadcrumb[]
  entries?: ShareListEntry[]
}

const { t } = useI18n()

const loading = ref(true)
const error = ref('')
/** 'listing' shows the folder; 'file' hands off to ShareView. */
const mode = ref<'listing' | 'file'>('listing')
const listing = ref<ShareListing | null>(null)
/** Absolute path of the file shown in `file` mode. */
const filePath = ref('')
/** Folder the file view returns to ('' = the share root). */
const fileParent = ref('')
/** The path this document was opened at ('' = the share root). Kept so a
 *  failed deep link can still offer a way back to the root. */
const requestedPath = ref('')

const entries = computed<ShareListEntry[]>(() => listing.value?.entries ?? [])
const crumbs = computed<ShareBreadcrumb[]>(() => listing.value?.crumbs ?? [])
const title = computed(() => listing.value?.name || t('share.dir.title'))
const canGoUp = computed(() => !!listing.value && !listing.value.atRoot)
/** A failed DEEP LINK still gets a way back: the share root always exists as a
 *  URL, and the reader would otherwise be stranded on the error page. */
const showErrorBack = computed(() => !!error.value && !!requestedPath.value)

/** Extract the capability token from /share/{token}. */
function parseTokenFromPath(): string {
  const m = location.pathname.match(/^\/share\/([^/]+)\/?$/)
  return m ? decodeURIComponent(m[1]) : ''
}

/** Deep-link target from the URL (?path=), or '' for the share root. */
function pathFromUrl(): string {
  try {
    return new URLSearchParams(location.search).get('path') || ''
  } catch {
    return ''
  }
}

/**
 * Resolve a path inside the share. A folder renders the listing; a file (the
 * target of a `?path=` deep link, or a link followed inside a shared document)
 * hands off to ShareView — the backend reports which one it is, so this view
 * never has to stat anything itself.
 *
 * Requests are raced on purpose: clicking into a folder and then pressing Back
 * (or opening another folder) leaves two fetches in flight, and the network may
 * answer them in either order. Only the newest one may write, or the listing
 * shows folder A while the URL says B — which reads as "I clicked the wrong
 * thing" and survives until a reload.
 */
let resolveGen = 0

async function resolve(target: string) {
  const gen = ++resolveGen
  loading.value = true
  error.value = ''
  requestedPath.value = target
  try {
    const query = target ? `?path=${encodeURIComponent(target)}` : ''
    const resp = await fetch(shareApiUrl('list') + query)
    if (gen !== resolveGen) return
    if (!resp.ok) {
      error.value = t('share.dir.notFound')
      listing.value = null
      mode.value = 'listing'
      return
    }
    const data = (await resp.json()) as ShareListing
    // Parsing is async too, so the generation has to be re-checked here.
    if (gen !== resolveGen) return
    if (data.kind === 'file') {
      mode.value = 'file'
      filePath.value = data.path
      fileParent.value = data.parent || ''
      return
    }
    mode.value = 'listing'
    listing.value = data
  } catch {
    if (gen !== resolveGen) return
    error.value = t('share.dir.notFound')
    listing.value = null
    mode.value = 'listing'
  } finally {
    // A superseded request must not clear the spinner the newer one raised.
    if (gen === resolveGen) loading.value = false
  }
}

/** Switch to a folder without a page load (folders share this document). */
function goToDir(absPath: string) {
  try {
    history.pushState({ sharePath: absPath }, '', shareDeepLink(absPath))
  } catch {
    // History unavailable (rare); the in-place switch still works.
  }
  void resolve(absPath)
}

function goUp() {
  if (!listing.value) return
  goToDir(listing.value.parent || '')
}

/**
 * Open a file. A full navigation, not an in-place swap: the file view is a
 * separate document state that owns its own history for followed links, and
 * reloading keeps the two views from fighting over popstate. Assets are cached,
 * so the cost is a single document parse.
 */
function openEntry(entry: ShareListEntry) {
  const base = listing.value?.path
  if (!base) return
  const absPath = joinPath(base, entry.name)
  if (entry.type === 'dir') {
    goToDir(absPath)
    return
  }
  location.assign(shareDeepLink(absPath))
}

/** Back from the file view: return to the folder that contains it. */
function backToFolder() {
  location.assign(shareDeepLink(fileParent.value))
}

/** Back to the share root from a failed deep link. */
function backToRoot() {
  location.assign(shareDeepLink(''))
}

function downloadEntry(entry: ShareListEntry) {
  const base = listing.value?.path
  if (!base) return
  const absPath = joinPath(base, entry.name)
  downloadUrlWithProgress(
    `${shareApiUrl('download')}?path=${encodeURIComponent(absPath)}`,
    entry.name,
  )
}

function formatModified(value?: string): string {
  if (!value) return ''
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

/** Browser Back/Forward between folders. Ignored in file mode: ShareView owns
 *  history there, and that document is reloaded for each file anyway. */
function onPopState() {
  if (mode.value !== 'listing') return
  void resolve(pathFromUrl())
}

onMounted(() => {
  const token = parseTokenFromPath()
  if (!token) {
    error.value = t('share.invalidUrl')
    loading.value = false
    return
  }
  // Child views build token-scoped URLs, so the token must be installed first.
  setShareToken(token)
  window.addEventListener('popstate', onPopState)
  void resolve(pathFromUrl())
})

onBeforeUnmount(() => {
  window.removeEventListener('popstate', onPopState)
})
</script>

<style scoped>
/* Structural chrome (.share-view/.share-topbar/.share-body/.share-content/
   .share-btn) comes from css/share-chrome.css, the same source the file view
   and the markdown HTML export use. Only the listing itself lives here. */

.share-status { font-size: var(--font-size-sm); color: var(--text-muted, #656d76); }
.share-error { color: #cf222e; }

.dir-share-content {
  padding: var(--space-4) var(--space-6) var(--space-8);
}

/* ── Breadcrumb ── */
.dir-crumbs {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: calc(2 * var(--dp));
  margin-bottom: var(--space-3);
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #57606a);
}

.dir-crumb {
  border: none;
  background: transparent;
  padding: calc(2 * var(--dp)) var(--space-1);
  border-radius: var(--radius-sm);
  color: inherit;
  font: inherit;
  cursor: pointer;
  max-width: calc(220 * var(--dp));
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dir-crumb:hover {
  background: var(--bg-tertiary, #eaeef2);
  color: var(--text-primary, #1f2328);
}

.dir-crumb--current {
  color: var(--text-primary, #1f2328);
  font-weight: var(--font-weight-medium, 500);
  cursor: default;
}

.dir-crumb-sep { opacity: 0.5; }

/* ── Listing ── */
.dir-list {
  display: flex;
  flex-direction: column;
  gap: calc(2 * var(--dp));
}

.dir-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-height: calc(44 * var(--dp));
  padding: calc(8 * var(--dp)) var(--space-3);
  border-radius: var(--radius-md);
  cursor: pointer;
  outline: none;
}

.dir-row:hover,
.dir-row:focus-visible {
  background: var(--bg-secondary, #f6f8fa);
}

.dir-row:focus-visible {
  box-shadow: inset 0 0 0 calc(2 * var(--dp)) var(--accent-color, #0969da);
}

.dir-row-icon { flex-shrink: 0; }

.dir-row-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-md, 14px);
  color: var(--text-primary, #1f2328);
}

.dir-row--folder .dir-row-name { font-weight: var(--font-weight-medium, 500); }

.dir-row-size,
.dir-row-date {
  flex-shrink: 0;
  font-size: var(--font-size-sm, 12px);
  color: var(--text-muted, #656d76);
  font-variant-numeric: tabular-nums;
}

.dir-row-size {
  min-width: calc(64 * var(--dp));
  text-align: right;
}

.dir-row-date {
  min-width: calc(132 * var(--dp));
  text-align: right;
}

/* The download action stays out of the way until the row is engaged — but a
   touch device has no hover, so it is always visible there. */
.dir-row-download { opacity: 0; }

.dir-row:hover .dir-row-download,
.dir-row:focus-within .dir-row-download {
  opacity: 1;
}

@media (hover: none) {
  .dir-row-download { opacity: 1; }
}

@media (max-width: 640px) {
  .dir-row-date { display: none; }
}

/* ── Empty / error ── */
.dir-empty,
.share-error-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  padding: calc(48 * var(--dp)) var(--space-4);
  color: var(--text-muted, #656d76);
  text-align: center;
}

.dir-empty-text { font-size: var(--font-size-md, 14px); }

.share-error-title {
  font-size: var(--font-size-lg, 16px);
  font-weight: 600;
  color: var(--text-primary, #1f2328);
}

.share-error-desc { font-size: var(--font-size-sm, 12px); }

/* Text button in the error state: .share-btn is a square icon chip, so the
   width/height and border are re-stated for a label. */
.dir-error-back {
  width: auto;
  height: calc(32 * var(--dp));
  padding: 0 var(--space-4);
  gap: var(--space-1);
  border: 1px solid var(--border-color, rgba(128, 128, 128, 0.25));
  border-radius: var(--radius-sm);
  font-size: var(--font-size-sm, 12px);
  color: var(--text-primary, #1f2328);
}

.dir-error-back:hover {
  background: var(--bg-tertiary, #eaeef2);
}

.dir-skeleton { padding-top: var(--space-2); }
</style>
