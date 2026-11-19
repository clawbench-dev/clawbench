<template>
  <div class="md-editor">
    <div ref="hostRef" class="md-editor-host markdown-body" :data-file-path="file?.path || ''"></div>

    <div class="code-editor-actions">
      <span class="code-editor-status">
        <span class="dirty-dot" v-if="dirty"></span>
      </span>
      <button class="editor-btn icon-btn" :disabled="saving" @mousedown.prevent @click="handleUndo" :title="t('file.editor.undo')">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M3 10h13a4 4 0 0 1 0 8H9"/><path d="M3 10l5-5M3 10l5 5"/></svg>
      </button>
      <button class="editor-btn icon-btn" :disabled="saving" @mousedown.prevent @click="handleRedo" :title="t('file.editor.redo')">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M21 10H8a4 4 0 0 0 0 8h7"/><path d="M21 10l-5-5M21 10l-5 5"/></svg>
      </button>
      <button v-if="dirty" class="editor-btn icon-btn primary" :disabled="saving" @mousedown.prevent @click="emitSave()" :title="t('file.editor.save')">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>
      </button>
      <button class="editor-btn icon-btn" @mousedown.prevent @click="handleExit" :title="t('file.editor.exitEdit')">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M18 6L6 18M6 6l12 12"/></svg>
      </button>
    </div>
  </div>
</template>

<script setup>
/**
 * WYSIWYG markdown editing (Milkdown), the second way to edit a markdown file.
 *
 * Source editing stays on CodeMirror; this component is only mounted while the
 * user picked the visual mode, so it can be loaded lazily (FileViewer imports
 * it through defineAsyncComponent) — the editor stack is ~200KB and has no
 * business in the first-screen bundle.
 *
 * The component deliberately mirrors CodeMirrorViewer's contract
 * (handleExit / isDirty / save / saveAndExit / exitEdit) so FileViewer's dirty
 * guard, back gesture and save flow keep working without branching per editor.
 *
 * Markdown stays the source of truth: the document is parsed on mount and
 * serialized back on save, which means the file is rewritten in this editor's
 * normal form. Table column padding and list markers can shift; that is why
 * FileViewer refuses files whose structure would be destroyed (frontmatter).
 */
import { ref, shallowRef, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Editor, rootCtx, defaultValueCtx, editorViewOptionsCtx, prosePluginsCtx } from '@milkdown/kit/core'
import { commonmark } from '@milkdown/kit/preset/commonmark'
import { gfm } from '@milkdown/kit/preset/gfm'
import { history, undoCommand, redoCommand } from '@milkdown/kit/plugin/history'
import { listener, listenerCtx } from '@milkdown/kit/plugin/listener'
import { callCommand, getMarkdown, $view, replaceAll } from '@milkdown/kit/utils'
import { imageSchema } from '@milkdown/kit/preset/commonmark'
import { resolveMarkdownImageSrc } from '@/utils/markdownImageSrc.ts'
import { createMathPlugins } from './milkdownMath.ts'
import { createCodeBlockView, createHighlightPlugin } from './milkdownCodeBlock.ts'
import { createHtmlBlockView } from './milkdownHtmlBlock.ts'
import { createHeadingAnchorPlugin } from './milkdownHeadingAnchors.ts'
import { useDialog } from '@/composables/useDialog.ts'
import { appLog } from '@/utils/appLog.ts'

const props = defineProps({
    /** Original file text — the round-trip baseline for the dirty check. */
    content: { type: String, default: '' },
    saving: { type: Boolean, default: false },
    /** Open file; only `path` is used (resolving relative image srcs). */
    file: { type: Object, default: null },
})
const emit = defineEmits(['save', 'saveAndExit', 'exitEdit'])

const { t } = useI18n()
const dialog = useDialog()

const hostRef = ref(null)
const editor = shallowRef(null)
const dirty = ref(false)
/** Last persisted text. Saving rewrites props.content, which advances this. */
const baseline = ref(props.content || '')
/**
 * The exact string handed to the save flow. A successful save comes back as
 * `props.content`; matching it lets the baseline advance even when the user
 * kept typing while the write was in flight.
 */
let pendingSave = null

/** Current document as markdown. Synchronous: Milkdown's serializer runs here. */
function getValue() {
    if (!editor.value) return baseline.value
    try {
        return editor.value.action(getMarkdown())
    } catch {
        return baseline.value
    }
}

function isDirty() {
    // A serializer failure must never read as "clean": that would let the exit
    // path discard edits without a prompt. Treat it as dirty instead.
    try {
        if (!editor.value) return false
        return editor.value.action(getMarkdown()) !== baseline.value
    } catch {
        return true
    }
}

function handleUndo() {
    editor.value?.action(callCommand(undoCommand.key))
}
function handleRedo() {
    editor.value?.action(callCommand(redoCommand.key))
}

/**
 * Image node view: markdown stores `screenshots/a.png`, the browser needs
 * `/api/fs/raw/docs/screenshots/a.png`. The document keeps the original src, so
 * only the rendered <img> is rewritten (and re-rewritten on every node update).
 */
// One cache-busting stamp per editor session: a fresh Date.now() on every node
// update would give the <img> a new URL on each keystroke and refetch it.
const imageStamp = Date.now()

const imageView = $view(imageSchema.node, () => (node) => {
    const img = document.createElement('img')
    img.className = 'md-editor-image'
    const apply = (n) => {
        img.src = resolveMarkdownImageSrc(n.attrs.src, props.file?.path, imageStamp)
        img.alt = n.attrs.alt || ''
        if (n.attrs.title) img.title = n.attrs.title
        else img.removeAttribute('title')
    }
    apply(node)
    return {
        dom: img,
        update: (next) => {
            if (next.type.name !== 'image') return false
            apply(next)
            return true
        },
        // Setting src from the node view is our own write, not a document edit.
        ignoreMutation: () => true,
    }
})

// Formulas are atoms; double-clicking one edits its LaTeX in a prompt.
const mathPlugins = createMathPlugins((value) =>
    dialog.prompt(t('file.editor.editMath'), { value, title: t('file.editor.editMath') }),
)

// Raw HTML blocks (a README's centred hero image, an author's <div> callout)
// are rendered in place instead of showing their tags. The atom keeps the
// source, so a save writes back exactly what was there; double-click edits it.
const htmlBlockView = createHtmlBlockView({
    filePath: () => props.file?.path,
    stamp: imageStamp,
    edit: (value) => dialog.prompt(t('file.editor.editHtml'), {
        value,
        title: t('file.editor.editHtml'),
        enterInsertsNewline: true,
    }),
})
const codeBlockView = createCodeBlockView()
const highlightPlugin = createHighlightPlugin()
const headingAnchorPlugin = createHeadingAnchorPlugin()

function emitSave() {
    const value = getValue()
    pendingSave = value
    emit('save', value)
}

// Ctrl/Cmd+S mirrors the save button. Registered on the host (not window) so it
// only fires while the editor owns focus.
function handleKeydown(e) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
        e.preventDefault()
        // isDirty() reads the live document: the listener that drives the dot is
        // debounced, so a keystroke followed immediately by Ctrl+S would still
        // look clean for a moment.
        if (isDirty() && !props.saving) emitSave()
    }
}

// Exit edit mode with the same save / discard / cancel confirmation the source
// editor uses. Returns true when editing really ends, false when the user
// cancels — FileViewer chains navigation actions on that answer.
async function handleExit() {
    if (!isDirty()) {
        emit('exitEdit')
        return true
    }
    const choice = await dialog.confirm(t('file.editor.confirmExit'), {
        confirmText: t('file.editor.save'),
        cancelText: t('common.cancel'),
        extraText: t('file.editor.dontSave'),
        extraPrimedText: t('common.confirm'),
        onExtraAction: () => {},
    })
    if (choice === true) {
        pendingSave = getValue()
        emit('saveAndExit', pendingSave)
        return true
    }
    if (choice === null) {
        emit('exitEdit')
        return true
    }
    return false
}

let mounted = true

onMounted(async () => {
    const host = hostRef.value
    if (!host) return
    host.addEventListener('keydown', handleKeydown)
    try {
        editor.value = await Editor.make()
            .config((ctx) => {
                ctx.set(rootCtx, host)
                ctx.set(defaultValueCtx, baseline.value)
                ctx.set(editorViewOptionsCtx, { editable: () => true })
                ctx.get(listenerCtx).markdownUpdated((_ctx, markdown) => {
                    dirty.value = markdown !== baseline.value
                })
                // ProseMirror plugins (not Milkdown plugins) go through this ctx.
                ctx.update(prosePluginsCtx, (plugins) => [...plugins, highlightPlugin, headingAnchorPlugin])
            })
            .use(commonmark)
            .use(gfm)
            .use(history)
            .use(listener)
            .use(imageView)
            .use(mathPlugins)
            .use(htmlBlockView)
            .use(codeBlockView)
            .create()
        if (!mounted) {
            // Unmounted while the async create was in flight.
            await editor.value.destroy()
            editor.value = null
        } else {
            // The baseline is the editor's OWN serialization of what it parsed,
            // not the raw file text: Milkdown normalizes some syntax (table
            // column padding, list markers), so comparing against the file would
            // mark every such document dirty the moment it opens and prompt
            // "save changes?" before the user touched anything.
            baseline.value = getValue()
            dirty.value = false
        }
    } catch (err) {
        // Creating the editor must never leave the user stuck in a blank pane:
        // log it and hand control back so the viewer can fall back to source.
        appLog.e('MilkdownEditor', 'failed to create editor', err)
        emit('exitEdit')
    }
})

onBeforeUnmount(() => {
    mounted = false
    hostRef.value?.removeEventListener('keydown', handleKeydown)
    const ed = editor.value
    editor.value = null
    if (ed) void ed.destroy()
})

/**
 * Content changed underneath us. Three cases, in order:
 *
 * 1. Our own save came back (or nothing actually changed): advance the baseline.
 * 2. The user typed while the save was in flight: `props.content` is the text we
 *    handed to the write, so advance the baseline to it and recompute dirty
 *    against the live document (it stays dirty — those newer edits are unsaved).
 * 3. An external change (file rewritten on disk, or the user accepted the
 *    "reload?" prompt): adopt it. When the buffer was dirty the prompt already
 *    asked; when it was clean the source editor replaces its document the same
 *    way. Ignoring it would let the next save silently overwrite the new text.
 */
let applyingExternal = false
watch(() => props.content, (next) => {
    if (applyingExternal || !editor.value) return
    const value = next || ''
    if (value === getValue()) {
        pendingSave = null
        baseline.value = value
        dirty.value = false
        return
    }
    if (pendingSave !== null && value === pendingSave) {
        pendingSave = null
        baseline.value = value
        dirty.value = getValue() !== baseline.value
        return
    }
    applyingExternal = true
    try {
        editor.value.action(replaceAll(value, true))
        pendingSave = null
        baseline.value = getValue()
        dirty.value = false
    } catch (err) {
        // Parsing the replacement failed: keep the current buffer (dirty) rather
        // than leaving a half-applied document.
        appLog.e('MilkdownEditor', 'failed to apply external content', err)
        dirty.value = true
    } finally {
        applyingExternal = false
    }
})

defineExpose({ getValue, isDirty, handleExit })
</script>

<style scoped>
.md-editor {
    position: relative;
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    overflow: hidden;
}
/* The host doubles as the reading column (.markdown-body supplies the width,
   padding and typography); it owns the scroll so the toolbar stays put. */
.md-editor-host {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
}
.code-editor-actions {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    justify-content: flex-end;
    padding: var(--space-3) var(--space-6);
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
    width: calc(8 * var(--dp));
    height: calc(8 * var(--dp));
    border-radius: 50%;
    background: var(--accent-color);
    flex-shrink: 0;
    transition: opacity var(--duration-slow);
}
.editor-btn.icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: calc(5 * var(--dp)) var(--space-4);
    line-height: 1;
}
.editor-btn {
    padding: calc(5 * var(--dp)) calc(14 * var(--dp));
    border: 1px solid var(--border-color);
    border-radius: var(--radius-lg);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--font-size-sm);
    font-weight: var(--font-weight-medium);
    cursor: pointer;
}
@media (hover: hover) {
  .editor-btn:hover { border-color: var(--accent-color); color: var(--accent-color); }
  .editor-btn.primary:hover { filter: brightness(1.1); }
}
.editor-btn.primary { background: var(--accent-color); border-color: var(--accent-color); color: #fff; }
.editor-btn:disabled { opacity: var(--opacity-muted); cursor: not-allowed; pointer-events: none; }
</style>

<style>
/* ProseMirror's editable surface is created at runtime, so scoped rules cannot
   reach it — these must stay global (same reason CodeMirrorViewer keeps its
   .cm-* rules in an unscoped block). Typography comes from .markdown-body. */
.md-editor-host .ProseMirror {
    outline: none;
    min-height: 100%;
    padding-bottom: calc(80 * var(--dp));
    white-space: pre-wrap;
    overflow-wrap: break-word;
}
/* Milkdown wraps the editable surface in a `.milkdown` div; without a height it
   collapses to content height and the empty area below the last block cannot be
   clicked to place the caret. */
.md-editor-host .milkdown {
    min-height: 100%;
}
.md-editor-host .ProseMirror > *:first-child {
    margin-top: 0;
}
.md-editor-host .ProseMirror-selectednode {
    outline: 2px solid var(--accent-color);
}
.md-editor-host .md-editor-image {
    max-width: 100%;
    height: auto;
    border-radius: var(--radius-md);
    vertical-align: bottom;
}
/* Raw HTML: a complete element owns its line, a lone void element stays in the
   text flow. The block is rendered, not editable — double-click edits source. */
.md-editor-host .md-html-block {
    display: block;
    margin: 0.4em 0;
    /* The editable surface inherits pre-wrap; the authored source's newlines
       and indentation must not turn into visible gaps inside rendered HTML. */
    white-space: normal;
}
.md-editor-host .md-html-block img {
    max-width: 100%;
    height: auto;
}
/* Rendered formulas (atoms): inline ones sit in the text flow, display ones
   take a centred block like the preview's `.katex-display`. Double-click edits
   the source, so give them a pointer cursor and no text selection. */
.md-editor-host .md-math-inline,
.md-editor-host .md-math-block {
    cursor: pointer;
    user-select: none;
}
.md-editor-host .md-math-block {
    margin: 1em 0;
    overflow-x: auto;
    overflow-y: hidden;
}
/* Mermaid preview sits above the still-editable fenced source. */
.md-editor-host .md-code-block {
    margin: 1em 0;
}
.md-editor-host .md-code-preview {
    margin-bottom: var(--space-4);
}
.md-editor-host .md-code-preview .mermaid {
    margin: 0;
}
</style>
