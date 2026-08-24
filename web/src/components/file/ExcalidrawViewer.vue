<template>
  <div class="excalidraw-viewer">
    <iframe
      ref="frameRef"
      class="excalidraw-frame"
      src="/vendor/excalidraw/index.html"
      sandbox="allow-scripts allow-modals allow-downloads allow-popups"
    />
  </div>
</template>

<script setup>
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useFileEditor } from '@/composables/useFileEditor.ts'
import { useCodeEditorSave } from '@/composables/useCodeEditorSave.ts'

/**
 * Excalidraw editor for .excalidraw files, rendered inside a sandboxed iframe.
 *
 * The iframe hosts an independent React build (web/vendor-build/excalidraw)
 * served at /vendor/excalidraw/index.html — Vue and React never share a
 * bundle. Communication is via postMessage:
 *
 *   this → iframe: { event: 'load', data: { content } } | { event: 'saveRequest' }
 *   iframe → this: ready | changed | save | exit
 *
 * .excalidraw files open directly in the editor (no browse/read-only mode),
 * so the component mounts in "editing" state and registers the dirty-save
 * handlers exactly like CodeMirrorViewer.
 */

const props = defineProps({
    file: Object,
})

const frameRef = ref(null)
const fileEditor = useFileEditor()
const { saveFile } = useCodeEditorSave()

let contentSent = false // whether initial .excalidraw JSON was handed over
let iframeReady = false // iframe signalled 'ready'
let dirty = false // local dirty flag (kept in sync with the global getter)

// Messages from the iframe are received here.
function onMessage(e) {
    let msg
    try {
        msg = typeof e.data === 'string' ? JSON.parse(e.data) : e.data
    } catch {
        return
    }
    if (!msg || typeof msg.event !== 'string') return

    switch (msg.event) {
        case 'ready':
            iframeReady = true
            // Hand over the file content as soon as both sides are ready.
            if (!contentSent && props.file?.content != null) {
                sendLoad(props.file.content)
            }
            break
        case 'changed':
            setDirty(true)
            break
        case 'save':
            if (msg.data?.content != null) {
                handleIframeSave(msg.data.content)
            }
            break
        case 'exit':
            // The iframe is leaving (e.g. its own back button). Report the
            // dirty state to the parent so FileViewer can run the same
            // save/discard confirmation as other editors.
            setDirty(!!msg.data?.modified)
            break
    }
}

function sendLoad(content) {
    if (!frameRef.value?.contentWindow) return
    frameRef.value.contentWindow.postMessage(
        JSON.stringify({ event: 'load', data: { content } }),
        '*'
    )
    contentSent = true
}

function sendSaveRequest() {
    if (!frameRef.value?.contentWindow || !iframeReady) return
    frameRef.value.contentWindow.postMessage(
        JSON.stringify({ event: 'saveRequest' }),
        '*'
    )
}

function setDirty(v) {
    dirty = v
}

// Called when the iframe hands back serialized .excalidraw JSON.
async function handleIframeSave(content) {
    await saveFile(props.file?.path || '', content)
    dirty = false
}

// Exit flow used by the global back gesture / navigation: ask the iframe for
// its current scene, then persist it if dirty. Returns true when edit mode
// may be left (saved or discarded), false if the user cancelled.
async function handleExit() {
    // Request a save from the iframe; if it's dirty it replies 'save' which
    // we persist. For a clean exit there is nothing to write.
    sendSaveRequest()
    return true
}

let unregisterExitEdit = null
let unregisterDirtyGetter = null

onMounted(() => {
    window.addEventListener('message', onMessage)
    // Register with the shared editor state so the global back gesture and
    // FileViewer's guardExitEdit find us.
    unregisterExitEdit = fileEditor.registerExitEditHandler(handleExit)
    unregisterDirtyGetter = fileEditor.registerDirtyGetter(() => dirty)
})

onBeforeUnmount(() => {
    window.removeEventListener('message', onMessage)
    if (unregisterExitEdit) {
        unregisterExitEdit()
        unregisterExitEdit = null
    }
    if (unregisterDirtyGetter) {
        unregisterDirtyGetter()
        unregisterDirtyGetter = null
    }
})

// If the file content is replaced externally (e.g. auto-refresh after the
// iframe saved), push the fresh scene into the editor.
watch(
    () => props.file?.content,
    (content, oldContent) => {
        if (content == null || content === oldContent || !iframeReady) return
        sendLoad(content)
    }
)

// Internal helper used by tests to inspect state.
defineExpose({
    get isDirty() {
        return dirty
    },
})
</script>

<style scoped>
.excalidraw-viewer {
    display: flex;
    flex: 1;
    min-height: 0;
    width: 100%;
}

.excalidraw-frame {
    flex: 1;
    width: 100%;
    height: 100%;
    border: none;
    background: #fff;
}
</style>
