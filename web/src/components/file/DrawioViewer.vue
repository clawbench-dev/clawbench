<template>
  <div class="drawio-viewer">
    <!-- Editor not installed yet: offer the download. The editor is ~118MB, so
         it is fetched once on the server and shared by every device. -->
    <div v-if="phase !== 'ready'" class="drawio-gate">
      <div class="drawio-gate-card">
        <div v-if="phase === 'checking'" class="drawio-gate-body">
          <LoadingIndicator size="sm" />
        </div>

        <template v-else>
          <p class="drawio-gate-title">{{ t('file.viewer.drawioTitle') }}</p>
          <p class="drawio-gate-desc">{{ t('file.viewer.drawioDesc') }}</p>

          <div v-if="phase === 'installing' || uploading" class="drawio-progress">
            <div class="drawio-progress-track">
              <div class="drawio-progress-fill" :style="{ width: busyPercent + '%' }" />
            </div>
            <span class="drawio-progress-text">{{ busyText }}</span>
          </div>

          <p v-if="phase === 'failed' && errorText" class="drawio-error">{{ errorText }}</p>

          <button
            v-if="phase !== 'installing' && !uploading && !readonly"
            class="drawio-action"
            @click="startInstall"
          >
            {{ phase === 'failed' ? t('file.viewer.drawioRetry') : t('file.viewer.drawioInstall') }}
          </button>
          <p v-else-if="phase !== 'installing' && !uploading && readonly" class="drawio-gate-desc">
            {{ t('file.viewer.drawioReadonlyMissing') }}
          </p>

          <!-- Offline path: the server may have no route to github.com at all,
               so the archive can also be handed over from this machine. -->
          <div v-if="!readonly" class="drawio-upload">
            <span class="drawio-upload-hint">{{ t('file.viewer.drawioUploadHint') }}</span>
            <button
              class="drawio-upload-btn"
              :disabled="uploading || phase === 'installing'"
              @click="pickFile"
            >
              {{ t('file.viewer.drawioPickFile') }}
            </button>
            <input
              ref="fileInputRef"
              class="drawio-file-input"
              type="file"
              accept=".war,.zip"
              @change="onFileChosen"
            />
          </div>
        </template>
      </div>
    </div>

    <iframe
      v-else
      ref="frameRef"
      class="drawio-frame"
      :src="frameSrc"
      sandbox="allow-scripts allow-same-origin allow-modals allow-downloads allow-popups"
    />
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiGet, apiPost } from '@/utils/api'
import { useDialog } from '@/composables/useDialog.ts'
import { useFileEditor } from '@/composables/useFileEditor.ts'
import { useCodeEditorSave } from '@/composables/useCodeEditorSave.ts'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'

/**
 * draw.io editor for .drawio files, rendered inside a sandboxed iframe.
 *
 * Unlike Excalidraw (whose bundle ships with the app), the draw.io webapp is
 * ~118MB and lives on the SERVER: it is downloaded on first use into the data
 * directory and served from /vendor/drawio/<version>/. This component therefore
 * has two jobs:
 *
 *   1. ask the server whether the editor is installed, and if not, offer to
 *      download it while showing progress (the download runs server-side, so
 *      every device shares one copy);
 *   2. once installed, host the iframe and bridge the file content in and out.
 *
 * The bridge uses draw.io's own embed protocol (embed=1&proto=json):
 *   iframe → us: {event:'init'} | {event:'autosave', xml} | {event:'export', data}
 *   us → iframe: {action:'load', xml, autosave:1} | {action:'export', format:'xml'}
 *
 * draw.io has no host-side "give me the current XML" call, so the editor
 * autosaves its XML to us continuously; an explicit export is requested on
 * save and the last autosave is the fallback if that does not come back.
 */

const props = defineProps({
  file: Object,
  /** Preview-only (the public share page): no editing state, no save flow. */
  readonly: { type: Boolean, default: false },
})

const emit = defineEmits(['requestClose'])

const { t, locale } = useI18n()
const dialog = useDialog()
const fileEditor = useFileEditor()
const { saveFile } = useCodeEditorSave()

const frameRef = ref(null)
const fileInputRef = ref(null)
// 'checking' | 'absent' | 'installing' | 'ready' | 'failed'
const phase = ref('checking')
const basePath = ref('')
const downloaded = ref(0)
const total = ref(0)
const errorText = ref('')
const uploading = ref(false)
const uploadPercent = ref(0)
// Light/dark of the app theme, mirrored from <html data-theme-base>. Observing
// the attribute covers the theme picker, the settings page and a system-scheme
// flip under 'auto' without re-deriving any of those rules here.
const isDark = ref(false)
let themeObserver = null

function readThemeBase() {
  isDark.value = document.documentElement.getAttribute('data-theme-base') === 'dark'
}

let pollTimer = null
let iframeReady = false
let dirty = false
let latestXml = ''
let pendingExport = null
// A user-requested export (see requestExport), distinct from the save flow's
// XML round trip above.
let pendingUserExport = null

const frameSrc = computed(() => {
  const lang = typeof locale.value === 'string' && locale.value.startsWith('zh') ? 'zh' : 'en'
  // The directory URL, not ".../index.html": http.FileServer redirects a path
  // ending in index.html back to the directory, which would cost an extra round
  // trip (and drop the embed parameters on some clients).
  //
  // Read-only preview hides the chrome and enables pan/zoom only, matching the
  // share page's treatment of Excalidraw — there is no save path there.
  const view = props.readonly ? '&nav=1&chrome=0' : ''
  // drawio picks its palette from `dark`; without it a dark app shows a white
  // editor. Only the URL carries it — there is no runtime switch — so a theme
  // change reloads the frame (see the watcher below).
  const dark = isDark.value ? '&dark=1' : ''
  return `${basePath.value}?embed=1&proto=json&spin=1&modified=1&noSaveBtn=1&noExitBtn=1&lang=${lang}${view}${dark}`
})

const percent = computed(() => {
  if (!total.value) return 0
  return Math.min(100, Math.round((downloaded.value / total.value) * 100))
})

const progressText = computed(() => {
  if (!total.value) return t('file.viewer.drawioDownloading')
  const mb = (n) => (n / 1048576).toFixed(1)
  return t('file.viewer.drawioProgress', { done: mb(downloaded.value), total: mb(total.value) })
})

// Uploading and downloading share one bar: only one of them can be running, and
// the user does not care which side the bytes are counted on.
const busyPercent = computed(() => (uploading.value ? uploadPercent.value : percent.value))
const busyText = computed(() =>
  uploading.value
    ? t('file.viewer.drawioUploading', { percent: uploadPercent.value })
    : progressText.value
)

function pickFile() {
  fileInputRef.value?.click()
}

async function onFileChosen(e) {
  const input = e.target
  const picked = input?.files?.[0]
  // Reset first so picking the same file twice still fires `change`.
  if (input) input.value = ''
  if (!picked) return

  uploading.value = true
  uploadPercent.value = 0
  errorText.value = ''
  try {
    await uploadArchive(picked)
    await refreshStatus()
  } catch (err) {
    // The server's message names the actual reason (checksum mismatch, not an
    // archive), which is far more useful than "upload failed".
    phase.value = 'failed'
    errorText.value = (err && err.message) || t('file.viewer.drawioUploadFailed')
  } finally {
    uploading.value = false
  }
}

/**
 * POST the archive as multipart and report byte progress.
 *
 * Raw XHR rather than apiPost: fetch has no upload-progress event, and 48MB over
 * a slow link needs a bar. The timeout is generous because this also covers the
 * server's verification and unpacking after the last byte is sent.
 */
function uploadArchive(file) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', '/api/drawio/upload')
    xhr.timeout = 300000
    xhr.upload.onprogress = (ev) => {
      if (ev.lengthComputable) uploadPercent.value = Math.round((ev.loaded / ev.total) * 100)
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve()
        return
      }
      let message = ''
      try {
        message = JSON.parse(xhr.responseText)?.error || ''
      } catch {
        // Non-JSON body (proxy error page): fall back to the status code below.
      }
      reject(new Error(message || `${t('file.viewer.drawioUploadFailed')} (HTTP ${xhr.status})`))
    }
    xhr.onerror = () => reject(new Error(t('file.viewer.drawioUploadFailed')))
    xhr.ontimeout = () => reject(new Error(t('file.viewer.drawioUploadFailed')))

    const form = new FormData()
    form.append('file', file)
    xhr.send(form)
  })
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function applyStatus(st) {
  if (!st) return
  if (st.installed) {
    basePath.value = st.basePath || ''
    phase.value = 'ready'
    stopPolling()
    return
  }
  if (st.state === 'downloading' || st.state === 'installing') {
    phase.value = 'installing'
    downloaded.value = st.downloaded || 0
    total.value = st.total || 0
    startPolling()
    return
  }
  stopPolling()
  if (st.state === 'failed') {
    phase.value = 'failed'
    errorText.value = st.error || ''
    return
  }
  phase.value = 'absent'
}

async function refreshStatus() {
  try {
    applyStatus(await apiGet('/api/drawio/status'))
  } catch {
    phase.value = 'failed'
    errorText.value = t('file.viewer.drawioStatusFailed')
  }
}

function startPolling() {
  if (pollTimer) return
  pollTimer = setInterval(async () => {
    try {
      applyStatus(await apiGet('/api/drawio/status'))
    } catch {
      // A single failed poll is not fatal: the install continues server-side,
      // and the next tick will pick the state back up.
    }
  }, 1000)
}

async function startInstall() {
  phase.value = 'installing'
  errorText.value = ''
  try {
    await apiPost('/api/drawio/install', {})
  } catch {
    // 409 means another install is already running, which is fine — polling
    // reports its progress. Any other failure shows up as state 'failed' in
    // the very next poll, so there is nothing to distinguish here.
  }
  await refreshStatus()
  startPolling()
}

// ---- iframe bridge -------------------------------------------------------

function postToFrame(msg) {
  frameRef.value?.contentWindow?.postMessage(JSON.stringify(msg), window.location.origin)
}

function onMessage(e) {
  if (e.source !== frameRef.value?.contentWindow) return
  let msg
  try {
    msg = typeof e.data === 'string' ? JSON.parse(e.data) : e.data
  } catch {
    return
  }
  if (!msg || typeof msg.event !== 'string') return

  switch (msg.event) {
    case 'init':
      iframeReady = true
      sendLoad()
      break
    case 'autosave':
      // draw.io pushes the current XML as the user edits. Keeping the latest
      // one means an exit right after an edit still saves the edit even if the
      // explicit export below does not answer in time. A read-only preview
      // must never look dirty — the share page has no save path.
      if (props.readonly) break
      if (typeof msg.xml === 'string' && msg.xml) {
        latestXml = msg.xml
        setDirty(true)
      }
      break
    case 'save':
      // drawio's own "Save & Exit" (the same message Ctrl+S sends). In embed
      // mode this is drawio's ONLY save action and it carries no exit flag, so
      // it always means "save and leave" — the host is the one that has to close
      // the view, otherwise the button saves and then appears to do nothing.
      if (typeof msg.xml === 'string' && msg.xml) {
        latestXml = msg.xml
        void persistXml(msg.xml).then((ok) => {
          if (ok) emit('requestClose')
        })
      }
      break
    case 'export':
      if (pendingUserExport) {
        const { resolve, timer } = pendingUserExport
        clearTimeout(timer)
        pendingUserExport = null
        resolve({ format: msg.format || '', data: msg.data || '' })
        break
      }
      if (pendingExport) {
        pendingExport(msg.data || latestXml)
        pendingExport = null
      }
      break
  }
}

function sendLoad() {
  if (!iframeReady) return
  // `latestXml` is the live copy drawio has been pushing us since the last
  // reload; falling back to the file's content keeps a reload (language/theme
  // switch) from discarding edits made before the first autosave landed.
  const xml = latestXml || props.file?.content || ''
  postToFrame({ action: 'load', xml, autosave: props.readonly ? 0 : 1 })
}

function setDirty(v) {
  dirty = v
  // "Editing" tracks unsaved work, not the fact that an editor is on screen.
  // The back/close state machine routes every press to "exit edit mode" while
  // this flag is set, so keeping it on permanently turned the close button into
  // a no-op: the press was consumed by an exit that had nothing to do, and the
  // viewer never closed. With it following `dirty`, a clean diagram closes in
  // one click and an edited one still asks before throwing the changes away.
  if (!props.readonly) fileEditor.setEditing(v)
}

/** Ask draw.io for the current XML; falls back to the last autosave. */
function requestXml() {
  return new Promise((resolve) => {
    if (!iframeReady) {
      resolve(latestXml)
      return
    }
    // Safety net: the export is a round trip through the iframe, which may be
    // gone or wedged. The timer is cleared as soon as the answer arrives so a
    // finished save does not leave one pending.
    const timer = setTimeout(() => {
      if (pendingExport) {
        pendingExport(latestXml)
        pendingExport = null
      }
    }, 3000)
    pendingExport = (xml) => {
      clearTimeout(timer)
      resolve(xml)
    }
    postToFrame({ action: 'export', format: 'xml' })
  })
}

async function persistXml(xml) {
  if (!xml) return false
  const ok = await saveFile(props.file?.path || '', xml)
  if (ok) setDirty(false)
  return ok
}

async function saveCurrent() {
  return persistXml(await requestXml())
}

// Same contract as ExcalidrawViewer: resolves true when the diagram is saved or
// discarded (navigation may proceed), false when the user cancelled or the
// write failed.
async function handleExit() {
  if (!dirty) return true
  const choice = await dialog.confirm(t('file.editor.confirmExit'), {
    confirmText: t('file.editor.save'),
    cancelText: t('common.cancel'),
    extraText: t('file.editor.dontSave'),
    extraPrimedText: t('common.confirm'),
  })
  if (choice === true) {
    return await saveCurrent()
  }
  if (choice === null) {
    setDirty(false)
    return true
  }
  return false
}

let unregisterExitEdit = null
let unregisterDirtyGetter = null

onMounted(async () => {
  window.addEventListener('message', onMessage)
  readThemeBase()
  themeObserver = new MutationObserver(readThemeBase)
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme-base'] })
  // A read-only preview is not an editor: registering it would make the share
  // page's (nonexistent) back gesture think there is unsaved work.
  if (!props.readonly) {
    unregisterExitEdit = fileEditor.registerExitEditHandler(handleExit)
    unregisterDirtyGetter = fileEditor.registerDirtyGetter(() => dirty)
  }
  await refreshStatus()
})

onBeforeUnmount(() => {
  window.removeEventListener('message', onMessage)
  themeObserver?.disconnect()
  themeObserver = null
  stopPolling()
  if (unregisterExitEdit) {
    unregisterExitEdit()
    unregisterExitEdit = null
  }
  if (unregisterDirtyGetter) {
    unregisterDirtyGetter()
    unregisterDirtyGetter = null
  }
  if (!props.readonly) {
    fileEditor.setEditing(false)
  }
})

// Content can arrive after the editor mounted (the file fetch resolves later),
// or be replaced after a save. Push it into the editor once it is ready.
watch(
  () => props.file?.content,
  (content, old) => {
    if (content == null || content === old) return
    if (iframeReady) {
      postToFrame({ action: 'load', xml: content, autosave: 1 })
      setDirty(false)
    }
  }
)

// The language picker changes the editor UI language; reloading the iframe is
// the only way draw.io accepts it.
// Language and light/dark both come from the iframe URL and have no runtime
// switch, so a change means a fresh frame. The bound `src` reloads the iframe by
// itself — assigning it again here would navigate a second time. All this has to
// do is stop answering with the old scene until the new frame announces itself.
// The scene then comes from the live copy (see sendLoad), so an in-progress edit
// survives the swap.
watch([locale, isDark], () => {
  if (phase.value !== 'ready') return
  iframeReady = false
})

/**
 * Ask drawio for an export and hand back the raw payload.
 *
 * drawio's own "Export as" menu is disabled in embed mode, so this is the only
 * way to produce a file — the caller decides where it goes.
 */
function requestExport(format) {
  return new Promise((resolve, reject) => {
    if (!iframeReady) {
      reject(new Error(t('file.drawioExport.notReady')))
      return
    }
    // Large diagrams take a while to rasterise; the timer only guards against
    // an iframe that never answers.
    const timer = setTimeout(() => {
      if (pendingUserExport) {
        pendingUserExport = null
        reject(new Error(t('file.drawioExport.timeout')))
      }
    }, 20000)
    pendingUserExport = { resolve, timer }
    postToFrame({ action: 'export', format })
  })
}

async function requestExit() {
  if (props.readonly) return true
  return handleExit()
}

defineExpose({ requestExit, requestExport, isDirty: () => dirty })
</script>

<style scoped>
.drawio-viewer {
  display: flex;
  flex: 1;
  min-height: 0;
  width: 100%;
}

.drawio-frame {
  flex: 1;
  width: 100%;
  height: 100%;
  border: none;
  background: #fff;
}

.drawio-gate {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: calc(24 * var(--dp));
}

.drawio-gate-card {
  max-width: calc(420 * var(--dp));
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: calc(10 * var(--dp));
  padding: calc(24 * var(--dp));
  border-radius: var(--radius-lg);
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  text-align: center;
}

.drawio-gate-body {
  display: flex;
  justify-content: center;
  padding: calc(12 * var(--dp)) 0;
}

.drawio-gate-title {
  margin: 0;
  font-size: var(--font-size-lg);
  font-weight: 600;
  color: var(--text-primary);
}

.drawio-gate-desc {
  margin: 0;
  font-size: var(--font-size-md);
  line-height: 1.6;
  color: var(--text-secondary);
}

.drawio-progress {
  display: flex;
  flex-direction: column;
  gap: calc(6 * var(--dp));
}

.drawio-progress-track {
  height: calc(6 * var(--dp));
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  overflow: hidden;
}

.drawio-progress-fill {
  height: 100%;
  border-radius: var(--radius-full);
  background: var(--accent-color);
  transition: width 0.3s ease;
}

.drawio-progress-text {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.drawio-error {
  margin: 0;
  font-size: var(--font-size-xs);
  color: var(--color-red);
  word-break: break-all;
}

.drawio-action {
  margin-top: calc(4 * var(--dp));
  padding: calc(8 * var(--dp)) calc(18 * var(--dp));
  border: none;
  border-radius: var(--radius-md);
  background: var(--accent-color);
  color: #fff;
  font-size: var(--font-size-md);
  cursor: pointer;
}

.drawio-action:hover {
  opacity: 0.9;
}

.drawio-upload {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: calc(8 * var(--dp));
  margin-top: calc(6 * var(--dp));
  padding-top: calc(10 * var(--dp));
  border-top: 1px dashed var(--border-color);
}

.drawio-upload-hint {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.drawio-upload-btn {
  padding: calc(4 * var(--dp)) calc(10 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  cursor: pointer;
}

.drawio-upload-btn:hover:not(:disabled) {
  background: var(--bg-tertiary);
}

.drawio-upload-btn:disabled {
  opacity: 0.5;
  cursor: default;
}

/* The real control is the button above; this stays out of the layout. */
.drawio-file-input {
  display: none;
}
</style>
