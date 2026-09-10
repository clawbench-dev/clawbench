<template>
  <div class="share-view">
    <!-- Read-only top bar (not the app FileHeader) -->
    <div class="share-topbar">
      <span class="share-file-name" :title="file?.name || ''">{{ file?.name || '' }}</span>
      <span v-if="loading" class="share-status">{{ t('share.loading') }}</span>
      <span v-else-if="error" class="share-status share-error">{{ error }}</span>
      <span v-else class="share-spacer" />
      <div v-if="(hasToc || showToggleView) && !error" class="share-top-actions">
        <!-- Toggle between rendered preview and source (markdown/html/openapi) -->
        <button
          v-if="showToggleView"
          class="share-btn share-view-toggle"
          :class="{ active: viewMode === 'rendered' }"
          type="button"
          :title="viewMode === 'rendered' ? t('share.sourceView') : t('share.renderedView')"
          :aria-pressed="viewMode === 'rendered'"
          @click="toggleViewMode"
        >
          <Eye :size="16" />
        </button>
        <button v-if="hasToc" class="share-btn" type="button" :title="t('share.toggleToc')" @click="tocOpen = !tocOpen">
          <List :size="16" />
        </button>
      </div>
      <a
        v-if="file && !error"
        class="share-btn"
        :href="downloadUrl"
        :download="file.name"
        :title="t('share.download')"
      >
        <Download :size="16" />
      </a>
    </div>

    <!-- Body: content + optional TOC -->
    <div class="share-body">
      <div
        class="share-content"
        :data-markdown-rendered="isMarkdownRenderedView || undefined"
        ref="contentRef"
      >
        <!-- Loading -->
        <div v-if="loading" class="share-center-hint">
          <LoadingIndicator size="md" />
        </div>

        <!-- Error / invalid link -->
        <div v-else-if="error" class="share-error-state">
          <FileX2 :size="40" />
          <div class="share-error-title">{{ t('share.invalidTitle') }}</div>
          <div class="share-error-desc">{{ error }}</div>
        </div>

        <template v-else-if="file">
          <!-- Markdown rendered preview (default) -->
          <MarkdownPreview
            v-if="isMarkdown && viewMode === 'rendered'"
            :file="file"
            view-mode="rendered"
            :word-wrap="wordWrap"
            @close-search="() => {}"
          />

          <!-- PDF -->
          <PdfPreview
            v-else-if="file.isPdf"
            :file="file"
          />

          <!-- Image / SVG — block figure with a header (lightbox view button)
               matching the in-app markdown image blocks. Share viewers are
               read-only, so the header carries no attach/open actions. -->
          <div v-else-if="file.isImage" class="share-image-view">
            <div class="image-block-wrapper">
              <div class="image-block-header">
                <span class="image-block-header-actions">
                  <button
                    class="image-block-view-btn"
                    type="button"
                    :title="t('imageBlock.view')"
                    :aria-label="t('imageBlock.view')"
                  >
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3"/><path d="M21 8V5a2 2 0 0 0-2-2h-3"/><path d="M3 16v3a2 2 0 0 0 2 2h3"/><path d="M16 21h3a2 2 0 0 0 2-2v-3"/></svg>
                  </button>
                </span>
              </div>
              <span class="lightbox-img-wrap">
                <img
                  class="share-image-img lightbox-img"
                  :src="shareImageUrl"
                  :alt="file.name"
                  draggable="false"
                />
              </span>
            </div>
          </div>

          <!-- Audio -->
          <AudioPreview
            v-else-if="file.isAudio"
            :file="file"
          />

          <!-- Video -->
          <VideoPreview
            v-else-if="file.isVideo"
            :file="file"
          />

          <!-- Office documents -->
          <OfficePreview
            v-else-if="file.isOffice"
            :file="file"
          />

          <!-- OpenAPI / Swagger spec (rendered docs) -->
          <div v-else-if="isOpenapi && viewMode === 'rendered'" class="share-fill-viewer">
            <OpenApiPreview :file="file" />
          </div>

          <!-- HTML rendered -->
          <iframe
            v-else-if="isHtml && viewMode === 'rendered'"
            class="share-html-iframe"
            :srcdoc="file.content"
            sandbox="allow-scripts"
          />

          <!-- Raw source view for markdown/html/openapi after the view toggle,
               and code/plain text files which have no rendered preview.
               stickyScroll is disabled: the share SPA has no auth for the
               backend symbol API the sticky overlay would query. -->
          <CodeMirrorViewer
            v-else-if="showRawSourceView"
            :file="file"
            :content="file.content"
            :language="rawLanguage"
            :editable="false"
            :word-wrap="wordWrap"
            :sticky-scroll="false"
          />

          <!-- Binary / too-large / unsupported fallback: download -->
          <div v-else class="share-center-hint share-unsupported">
            <FileIcon :path="file.name" :size="48" />
            <div class="share-error-desc">{{ t('share.noPreview') }}</div>
            <a class="share-download-btn" :href="downloadUrl" :download="file.name">
              <Download :size="14" />
              {{ t('common.download') }}
            </a>
          </div>
        </template>
      </div>

      <!-- TOC rail (wide screens). Narrow screens render a slide-in drawer. -->
      <div v-if="hasToc && tocOpen && !isNarrow" class="share-toc">
        <div class="share-toc-title">{{ t('share.toc') }}</div>
        <button
          v-for="item in tocItems"
          :key="item.id"
          class="share-toc-item"
          :data-level="item.level"
          :style="{ paddingLeft: (8 + (item.level - 1) * 14) + 'px' }"
          :title="item.text"
          @click="scrollToTocItem(item)"
        >{{ item.text }}</button>
      </div>
    </div>

    <!-- Narrow-screen TOC drawer: backdrop + slide-in panel -->
    <Teleport to="body">
      <div v-if="isNarrow && hasToc && tocOpen" class="share-toc-drawer">
        <div class="share-toc-backdrop" @click="tocOpen = false" />
        <aside class="share-toc share-toc-panel">
          <div class="share-toc-title">{{ t('share.toc') }}</div>
          <button
            v-for="item in tocItems"
            :key="item.id"
            class="share-toc-item"
            :data-level="item.level"
            :style="{ paddingLeft: (8 + (item.level - 1) * 14) + 'px' }"
            :title="item.text"
            @click="scrollToTocItem(item); tocOpen = false"
          >{{ item.text }}</button>
        </aside>
      </div>
    </Teleport>

    <!-- Full-image viewer: view buttons (image blocks + rendered markdown) open
         here. Lightbox provides its own openLightbox/openMdImages via provide;
         its document-level click listener drives .image-block-view-btn. -->
    <Lightbox />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, defineAsyncComponent, provide, readonly } from 'vue'
import { useI18n } from 'vue-i18n'
import { Download, Eye, FileX2, List } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import Lightbox from '@/components/media/Lightbox.vue'
import PdfPreview from '@/components/media/PdfPreview.vue'
import AudioPreview from '@/components/media/AudioPreview.vue'
import VideoPreview from '@/components/media/VideoPreview.vue'
import { buildAsyncComponentOptions } from '@/composables/useAsyncComponent.ts'
import MarkdownPreview from '@/components/file/MarkdownPreview.vue'
const CodeMirrorViewer = defineAsyncComponent(buildAsyncComponentOptions({ loader: () => import('@/components/file/CodeMirrorViewer.vue') }))
const OfficePreview = defineAsyncComponent(buildAsyncComponentOptions({ loader: () => import('@/components/media/OfficePreview.vue') }))
const OpenApiPreview = defineAsyncComponent(buildAsyncComponentOptions({ loader: () => import('@/components/file/OpenApiPreview.vue') }))
import { getFileType } from '@/utils/fileType.ts'
import { flashElement } from '@/utils/domFlash'
import { extractToc, type TocItem } from '@/utils/toc.ts'
import { setShareToken, setSharedFile, shareApiUrl } from '@/share/shareMode'
import { buildLocalFileUrl } from '@/utils/download'
import { store } from '@/stores/app.ts'

// Share the resolved theme id with child components (OpenApiPreview reads it via
// inject('theme') to pick Swagger UI colors). share.html sets data-theme on <html>.
const themeId = ref(document.documentElement.getAttribute('data-theme') || 'github-dark')
provide('theme', readonly(themeId))

/** The file payload returned by the share /file endpoint (FileContent JSON)
 *  extended with the viewer flags set by decorateFile. */
interface ShareFile {
  name: string
  path: string
  content: string
  isBinary?: boolean
  isPdf?: boolean
  isImage?: boolean
  isAudio?: boolean
  isVideo?: boolean
  isOffice?: boolean
  isHtml?: boolean
  isExcalidraw?: boolean
  tooLarge?: boolean
  subtype?: string
}

const { t } = useI18n()

// ─── State ───
const loading = ref(true)
const error = ref('')
const file = ref<ShareFile | null>(null)
const tocOpen = ref(true)
/** Narrow layout (<900px): TOC moves to a slide-in drawer over the content. */
const isNarrow = ref(false)
let tocMq: MediaQueryList | null = null
function syncNarrow() {
  if (typeof window.matchMedia !== 'function') return // jsdom / non-browser
  isNarrow.value = window.matchMedia('(max-width: 899px)').matches
}
const wordWrap = ref(false)
const contentRef = ref<HTMLElement | null>(null)
const tocItems = ref<TocItem[]>([])
/** 'rendered' (preview) | 'raw' (source code). Only used by file types that
 *  have both a rendered preview and a viewable source (markdown/html/openapi). */
const viewMode = ref<'rendered' | 'raw'>('rendered')

// ─── Parse token from /share/{token} ───
function parseTokenFromPath(): string {
  const m = location.pathname.match(/^\/share\/([^/]+)\/?$/)
  return m ? decodeURIComponent(m[1]) : ''
}

const downloadUrl = computed(() => {
  if (!file.value) return ''
  return shareApiUrl('download')
})

/** Full-size token-scoped URL for the single-file image / SVG preview. */
const shareImageUrl = computed(() => {
  if (!file.value?.path) return ''
  return buildLocalFileUrl(file.value.path)
})

const rawLanguage = computed(() => {
  if (!file.value?.name) return 'plaintext'
  return getFileType(file.value.name)?.lang || 'plaintext'
})

const isMarkdown = computed(() => {
  if (!file.value) return false
  return !!getFileType(file.value.name)?.isMarkdown
})

const isHtml = computed(() => {
  if (!file.value) return false
  return !!getFileType(file.value.name)?.isHtml
})

const isOpenapi = computed(() => file.value?.subtype === 'openapi' || false)

const isTextContent = computed(() => {
  if (!file.value) return false
  if (file.value.isBinary || file.value.tooLarge) return false
  return typeof file.value.content === 'string' && file.value.content.length > 0
})

/** Whether the file has a rendered preview AND a readable source (markdown
 *  rendered preview, HTML rendered iframe, OpenAPI/Swagger docs) — i.e. the
 *  source/rendered toggle button is shown. */
const showToggleView = computed(() => isTextContent.value && (isMarkdown.value || isHtml.value || isOpenapi.value))

/** Whether the current file body is rendered through CodeMirrorViewer (the raw
 *  source view). True for code/plain text files and for markdown/html/openapi
 *  after the user toggles to source. Drives the toggle button + TOC line jumps. */
const showRawSourceView = computed(() => {
  if (!isTextContent.value) return false
  if (isMarkdown.value || isHtml.value || isOpenapi.value) return viewMode.value === 'raw'
  return true
})

/** Rendered markdown preview is active. Its reading column is capped at 900px
 *  by the shared .markdown-preview .markdown-body rule (css/content.css), the
 *  same as the in-app file viewer, so the wide-screen .share-content cap must
 *  be lifted here to let the scrollbar hug the viewport edge. */
const isMarkdownRenderedView = computed(() => isMarkdown.value && viewMode.value === 'rendered')

const hasToc = computed(() => {
  if (!file.value || error.value) return false
  if (file.value.isBinary || file.value.tooLarge) return false
  // Mirror the file browser: OpenAPI renders through its own Swagger UI
  // sidebar (fileSupportsToc returns false for openapi in rendered view),
  // so no heading outline is extracted or shown.
  if (file.value.subtype === 'openapi') return false
  return isMarkdown.value || isTextContent.value
})

// ─── File shape (mirrors store.selectFile extension detection) ───
function decorateFile(data: ShareFile): ShareFile {
  const lower = (data.name || '').toLowerCase()
  const imageExts = ['.png', '.jpg', '.jpeg', '.gif', '.webp', '.svg', '.bmp', '.ico', '.tiff', '.tif', '.avif']
  const audioExts = ['.mp3', '.wav', '.ogg', '.m4a', '.aac', '.flac', '.wma', '.opus']
  const videoExts = ['.mp4', '.mkv', '.avi', '.mov', '.webm', '.flv', '.wmv', '.m4v', '.3gp', '.m3u8']
  const officeExts = ['.docx', '.xlsx', '.pptx', '.xls']
  if (lower.endsWith('.pdf')) data.isPdf = true
  if (imageExts.some(e => lower.endsWith(e))) data.isImage = true
  if (audioExts.some(e => lower.endsWith(e))) data.isAudio = true
  if (videoExts.some(e => lower.endsWith(e))) data.isVideo = true
  if (officeExts.some(e => lower.endsWith(e))) data.isOffice = true
  const htmlExts = ['.html', '.htm', '.xhtml']
  if (htmlExts.some(e => lower.endsWith(e))) data.isHtml = true
  if (data.subtype === 'excalidraw') data.isExcalidraw = true
  return data
}

async function loadFile() {
  loading.value = true
  error.value = ''
  // A fresh file always opens in its rendered/preview view. (Mirrors the App's
  // file-view reset on file change — guards against reusing this instance for
  // another file while still toggled to source.)
  viewMode.value = 'rendered'
  try {
    const resp = await fetch(shareApiUrl('file'))
    if (!resp.ok) {
      error.value = t('share.notFound')
      return
    }
    const data = await resp.json()
    decorateFile(data)
    file.value = data
    setSharedFile(data.path, data.name)

    // Build TOC for markdown / text content.
    if (isTextContent.value) {
      const lang = isMarkdown.value ? 'markdown' : (rawLanguage.value || 'plaintext')
      if (typeof data.content === 'string' && data.content) {
        tocItems.value = extractToc(data.content, lang)
      }
    }
    // Keep store project/home roots empty so markdown file-path annotations
    // inside the shared doc cannot resolve to clickable in-app file opens.
    store.state.projectRoot = store.state.projectRoot || ''
    store.state.homeDir = store.state.homeDir || ''
    // Desktop opens with the TOC rail visible; narrow screens default closed
    // (opened on demand via the top-bar button → slide-in drawer).
    tocOpen.value = !isNarrow.value
  } finally {
    loading.value = false
  }
}

function scrollToHeading(id: string) {
  const root = contentRef.value
  if (!root) return
  const el = root.querySelector(`#${CSS.escape(id)}`)
  if (!el) return
  // Scroll ONLY the content column (never the outer page / header). A bare
  // scrollIntoView({smooth}) would scroll every scrollable ancestor and, during
  // the multi-frame animation, drift off target when async content (images,
  // mermaid, tables) reflows — pushing the topbar out of view. Compute the
  // heading's absolute position inside .share-content and scroll just that
  // container, matching the two-phase approach used for the in-app viewer.
  const targetTop = el.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop
  root.scrollTo({ top: targetTop, behavior: 'smooth' })
  // Flash the jumped heading (reuses the canonical .line-flash animation,
  // same as the in-app markdown preview anchor jump).
  flashElement(el)
}

// ─── View toggle ───

function toggleViewMode() {
  viewMode.value = viewMode.value === 'rendered' ? 'raw' : 'rendered'
}

// ─── TOC jump routing ───
// CodeMirror-rendered bodies (pure code files, or markdown/html/openapi toggled
// to source) have no heading DOM, so TOC entries jump by line through the
// `cm-scroll-to-line` window event CodeMirrorViewer listens for. Rendered
// markdown previews keep the old heading-anchor jump.

// The CodeMirror viewer is an async chunk (defineAsyncComponent). A user can
// click a TOC entry right after toggling to source, while the editor is still
// loading and its window listener is not yet registered. The first dispatch is
// synchronous (lowest latency when the editor is already mounted); if the
// editor has not acknowledged it via `cm-scroll-to-line-handled`, retry every
// animation frame until it does (or a frame budget runs out), so an
// immediately-after-toggle click is never lost.
const LINE_SCROLL_MAX_ATTEMPTS = 60
let lineScrollRequestId = 0
let activeLineScrollCancel: (() => void) | null = null

function scrollToCodeLine(line: number) {
  const f = file.value
  if (!f?.path) return
  // Capture for use inside the rAF-retry closure (narrowing is lost there).
  const filePath = f.path
  const requestId = ++lineScrollRequestId
  let attempts = 0
  let handled = false

  activeLineScrollCancel?.()

  function onHandled(e: Event) {
    if ((e as CustomEvent).detail?.requestId !== requestId) return
    handled = true
    cleanup()
  }

  function cleanup() {
    window.removeEventListener('cm-scroll-to-line-handled', onHandled)
    if (activeLineScrollCancel === cleanup) activeLineScrollCancel = null
  }

  activeLineScrollCancel = cleanup
  window.addEventListener('cm-scroll-to-line-handled', onHandled)

  function tryScroll() {
    attempts += 1
    window.dispatchEvent(new CustomEvent('cm-scroll-to-line', {
      detail: { line, path: filePath, requestId },
    }))
    if (!handled && attempts < LINE_SCROLL_MAX_ATTEMPTS) {
      requestAnimationFrame(tryScroll)
    } else {
      cleanup()
    }
  }
  tryScroll()
}

function scrollToTocItem(item: TocItem) {
  if (showRawSourceView.value) {
    if (item.line) scrollToCodeLine(item.line)
    return
  }
  scrollToHeading(item.id)
}

onMounted(() => {
  syncNarrow()
  if (typeof window.matchMedia === 'function') {
    tocMq = window.matchMedia('(max-width: 899px)')
    tocMq.addEventListener('change', syncNarrow)
  }

  const token = parseTokenFromPath()
  if (!token) {
    error.value = t('share.invalidUrl')
    loading.value = false
    return
  }
  setShareToken(token)
  void loadFile()
})

onBeforeUnmount(() => {
  tocMq?.removeEventListener('change', syncNarrow)
  tocMq = null
})
</script>

<style scoped>
/* Structural chrome (.share-view/.share-topbar/.share-body/.share-content/
   .share-toc/.share-btn/…) lives in css/share-chrome.css — the SAME source the
   markdown HTML export embeds. Only page-specific rules stay below. */

.share-status { font-size: 12px; color: var(--text-muted, #656d76); }
.share-error { color: #cf222e; }

/* Active (rendered preview shown) state for the view toggle */
.share-btn.active {
  background: var(--bg-tertiary, #eaeef2);
  color: var(--accent-color, #0969da);
}

.share-center-hint {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  height: 100%;
  padding: 32px;
  text-align: center;
}

.share-error-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: 100%;
  padding: 32px;
  color: var(--text-muted, #656d76);
  text-align: center;
}

.share-error-title { font-size: 16px; font-weight: 600; color: var(--text-primary, #1f2328); }
.share-error-desc { font-size: 13px; max-width: 480px; word-break: break-word; }

.share-html-iframe {
  width: 100%;
  height: 100%;
  border: none;
}

/* OpenAPI preview fills the visible content area. .share-content is an
   overflow:auto scroller whose height is determined by its children, so a
   flex:1 child would collapse to content height. Pin the viewer to the
   scroller's visible viewport instead. */
.share-fill-viewer {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  display: flex;
  flex-direction: column;
}

.share-download-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border-radius: 8px;
  background: var(--accent-color, #0969da);
  color: #fff;
  text-decoration: none;
  font-size: 14px;
  cursor: pointer;
}

/* Single-file image / SVG: centered full-bleed host around the shared
   .image-block figure (header + image). */
.share-image-view {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  padding: 24px;
  box-sizing: border-box;
  background: var(--bg-primary, #fff);
  overflow: auto;
}

/* The global .image-block-wrapper is width:fit-content for markdown text flow;
   inside the full-screen share host it must fit the available space and center
   the image (the action toolbar now floats over the image top-right corner). */
.share-image-view .image-block-wrapper {
  margin: 0;
  max-width: 100%;
  max-height: 100%;
}

.share-image-view .share-image-img {
  display: block;
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
}

/* Narrow-screen TOC drawer — Teleported to <body>, so its styles must be
   global (scoped selectors would not reach the teleported nodes). The panel
   reuses .share-toc from share-chrome.css for sizing/item styles. */
:global(.share-toc-backdrop) {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.4);
  z-index: 300;
  animation: share-toc-fade 0.18s ease-out;
}

:global(.share-toc-panel) {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  height: 100%;
  z-index: 301;
  box-sizing: border-box;
  border-left: 1px solid var(--border-color, rgba(128, 128, 128, .25));
  box-shadow: -8px 0 24px rgba(0, 0, 0, 0.12);
  animation: share-toc-slide 0.2s ease-out;
}

@keyframes share-toc-fade {
  from { opacity: 0; }
  to { opacity: 1; }
}

@keyframes share-toc-slide {
  from { transform: translateX(100%); }
  to { transform: translateX(0); }
}

/* On very wide screens the content column would stretch content (PDFs, code)
   unreasonably wide. Cap it and center it, leaving generous side margins. */
@media (min-width: 1100px) {
  .share-content {
    max-width: 1080px;
    margin: 0 auto;
  }
}

/* Rendered markdown preview aligns with the in-app file viewer: the reading
   column is capped at 900px by the shared .markdown-body padding rule, so the
   .share-content wide-screen cap must NOT shrink the scroll container here —
   otherwise the scrollbar would float mid-window instead of hugging the edge. */
@media (min-width: 1100px) {
  .share-content[data-markdown-rendered] {
    max-width: none;
    margin: 0;
  }
}
</style>
