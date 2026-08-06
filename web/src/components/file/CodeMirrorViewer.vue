<template>
  <div class="cm-viewer" :class="{ 'is-editable': editable, 'cm-readonly': !editable }">
    <div ref="editorHost" class="cm-host"></div>
    <div v-if="editable" class="code-editor-actions">
      <span class="code-editor-status">{{ t('file.editor.dirty') }}</span>
      <button class="editor-btn" :disabled="saving" @click="emit('cancel')">{{ t('file.editor.cancel') }}</button>
      <button class="editor-btn primary" :disabled="saving" @click="emit('save', getValue())">
        {{ saving ? t('file.editor.saving') : t('file.editor.save') }}
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Compartment, EditorState, RangeSetBuilder } from '@codemirror/state'
import { EditorView, lineNumbers, Decoration, gutter, GutterMarker, keymap } from '@codemirror/view'
import { defaultKeymap, historyKeymap, history } from '@codemirror/commands'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { tags } from '@lezer/highlight'
import { buildLangExtension } from '@/utils/codeEditorLang'
import { diffMarkers, openDiffDrawer } from '@/composables/useMarkdownDiff.ts'
import { flashRanges, flashType } from '@/composables/useFileRefresh.ts'
import { copyText } from '@/utils/clipboard.ts'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'
import { buildOverlayDecorations } from '@/utils/codeMirrorOverlay.ts'

const props = defineProps({
    file: Object,
    content: { type: String, default: '' },
    language: { type: String, default: 'plaintext' },
    wordWrap: { type: Boolean, default: false },
    showLineNumbers: { type: Boolean, default: true },
    /** false = read-only browse (default); true = source editing */
    editable: { type: Boolean, default: false },
    saving: { type: Boolean, default: false },
})
const emit = defineEmits(['save', 'cancel'])

const { t } = useI18n()
const editorHost = ref(null)
const view = ref(null)
const diffLineMap = ref(new Map())
const quoteQuestion = useQuoteQuestion()

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
let flashTimer = null
function scrollToLine(line, lineEnd) {
    const editor = view.value
    if (!editor) return
    const target = Math.min(Math.max(1, line || 1), editor.state.doc.lines)
    const pos = editor.state.doc.line(target).from
    editor.dispatch({ effects: EditorView.scrollIntoView(pos, { y: 'center' }) })
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
    scrollToLine(d.line, d.lineEnd)
}

// ─── Assemble extensions ───
// All extensions are passed directly to EditorState at creation — NO basicSetup,
// NO vue-codemirror. Toggleable parts live in top-level Compartments, which
// CodeMirror reconfigures reliably (verified against raw CodeMirror).
function buildAllExtensions() {
    return [
        readonlyCompartment.of(props.editable ? [] : [EditorState.readOnly.of(true)]),
        langCompartment.of(buildLangExtension(props.language)),
        lineNumbersCompartment.of(props.showLineNumbers ? [lineNumbers()] : []),
        wrapCompartment.of(props.wordWrap ? [EditorView.lineWrapping] : []),
        codeMirrorTheme,
        syntaxHighlighting(codeHighlightStyle),
        history(),
        keymap.of([...defaultKeymap, ...historyKeymap]),
        interactionExtension,
        selectionExtension,
        jumpFlashCompartment.of([]),
        overlayCompartment.of([]),
    ]
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
    recomputeOverlay()
    window.addEventListener('cm-scroll-to-line', onScrollToLine)
})

onUnmounted(() => {
    window.removeEventListener('cm-scroll-to-line', onScrollToLine)
    if (flashTimer) clearTimeout(flashTimer)
    if (selDebounceTimer) clearTimeout(selDebounceTimer)
    view.value?.destroy()
    view.value = null
})

// Reconfigure toggleable compartments when their props change.
watch([() => props.editable], () => {
    reconfigure(readonlyCompartment, props.editable ? [] : [EditorState.readOnly.of(true)])
    updateInputMode()
})
watch([() => props.showLineNumbers], () => reconfigure(lineNumbersCompartment, props.showLineNumbers ? [lineNumbers()] : []))
watch([() => props.wordWrap], () => reconfigure(wrapCompartment, props.wordWrap ? [EditorView.lineWrapping] : []))
watch([() => props.language], () => reconfigure(langCompartment, buildLangExtension(props.language)))

// Rebuild overlay decorations whenever diff/flash/path inputs change.
watch([diffMarkers, flashRanges, flashType, () => props.content], recomputeOverlay)

// Update the document when the file content changes (file switch / refresh).
watch(() => props.content, (c) => {
    const editor = view.value
    if (!editor) return
    const next = c || ''
    if (editor.state.doc.toString() !== next) {
        editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: next } })
    }
})

function getValue() {
    return view.value ? view.value.state.doc.toString() : (props.content || '')
}

defineExpose({ getValue, scrollToLine, getView: () => view.value })
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
    font-size: 12px;
    color: var(--text-muted);
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
</style>
