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

          <div v-if="phase === 'installing'" class="drawio-progress">
            <div class="drawio-progress-track">
              <div class="drawio-progress-fill" :style="{ width: percent + '%' }" />
            </div>
            <span class="drawio-progress-text">{{ progressText }}</span>
          </div>

          <p v-if="phase === 'failed' && errorText" class="drawio-error">{{ errorText }}</p>

          <button
            v-if="phase !== 'installing' && !readonly"
            class="drawio-action"
            @click="startInstall"
          >
            {{ phase === 'failed' ? t('file.viewer.drawioRetry') : t('file.viewer.drawioInstall') }}
          </button>
          <p v-else-if="phase !== 'installing' && readonly" class="drawio-gate-desc">
            {{ t('file.viewer.drawioReadonlyMissing') }}
          </p>
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

const { t, locale } = useI18n()
const dialog = useDialog()
const fileEditor = useFileEditor()
const { saveFile } = useCodeEditorSave()

const frameRef = ref(null)
// 'checking' | 'absent' | 'installing' | 'ready' | 'failed'
const phase = ref('checking')
const basePath = ref('')
const downloaded = ref(0)
const total = ref(0)
const errorText = ref('')

let pollTimer = null
let iframeReady = false
let dirty = false
let latestXml = ''
let pendingExport = null

const frameSrc = computed(() => {
  const lang = typeof locale.value === 'string' && locale.value.startsWith('zh') ? 'zh' : 'en'
  // The directory URL, not ".../index.html": http.FileServer redirects a path
  // ending in index.html back to the directory, which would cost an extra round
  // trip (and drop the embed parameters on some clients).
  //
  // Read-only preview hides the chrome and enables pan/zoom only, matching the
  // share page's treatment of Excalidraw — there is no save path there.
  const view = props.readonly ? '&nav=1&chrome=0' : ''
  return `${basePath.value}?embed=1&proto=json&spin=1&modified=1&noSaveBtn=1&noExitBtn=1&lang=${lang}${view}`
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
    case 'export':
      if (pendingExport) {
        pendingExport(msg.data || latestXml)
        pendingExport = null
      }
      break
  }
}

function sendLoad() {
  if (!iframeReady) return
  postToFrame({ action: 'load', xml: props.file?.content || '', autosave: props.readonly ? 0 : 1 })
}

function setDirty(v) {
  dirty = v
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

async function saveCurrent() {
  const xml = await requestXml()
  if (!xml) return false
  const ok = await saveFile(props.file?.path || '', xml)
  if (ok) setDirty(false)
  return ok
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
    const ok = await saveCurrent()
    if (ok) fileEditor.setEditing(false)
    return ok
  }
  if (choice === null) {
    setDirty(false)
    fileEditor.setEditing(false)
    return true
  }
  return false
}

let unregisterExitEdit = null
let unregisterDirtyGetter = null

onMounted(async () => {
  window.addEventListener('message', onMessage)
  // A read-only preview is not an editor: registering it would make the share
  // page's (nonexistent) back gesture think there is unsaved work.
  if (!props.readonly) {
    fileEditor.setEditing(true)
    unregisterExitEdit = fileEditor.registerExitEditHandler(handleExit)
    unregisterDirtyGetter = fileEditor.registerDirtyGetter(() => dirty)
  }
  await refreshStatus()
})

onBeforeUnmount(() => {
  window.removeEventListener('message', onMessage)
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
watch(locale, () => {
  if (phase.value === 'ready') {
    iframeReady = false
    const el = frameRef.value
    if (el) el.src = frameSrc.value
  }
})

async function requestExit() {
  if (props.readonly) return true
  return handleExit()
}

defineExpose({ requestExit, isDirty: () => dirty })
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
</style>
