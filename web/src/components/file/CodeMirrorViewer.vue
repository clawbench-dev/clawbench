<template>
  <div class="cm-viewer" :class="{ 'is-editable': editable, 'cm-readonly': !editable }">
    <div ref="editorHost" class="cm-host"></div>
    <div v-if="editable" class="code-editor-actions">
      <span class="code-editor-status">
        <span class="dirty-dot" v-if="dirty"></span>
      </span>
      <button class="editor-btn icon-btn" :disabled="!canUndo || saving" @mousedown.prevent @click="handleUndo" :title="t('file.editor.undo')">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M3 10h13a4 4 0 0 1 0 8H9"/><path d="M3 10l5-5M3 10l5 5"/></svg>
      </button>
      <button class="editor-btn icon-btn" :disabled="!canRedo || saving" @mousedown.prevent @click="handleRedo" :title="t('file.editor.redo')">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M21 10H8a4 4 0 0 0 0 8h7"/><path d="M21 10l-5-5M21 10l-5 5"/></svg>
      </button>
      <button v-if="dirty" class="editor-btn icon-btn primary" :disabled="saving" @mousedown.prevent @click="emit('save', getValue())" :title="t('file.editor.save')">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>
      </button>
      <button class="editor-btn icon-btn" @mousedown.prevent @click="handleExit" :title="t('file.editor.exitEdit')">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M18 6L6 18M6 6l12 12"/></svg>
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, shallowRef, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Compartment, EditorState, RangeSetBuilder } from '@codemirror/state'
import { EditorView, lineNumbers, Decoration, gutter, GutterMarker, keymap } from '@codemirror/view'
import { defaultKeymap, historyKeymap, history, undo, redo, undoDepth, redoDepth } from '@codemirror/commands'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { tags } from '@lezer/highlight'
import { buildLangExtension } from '@/utils/codeEditorLang'
import { diffMarkers, openDiffDrawer } from '@/composables/useMarkdownDiff.ts'
import { flashRanges, flashType } from '@/composables/useFileRefresh.ts'
import { copyText } from '@/utils/clipboard.ts'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'
import { buildOverlayDecorations } from '@/utils/codeMirrorOverlay.ts'
import { useDialog } from '@/composables/useDialog.ts'
import { useCodeStickyScroll } from '@/composables/useCodeStickyScroll.ts'

const props = defineProps({
    file: Object,
    content: { type: String, default: '' },
    language: { type: String, default: 'plaintext' },
    wordWrap: { type: Boolean, default: false },
    showLineNumbers: { type: Boolean, default: true },
    /** VS Code-style sticky scroll: pin enclosing scope definition lines to the top. */
    stickyScroll: { type: Boolean, default: true },
    /** false = read-only browse (default); true = source editing */
    editable: { type: Boolean, default: false },
    saving: { type: Boolean, default: false },
})
const emit = defineEmits(['save', 'cancel', 'exitEdit'])

const { t } = useI18n()
const editorHost = ref(null)
// EditorView must be stored in a shallowRef: Vue's ref() wraps objects in a
// reactive Proxy, and CodeMirror's undo/redo build transactions against the
// raw EditorState identity. Operating on the proxied view/state yields a
// "transaction doesn't start from the previous state" RangeError that silently
// breaks the Undo/Redo buttons.
const view = shallowRef(null)
const diffLineMap = ref(new Map())
const quoteQuestion = useQuoteQuestion()
const dialog = useDialog()
const canUndo = ref(false)
const canRedo = ref(false)
const dirty = ref(false)

const MONO_FONT = "'SF Mono', Monaco, 'Cascadia Code', 'Segoe UI Mono', 'Roboto Mono', Consolas, 'Liberation Mono', monospace"

const codeMirrorTheme = EditorView.theme({
    '&': {
        backgroundColor: 'var(--code-bg)',
        color: 'var(--text-primary)',
    },
    '.cm-content': {
        fontFamily: MONO_FONT,
        fontSize: '13px',
        lineHeight: '1.6',
        caretColor: 'var(--accent-color)',
        padding: '0 0 24px 0',
        minWidth: 'max-content',
    },
    '.cm-scroller': {
        fontFamily: MONO_FONT,
        lineHeight: '1.6',
    },
    '.cm-gutters': {
        backgroundColor: 'var(--code-bg)',
        color: 'var(--text-muted)',
        border: 'none',
        borderRight: '1px solid var(--border-color)',
    },
    '.cm-lineNumbers .cm-gutterElement': {
        color: 'var(--text-muted)',
        opacity: '0.5',
        minWidth: '1.2em',
        padding: '0 6px 0 8px',
    },
    '.cm-lineNumbers .cm-gutterElement:hover': {
        opacity: '1',
        color: 'var(--accent-color)',
    },
    '.cm-diff-gutter .cm-gutterElement': {
        padding: '0 2px',
        minWidth: '18px',
    },
    '.cm-activeLine': { backgroundColor: 'color-mix(in srgb, var(--accent-color) 7%, transparent)' },
    '.cm-activeLineGutter': { backgroundColor: 'color-mix(in srgb, var(--accent-color) 7%, transparent)' },
    '.cm-cursor, &.cm-focused .cm-cursor': { borderLeftColor: 'var(--accent-color)' },
    '&.cm-focused .cm-selectionBackground, ::selection, .cm-selectionBackground, .cm-content ::selection': {
        backgroundColor: 'color-mix(in srgb, var(--accent-color) 25%, transparent)',
    },
    '.cm-selectionMatch': { backgroundColor: 'color-mix(in srgb, var(--accent-color) 18%, transparent)' },
})

const codeHighlightStyle = HighlightStyle.define([
    { tag: tags.comment, color: 'var(--code-syntax-comment)', fontStyle: 'italic' },
    { tag: [tags.keyword, tags.operator, tags.modifier], color: 'var(--code-syntax-keyword)' },
    { tag: [tags.string, tags.special(tags.string), tags.regexp, tags.monospace], color: 'var(--code-syntax-string)' },
    { tag: [tags.number, tags.bool, tags.null], color: 'var(--code-syntax-number)' },
    { tag: [tags.function(tags.variableName), tags.function(tags.propertyName), tags.function(tags.definition(tags.variableName))], color: 'var(--code-syntax-function)' },
    { tag: [tags.typeName, tags.className, tags.namespace], color: 'var(--code-syntax-type)' },
    { tag: [tags.variableName, tags.definition(tags.variableName)], color: 'var(--code-syntax-variable)' },
    { tag: [tags.propertyName], color: 'var(--code-syntax-property)' },
    { tag: [tags.tagName], color: 'var(--code-syntax-tag)' },
    { tag: [tags.attributeName], color: 'var(--code-syntax-attribute)' },
    { tag: [tags.meta, tags.contentSeparator], color: 'var(--code-syntax-meta)' },
    { tag: [tags.heading], color: 'var(--code-syntax-heading)', fontWeight: 'bold' },
    { tag: [tags.link, tags.url], color: 'var(--code-syntax-link)', textDecoration: 'underline' },
    { tag: [tags.emphasis], fontStyle: 'italic' },
    { tag: [tags.strong], fontWeight: 'bold' },
    { tag: [tags.quote], color: 'var(--code-syntax-comment)', fontStyle: 'italic' },
])

// Sticky scroll (browse mode only): pin enclosing scope definition lines to the top.
const stickyScrollEnabled = () => props.stickyScroll && !props.editable
const sticky = useCodeStickyScroll({
    highlighter: codeHighlightStyle,
    onStickyClick: (lineNum) => scrollToLine(lineNum),
})

// ─── Compartments for dynamically toggled extensions ───
const readonlyCompartment = new Compartment()
const langCompartment = new Compartment()
const lineNumbersCompartment = new Compartment()
const wrapCompartment = new Compartment()
const overlayCompartment = new Compartment()
const jumpFlashCompartment = new Compartment()

// Gutter markers for diff markers (M/D/+), clickable to open the diff drawer.
class DiffGutterMarker extends GutterMarker {
    constructor(marker) { super(); this.marker = marker }
    toDOM() {
        const span = document.createElement('span')
        span.className = `cm-diff-gutter-marker cm-diff-gutter-${this.marker.type}`
        span.dataset.markerId = this.marker.id
        span.textContent = this.marker.label
        span.title = this.marker.ariaLabel
        span.setAttribute('aria-label', this.marker.ariaLabel)
        return span
    }
}

const diffGutter = gutter({
    class: 'cm-diff-gutter',
    lineMarker(_view, line) {
        const marker = diffLineMap.value.get(line.number)
        return marker ? new DiffGutterMarker(marker) : null
    },
    initialSpacer: () => null,
    lineMarkerChange: (update) => update.docChanged,
})

// ─── Interactions (click path / diff gutter, double-click copy + quote) ───
function handleEditorClick(event) {
    const target = event.target
    const gutterMarker = target.closest?.('.cm-diff-gutter-marker')
    if (gutterMarker) {
        event.preventDefault()
        event.stopPropagation()
        const id = gutterMarker.getAttribute('data-marker-id')
        const marker = diffMarkers.value.find(m => m.id === id)
        if (marker) openDiffDrawer(marker)
        return true
    }
    return false
}

function handleEditorDblClick(_event, editor) {
    const sel = editor.state.selection.main
    if (sel.empty) return
    const text = editor.state.sliceDoc(sel.from, sel.to).trim()
    if (!text) return
    const startLine = editor.state.doc.lineAt(sel.from).number
    const endLine = editor.state.doc.lineAt(sel.to).number
    copyText(text)
    quoteQuestion.showBar({
        text,
        filePath: props.file?.path || '',
        language: props.language,
        startLine,
        endLine,
    })
}

const interactionExtension = EditorView.domEventHandlers({
    click(event, editor) { handleEditorClick(event, editor) },
    dblclick(event, editor) { handleEditorDblClick(event, editor) },
})

// ─── Selection-based quote question (read-only mode) ───
// CodeMirror keeps its selection internally, so the global selectionchange handler
// never fires for it. Watch selection changes here and surface the quote bar with
// accurate line numbers from the editor state.
let selDebounceTimer = null
function handleSelectionChange(update) {
    if (props.editable) return // only read-only browse
    if (!update.selectionSet && !update.docChanged) return
    const sel = update.state.selection.main
    if (selDebounceTimer) clearTimeout(selDebounceTimer)
    if (sel.empty) {
        selDebounceTimer = setTimeout(() => quoteQuestion.hideBar(), 200)
        return
    }
    const text = update.state.sliceDoc(sel.from, sel.to).trim()
    if (!text) return
    const startLine = update.state.doc.lineAt(sel.from).number
    const endLine = update.state.doc.lineAt(sel.to).number
    selDebounceTimer = setTimeout(() => {
        quoteQuestion.showBar({
            text,
            filePath: props.file?.path || '',
            language: props.language,
            startLine,
            endLine,
        })
    }, 200)
}

const selectionExtension = EditorView.updateListener.of(handleSelectionChange)

// Re-measure the sticky overlay whenever the editor geometry changes (resize,
// line-number toggle, wrap toggle, doc height shifts) so its content offset and
// full-width row stay aligned with the code lines.
const geometryExtension = EditorView.updateListener.of((update) => {
    if (update.geometryChanged) sticky.refresh()
})

// ─── Edit state tracking (undo/redo availability, dirty) ───
// Track the saved content snapshot so dirty = current doc differs from saved version.
let savedSnapshot = ''
function handleEditStateChange(update) {
    if (!props.editable) return
    if (!update.docChanged && !update.selectionSet) return
    const state = update.state
    canUndo.value = undoDepth(state) > 0
    canRedo.value = redoDepth(state) > 0
    dirty.value = state.doc.toString() !== savedSnapshot
}

const editStateExtension = EditorView.updateListener.of(handleEditStateChange)

// ─── Overlay decorations (diff lines + flash + clickable paths) ───
function recomputeOverlay() {
    const editor = view.value
    if (!editor) return
    const state = editor.state
    const { decorations, diffLines } = buildOverlayDecorations(state, diffMarkers.value, flashRanges.value, flashType.value)
    diffLineMap.value = diffLines
    // Only mount the diff gutter while diff markers exist; otherwise it would
    // occupy a gutter column even when line numbers are hidden.
    const ext = diffMarkers.value.length > 0 ? [diffGutter] : []
    editor.dispatch({
        effects: overlayCompartment.reconfigure([...ext, EditorView.decorations.of(decorations)]),
    })
}

// ─── Scroll-to-line (search/TOC jump) with flash ───
// CodeMirror's EditorView.scrollIntoView snaps instantly, so we animate
// scrollDOM.scrollTop toward the target line to give a smooth scroll instead.
let flashTimer = null
let scrollRAF = null
function centeredScrollTop(editor, pos) {
    const scroller = editor.scrollDOM
    const block = editor.lineBlockAt(pos)
    const viewportHeight = scroller.clientHeight
    const maxScrollTop = Math.max(0, scroller.scrollHeight - viewportHeight)
    const centeredTop = block.top - (viewportHeight - block.height) / 2
    return Math.min(maxScrollTop, Math.max(0, centeredTop))
}
function smoothScrollToLine(editor, pos) {
    const scroller = editor.scrollDOM
    const targetTop = centeredScrollTop(editor, pos)
    const startTop = scroller.scrollTop
    const delta = targetTop - startTop
    if (Math.abs(delta) < 0.5) return
    const duration = 300
    const startTime = performance.now()
    if (scrollRAF) cancelAnimationFrame(scrollRAF)
    function step(now) {
        const t = Math.min(1, (now - startTime) / duration)
        // easeInOutCubic
        const eased = t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2
        scroller.scrollTop = startTop + delta * eased
        if (t < 1) {
            scrollRAF = requestAnimationFrame(step)
        } else {
            // Recalculate after the animation: CodeMirror may finish measuring
            // the document while the smooth movement is in progress.
            scroller.scrollTop = centeredScrollTop(editor, pos)
            scrollRAF = null
        }
    }
    scrollRAF = requestAnimationFrame(step)
}
let pendingScrollRequestId = null
let pendingScrollRAF = null
function scrollToLine(line, lineEnd) {
    const editor = view.value
    if (!editor) return
    const target = Math.min(Math.max(1, line || 1), editor.state.doc.lines)
    const pos = editor.state.doc.line(target).from
    smoothScrollToLine(editor, pos)
    const builder = new RangeSetBuilder()
    const last = Math.min(lineEnd || target, editor.state.doc.lines)
    for (let n = target; n <= last; n++) {
        const l = editor.state.doc.line(n)
        builder.add(l.from, l.from, Decoration.line({ class: 'line-flash' }))
    }
    const flashDeco = builder.finish()
    editor.dispatch({ effects: jumpFlashCompartment.reconfigure(EditorView.decorations.of(flashDeco)) })
    if (flashTimer) clearTimeout(flashTimer)
    flashTimer = setTimeout(() => {
        editor.dispatch({ effects: jumpFlashCompartment.reconfigure(EditorView.decorations.of(Decoration.none)) })
    }, 1500)
}

function onScrollToLine(e) {
    const d = e.detail
    if (!d || typeof d.line !== 'number') return
    if (d.path && d.path !== props.file?.path) return
    if (!d.requestId) {
        window.dispatchEvent(new CustomEvent('cancel-scroll-restore'))
        scrollToLine(d.line, d.lineEnd)
        return
    }
    if (pendingScrollRequestId === d.requestId) return
    pendingScrollRequestId = d.requestId

    const applyWhenLaidOut = () => {
        if (pendingScrollRequestId !== d.requestId) return
        const editor = view.value
        if (!editor || (d.path && d.path !== props.file?.path)) {
            pendingScrollRequestId = null
            return
        }
        const scroller = editor.scrollDOM
        // A newly mounted editor can have content but no measured viewport for
        // one or more frames. Wait for layout before calculating the center.
        if (scroller.clientHeight <= 0 && scroller.scrollHeight > 0) {
            pendingScrollRAF = requestAnimationFrame(applyWhenLaidOut)
            return
        }
        pendingScrollRequestId = null
        pendingScrollRAF = null
        window.dispatchEvent(new CustomEvent('cancel-scroll-restore'))
        scrollToLine(d.line, d.lineEnd)
        window.dispatchEvent(new CustomEvent('cm-scroll-to-line-handled', { detail: { requestId: d.requestId } }))
    }
    // Always defer the first measurement by one frame so the editor has its
    // final height after a rendered/raw view transition.
    pendingScrollRAF = requestAnimationFrame(applyWhenLaidOut)
}

// ─── Assemble extensions ───
// All extensions are passed directly to EditorState at creation — NO basicSetup,
// NO vue-codemirror. Toggleable parts live in top-level Compartments, which
// CodeMirror reconfigures reliably (verified against raw CodeMirror).
function buildAllExtensions() {
    return [
        readonlyCompartment.of(props.editable ? [] : [EditorState.readOnly.of(true)]),
        langCompartment.of([]), // placeholder; loaded async in mountLang()
        lineNumbersCompartment.of(props.showLineNumbers ? [lineNumbers()] : []),
        wrapCompartment.of(props.wordWrap ? [EditorView.lineWrapping] : []),
        codeMirrorTheme,
        syntaxHighlighting(codeHighlightStyle),
        history(),
        keymap.of([{ key: 'Mod-s', run: handleSaveShortcut, preventDefault: true }, ...defaultKeymap, ...historyKeymap]),
        interactionExtension,
        selectionExtension,
        editStateExtension,
        geometryExtension,
        jumpFlashCompartment.of([]),
        overlayCompartment.of([]),
    ]
}

/** Load the language extension asynchronously and apply it to the lang compartment. */
async function mountLang() {
    const ext = await buildLangExtension(props.language)
    if (view.value) {
        view.value.dispatch({ effects: langCompartment.reconfigure(ext) })
    }
}

function reconfigure(compartment, ext) {
    if (view.value) {
        view.value.dispatch({ effects: compartment.reconfigure(ext) })
    }
}

/** Set inputmode="none" on .cm-content to suppress mobile soft keyboard in read-only mode. */
function updateInputMode() {
    const editor = view.value
    if (!editor) return
    const contentEl = editor.contentDOM
    if (contentEl) {
        contentEl.inputMode = props.editable ? '' : 'none'
    }
}

onMounted(() => {
    view.value = new EditorView({
        parent: editorHost.value,
        state: EditorState.create({ doc: props.content || '', extensions: buildAllExtensions() }),
    })
    updateInputMode()
    savedSnapshot = props.content || ''
    recomputeOverlay()
    mountLang()
    sticky.init(view.value, props.file?.path, stickyScrollEnabled())
    window.addEventListener('cm-scroll-to-line', onScrollToLine)
})

onUnmounted(() => {
    window.removeEventListener('cm-scroll-to-line', onScrollToLine)
    if (pendingScrollRAF) cancelAnimationFrame(pendingScrollRAF)
    pendingScrollRAF = null
    pendingScrollRequestId = null
    if (scrollRAF) cancelAnimationFrame(scrollRAF)
    if (flashTimer) clearTimeout(flashTimer)
    if (selDebounceTimer) clearTimeout(selDebounceTimer)
    sticky.teardown()
    view.value?.destroy()
    view.value = null
})

// Reconfigure toggleable compartments when their props change.
watch([() => props.editable], () => {
    reconfigure(readonlyCompartment, props.editable ? [] : [EditorState.readOnly.of(true)])
    updateInputMode()
    // Reset edit state when entering edit mode
    canUndo.value = false
    canRedo.value = false
    dirty.value = false
    savedSnapshot = props.content || ''
})
watch([() => props.showLineNumbers], () => {
    reconfigure(lineNumbersCompartment, props.showLineNumbers ? [lineNumbers()] : [])
    // Line numbers shift the content right; re-measure the sticky overlay offset.
    sticky.refresh()
})
watch([() => props.wordWrap], () => {
    reconfigure(wrapCompartment, props.wordWrap ? [EditorView.lineWrapping] : [])
    sticky.refresh()
})
watch([() => props.language], () => mountLang())

// Sticky scroll: enable/disable when browse/edit mode or the toggle changes.
// Re-init on enabling so definition lines are re-fetched against the current doc.
watch(stickyScrollEnabled, (on) => {
    if (on) {
        sticky.init(view.value, props.file?.path, true)
    } else {
        sticky.setEnabled(false)
    }
})

// Rebuild overlay decorations whenever diff/flash/path inputs change.
watch([diffMarkers, flashRanges, flashType, () => props.content], recomputeOverlay)

// Update the document when the file content changes (file switch / refresh).
// Re-create the editor state to clear undo history so the dirty indicator resets.
watch(() => props.content, (c) => {
    const editor = view.value
    if (!editor) return
    const next = c || ''
    if (editor.state.doc.toString() !== next) {
        editor.setState(EditorState.create({ doc: next, extensions: buildAllExtensions() }))
        updateInputMode()
        canUndo.value = false
        canRedo.value = false
        dirty.value = false
    }
    savedSnapshot = next
    // Sticky def lines may have shifted; clear the highlight cache and recompute.
    if (view.value) sticky.refresh()
})

// Re-init sticky scroll when switching to a different file (new symbol set).
watch(() => props.file?.path, (path) => {
    if (view.value) sticky.init(view.value, path, stickyScrollEnabled())
})

function getValue() {
    return view.value ? view.value.state.doc.toString() : (props.content || '')
}

function handleUndo() {
    if (view.value) undo(view.value)
}

function handleRedo() {
    if (view.value) redo(view.value)
}

// Ctrl/Cmd+S save shortcut (Mod = Ctrl on Windows/Linux, Cmd on Mac). Mirrors
// the save button: only when editing, dirty, and not already saving.
function handleSaveShortcut() {
    if (!props.editable || !dirty.value || props.saving) return false
    emit('save', getValue())
    return true
}

// Exit edit mode. If there are unsaved changes, confirm whether to save,
// discard, or cancel (stay editing) instead of silently exiting.
async function handleExit() {
    if (!dirty.value) {
        emit('exitEdit')
        return
    }
    const choice = await dialog.confirm(t('file.editor.confirmExit'), {
        confirmText: t('file.editor.save'),
        cancelText: t('common.cancel'),
        extraText: t('file.editor.dontSave'),
        extraPrimedText: t('common.confirm'),
        onExtraAction: () => {},
    })
    if (choice === true) {
        // Save and exit (FileViewer's handleSave reloads content and leaves edit mode).
        emit('save', getValue())
    } else if (choice === null) {
        // Discard changes and exit.
        emit('exitEdit')
    }
    // choice === false → user cancelled; stay in edit mode.
}

defineExpose({ getValue, scrollToLine, getView: () => view.value, handleExit })
</script>

<style scoped>
.cm-viewer {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
    overflow: hidden;
    background: var(--code-bg);
    position: relative;
}
/* Edit mode: tint the whole code area with the accent color and frame the top
   with an accent line so browse vs edit are clearly distinct at a glance. */
.cm-viewer.is-editable {
    --code-bg-editing: color-mix(in srgb, var(--accent-color) 6%, var(--code-bg));
    background: var(--code-bg-editing);
    border-top: 2px solid var(--accent-color);
}
.cm-host {
    flex: 1;
    min-height: 0;
    overflow: hidden;
    position: relative;
}
.code-editor-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    justify-content: flex-end;
    padding: 6px 12px;
    border-top: 1px solid var(--border-color);
    background: var(--bg-secondary);
    flex-shrink: 0;
}
.code-editor-status {
    margin-right: auto;
    display: flex;
    align-items: center;
}
.dirty-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent-color);
    flex-shrink: 0;
    transition: opacity 0.2s;
}
.editor-btn.icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 5px 8px;
    line-height: 1;
}
.editor-btn {
    padding: 5px 14px;
    border: 1px solid var(--border-color);
    border-radius: 14px;
    background: transparent;
    color: var(--text-secondary);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
}
.editor-btn:hover { border-color: var(--accent-color); color: var(--accent-color); }
.editor-btn.primary { background: var(--accent-color); border-color: var(--accent-color); color: #fff; }
.editor-btn.primary:hover { filter: brightness(1.1); }
.editor-btn:disabled { opacity: 0.5; cursor: not-allowed; pointer-events: none; }
</style>

<style>
/* CodeMirror DOM (`.cm-editor`, `.cm-scroller`, `.cm-content`) is injected
   dynamically, so it lacks the scoped attribute — these rules MUST be global.
   Mirrors the browse-mode CodePreview styles. */

/* Bound the editor to the host (viewport) height so the scroller can overflow
   and scroll vertically. CM forces its own editor sizing, so we use !important. */
.cm-host .cm-editor {
    height: 100% !important;
    overflow: hidden;
}
/* In edit mode, tint the CodeMirror editor background and gutters to match the
   tinted container so the whole editable region reads as one distinct block. */
.cm-viewer.is-editable .cm-editor,
.cm-viewer.is-editable .cm-gutters {
    background: var(--code-bg-editing);
}
.cm-host .cm-editor .cm-scroller {
    height: 100% !important;
    overflow-y: auto;
}
/* Allow horizontal scroll for long lines when NOT wrapping (min-width: max-content).
   When wrapping is enabled (cm-lineWrapping), relax so soft-wrap actually wraps. */
.cm-host .cm-content.cm-lineWrapping {
    min-width: 0;
}

.cm-viewer.cm-readonly .cm-cursor,
.cm-viewer.cm-readonly .cm-activeLine,
.cm-viewer.cm-readonly .cm-activeLineGutter {
    display: none;
}

/* Diff marker gutter label */
.cm-diff-gutter-marker {
    display: inline-block;
    min-width: 16px;
    text-align: center;
    font-size: 11px;
    font-weight: 600;
    line-height: 1.6;
    cursor: pointer;
    user-select: none;
    border-radius: 3px;
    padding: 0 2px;
}
.cm-diff-gutter-marker:hover {
    background: color-mix(in srgb, var(--accent-color) 20%, transparent);
}
.cm-diff-gutter-M { color: var(--color-yellow); }
.cm-diff-gutter-D { color: var(--color-red); }
.cm-diff-gutter-A { color: var(--color-green); }

/* Diff line backgrounds */
.cm-diff-line-M { background: color-mix(in srgb, var(--color-yellow) 8%, transparent); }
.cm-diff-line-D { background: color-mix(in srgb, var(--color-red) 8%, transparent); }
.cm-diff-line-A { background: color-mix(in srgb, var(--color-green) 8%, transparent); }

/* Word-wrap mode */
.cm-viewer .cm-lineWrapping { word-break: break-all; }

/* Sticky scroll overlay (VS Code-style pinned scope definition lines).
   The overlay is injected into .cm-scroller as a sibling of .cm-content, so
   position:sticky pins it to the scroller's top while the code scrolls beneath.
   height:0 keeps it from pushing content; rows overflow above it. */
.cm-viewer .sticky-scroll-overlay {
    /* Vertically sticky only (top:0). NO left constraint — on the horizontal axis it
       stays in the flex row's normal flow, so it scrolls left/right with the content
       lines just like a normal line. */
    position: sticky;
    top: 0;
    height: 0;
    width: 0;
    overflow: visible;
    /* .cm-scroller is display:flex; a sticky overlay in the flex row must take no
       width so it doesn't shrink the .cm-content. Rows overflow to full width via
       --sticky-width. */
    flex: 0 0 0;
    /* Below the line-number gutter (z-index:200): when the sticky rows slide left on
       horizontal scroll, the fixed line numbers stay on top instead of being covered.
       Still above the code content so the sticky rows are visible over it. */
    z-index: 5;
    pointer-events: none;
}
.cm-viewer .sticky-line {
    /* Full-width row from the editor's left edge (covers the gutter region like
       VS Code), so the bar isn't pushed right by the line-number column. */
    position: absolute;
    left: 0;
    width: var(--sticky-width, 100%);
    min-width: 0;
    background: var(--code-bg);
    border-bottom: 1px solid var(--border-color);
    opacity: 0.94;
    cursor: pointer;
    font-family: 'SF Mono', Monaco, 'Cascadia Code', 'Segoe UI Mono', 'Roboto Mono', Consolas, 'Liberation Mono', monospace;
    font-size: 13px;
    line-height: 20.8px;
    pointer-events: auto;
}
.cm-viewer .sticky-line:hover {
    opacity: 1;
    background: var(--bg-tertiary);
}
/* Code text starts after the line-number gutter (--sticky-left), so it aligns with
   the content text and doesn't overlap the fixed line numbers. */
.cm-viewer .sticky-line-code {
    position: absolute;
    left: var(--sticky-left, 0px);
    top: 0;
    height: 100%;
    overflow: hidden;
    white-space: pre;
}
.cm-viewer .cm-lineWrapping .sticky-line-code {
    white-space: pre-wrap;
    word-break: break-all;
    overflow-wrap: break-word;
}
</style>
