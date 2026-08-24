<template>
  <div class="excalidraw-viewer">
    <iframe
      ref="frameRef"
      class="excalidraw-frame"
      src="/vendor/excalidraw/index.html"
      sandbox="allow-scripts allow-same-origin allow-modals allow-downloads allow-popups"
    />
  </div>
</template>

<script setup>
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useFileEditor } from '@/composables/useFileEditor.ts'
import { useCodeEditorSave } from '@/composables/useCodeEditorSave.ts'

/**
 * Excalidraw editor for .excalidraw files, rendered inside a sandboxed iframe.
 *
 * The iframe hosts an independent React build (web/vendor-build/excalidraw)
 * served at /vendor/excalidraw/index.html — Vue and React never share a
 * bundle. Communication is via postMessage:
 *
 *   this → iframe: { event: 'load', data: { content, lang, theme } }
 *                   { event: 'theme', data: { theme } } | { event: 'lang', data: { lang } }
 *   iframe → this: ready | changed | save | exit
 *
 * .excalidraw files open directly in the editor (no browse/read-only mode),
 * so the component mounts in "editing" state and registers the dirty-save
 * handlers exactly like CodeMirrorViewer.
 */

const props = defineProps({
    file: Object,
})

const { locale } = useI18n()
const frameRef = ref(null)
const fileEditor = useFileEditor()
const { saveFile } = useCodeEditorSave()

let contentSent = false // whether initial .excalidraw JSON was handed over
let iframeReady = false // iframe signalled 'ready'
let pendingContent = null // content received before the iframe signalled 'ready'
let dirty = false // local dirty flag (kept in sync with the global getter)

// Resolve the current theme base ("light"|"dark") from the host document —
// the app sets data-theme-base on <html> via applyThemeAttributes.
function currentTheme() {
    return document.documentElement.getAttribute('data-theme-base') === 'dark' ? 'dark' : 'light'
}

function currentLang() {
    return typeof locale.value === 'string' ? locale.value : 'en'
}

// Messages from the iframe are received here.
function onMessage(e) {
    // Only accept messages from our own iframe — anything else could be a
    // forged save/changed/exit from another window.
    if (e.source !== frameRef.value?.contentWindow) return
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
            // Hand over any content that arrived before the iframe was ready
            // (the fetch may resolve before React finishes mounting), plus the
            // initial content. Without this, the editor stays blank when the
            // content watcher fires before 'ready'.
            if (pendingContent != null && !contentSent) {
                sendLoad(pendingContent)
            } else if (props.file?.content != null && !contentSent) {
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
        JSON.stringify({
            event: 'load',
            data: { content, lang: currentLang() },
        }),
        window.location.origin
    )
    contentSent = true
    // Theme is sent separately (after load) — applying theme+lang in the same
    // load message makes Excalidraw's theme initialization reset the language
    // back to English.
    if (iframeReady) {
        sendTheme(currentTheme())
    }
}

function sendTheme(theme) {
    if (!frameRef.value?.contentWindow || !iframeReady) return
    frameRef.value.contentWindow.postMessage(
        JSON.stringify({ event: 'theme', data: { theme } }),
        window.location.origin
    )
}

function sendLang(lang) {
    if (!frameRef.value?.contentWindow || !iframeReady) return
    frameRef.value.contentWindow.postMessage(
        JSON.stringify({ event: 'lang', data: { lang } }),
        window.location.origin
    )
}

// Resolver for the in-flight save round-trip. When handleExit sends
// saveRequest, this resolves once the iframe replies with 'save' (and the
// write completes), so navigation waits for persistence before tearing down.
let saveResolver = null

function sendSaveRequest() {
    if (!frameRef.value?.contentWindow || !iframeReady) return
    frameRef.value.contentWindow.postMessage(
        JSON.stringify({ event: 'saveRequest' }),
        window.location.origin
    )
}

function setDirty(v) {
    dirty = v
}

// Called when the iframe hands back serialized .excalidraw JSON.
async function handleIframeSave(content) {
    const ok = await saveFile(props.file?.path || '', content)
    // Only clear dirty when the write actually succeeded — otherwise the
    // exit flow would discard changes silently.
    if (ok) {
        dirty = false
    }
    if (saveResolver) {
        saveResolver(ok)
        saveResolver = null
    }
}

// Exit flow used by the global back gesture / navigation: ask the iframe for
// its current scene, then persist it if dirty. Resolves true when the scene
// is saved (or nothing was dirty); resolves false if the write failed, so the
// caller can keep the file open and surface the error.
function handleExit() {
    if (!iframeReady) return true
    // If nothing is dirty there's nothing to persist.
    if (!dirty) return true
    // Wait for the iframe's 'save' reply so persistence completes before the
    // editor unmounts.
    return new Promise((resolve) => {
        saveResolver = (ok) => {
            // A successful save means the back gesture may proceed next time
            // (the "editing" flag consumed the first back to persist changes).
            if (ok) fileEditor.setEditing(false)
            resolve(ok)
        }
        sendSaveRequest()
        // Safety timeout — never block navigation forever if the iframe is
        // unresponsive (e.g. it was killed or the message was lost).
        setTimeout(() => {
            if (saveResolver) {
                saveResolver(false)
                saveResolver = null
            }
        }, 3000)
    })
}

let unregisterExitEdit = null
let unregisterDirtyGetter = null
let themeObserver = null

onMounted(() => {
    window.addEventListener('message', onMessage)
    // Excalidraw opens directly in the editor, so it counts as "editing" for
    // the global back gesture (without this, back/navigation skips the
    // dirty-save flow entirely and unsaved edits are silently lost).
    fileEditor.setEditing(true)
    // Register with the shared editor state so the global back gesture and
    // FileViewer's guardExitEdit find us.
    unregisterExitEdit = fileEditor.registerExitEditHandler(handleExit)
    unregisterDirtyGetter = fileEditor.registerDirtyGetter(() => dirty)

    // Push host theme changes (light/dark) into the iframe so the editor stays
    // in sync with the rest of the app.
    themeObserver = new MutationObserver(() => {
        sendTheme(currentTheme())
    })
    themeObserver.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ['data-theme-base'],
    })
})

onBeforeUnmount(() => {
    window.removeEventListener('message', onMessage)
    if (themeObserver) {
        themeObserver.disconnect()
        themeObserver = null
    }
    if (unregisterExitEdit) {
        unregisterExitEdit()
        unregisterExitEdit = null
    }
    if (unregisterDirtyGetter) {
        unregisterDirtyGetter()
        unregisterDirtyGetter = null
    }
    fileEditor.setEditing(false)
})

// Public method used by FileViewer's exit handler to route the global back
// gesture to this editor instead of the (inactive) CodeMirror editor.
async function requestExit() {
    return handleExit()
}

// Push app language changes into the iframe editor.
watch(locale, (lang) => {
    if (typeof lang !== 'string' || !lang) return
    sendLang(lang)
})

// If the file content arrives (fetch resolves) or is replaced externally
// (e.g. auto-refresh after the iframe saved), push the fresh scene into the
// editor. Content that arrives before the iframe signals 'ready' is queued in
// pendingContent and flushed on 'ready'.
watch(
    () => props.file?.content,
    (content, oldContent) => {
        if (content == null || content === oldContent) return
        if (iframeReady) {
            sendLoad(content)
        } else {
            pendingContent = content
        }
    }
)

// Internal helper used by tests to inspect state.
defineExpose({
    get isDirty() {
        return dirty
    },
    requestExit,
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
