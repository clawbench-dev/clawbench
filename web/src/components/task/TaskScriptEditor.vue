<template>
  <div class="task-script-editor" :class="{ 'is-disabled': disabled }">
    <div ref="hostRef" class="tse-host"></div>
  </div>
</template>

<script setup>
import { ref, shallowRef, watch, onMounted, onUnmounted } from 'vue'
import { EditorState, Compartment } from '@codemirror/state'
import { EditorView, keymap, lineNumbers, placeholder as cmPlaceholder } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { syntaxHighlighting } from '@codemirror/language'
import { buildLangExtension } from '@/utils/codeEditorLang'
import { codeHighlightStyle } from '@/utils/codeHighlightStyle'

/**
 * Editable CodeMirror surface for the scheduled task's gating script.
 *
 * Deliberately a small, self-contained editor rather than a reuse of
 * CodeMirrorViewer: that component is built around a file buffer (diff
 * markers, sticky scroll, quote bar, save/exit lifecycle) and pulling it in
 * here would drag a file-shaped lifecycle into a form field. What the two must
 * share is the syntax palette, which lives in `codeHighlightStyle`.
 */
const props = defineProps({
  modelValue: { type: String, default: '' },
  language: { type: String, default: 'shell' },
  placeholder: { type: String, default: '' },
  disabled: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])

const hostRef = ref(null)
// CodeMirror's state identity must not be proxied by Vue's ref(), or undo/redo
// transactions fail with a "doesn't start from the previous state" RangeError.
const view = shallowRef(null)
const langCompartment = new Compartment()
const editableCompartment = new Compartment()
// The placeholder is a localized string, so it must follow a locale switch
// while the form is open — a value captured at setup would stay in the old
// language until the component remounts.
const placeholderCompartment = new Compartment()
// Guards the modelValue watcher against re-dispatching the change it just made.
let applyingExternal = false

const editorTheme = EditorView.theme({
  '&': {
    backgroundColor: 'var(--code-bg)',
    color: 'var(--text-primary)',
    border: '1px solid var(--border-color)',
    borderRadius: 'var(--radius-sm)',
  },
  '&.cm-focused': { outline: 'none', borderColor: 'var(--accent-color)' },
  '.cm-content': {
    fontFamily: 'var(--font-mono)',
    fontSize: 'var(--font-size-sm)',
    lineHeight: '1.6',
    caretColor: 'var(--accent-color)',
    padding: 'var(--space-3) 0',
    minHeight: '88px',
  },
  '.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '1.6' },
  '.cm-gutters': {
    backgroundColor: 'var(--code-bg)',
    color: 'var(--text-muted)',
    border: 'none',
    borderRight: '1px solid var(--border-color)',
  },
  '.cm-lineNumbers .cm-gutterElement': {
    color: 'var(--text-muted)',
    opacity: 'var(--opacity-muted)',
    padding: '0 var(--space-2) 0 var(--space-3)',
  },
  '.cm-activeLine': { backgroundColor: 'color-mix(in srgb, var(--accent-color) 6%, transparent)' },
  '.cm-activeLineGutter': { backgroundColor: 'transparent' },
  '.cm-cursor, &.cm-focused .cm-cursor': { borderLeftColor: 'var(--accent-color)' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
    backgroundColor: 'color-mix(in srgb, var(--accent-color) 25%, transparent)',
  },
  '&.cm-editor.cm-readonly .cm-cursor, &.cm-editor.cm-readonly .cm-activeLine': { display: 'none' },
})

function placeholderExt() {
  return props.placeholder ? cmPlaceholder(props.placeholder) : []
}

function buildExtensions() {
  return [
    lineNumbers(),
    history(),
    editableCompartment.of([
      EditorState.readOnly.of(props.disabled),
      EditorView.editable.of(!props.disabled),
    ]),
    langCompartment.of([]),
    placeholderCompartment.of(placeholderExt()),
    syntaxHighlighting(codeHighlightStyle),
    keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
    editorTheme,
    EditorView.updateListener.of((update) => {
      if (!update.docChanged || applyingExternal) return
      emit('update:modelValue', update.state.doc.toString())
    }),
  ]
}

async function mountLang() {
  const ext = await buildLangExtension(props.language)
  if (view.value) view.value.dispatch({ effects: langCompartment.reconfigure(ext) })
}

onMounted(() => {
  view.value = new EditorView({
    parent: hostRef.value,
    state: EditorState.create({ doc: props.modelValue || '', extensions: buildExtensions() }),
  })
  mountLang()
})

onUnmounted(() => {
  view.value?.destroy()
  view.value = null
})

// Reflect external changes (form init / task switch) into the editor. Rebuilding
// the state clears the undo history, which is correct when the buffer was
// replaced wholesale rather than edited.
watch(() => props.modelValue, (next) => {
  const editor = view.value
  if (!editor) return
  const value = next || ''
  if (editor.state.doc.toString() === value) return
  applyingExternal = true
  editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: value } })
  applyingExternal = false
})

watch(() => props.language, mountLang)

watch(() => props.placeholder, (placeholder) => {
  view.value?.dispatch({ effects: placeholderCompartment.reconfigure(placeholder ? cmPlaceholder(placeholder) : []) })
})

watch(() => props.disabled, (disabled) => {
  const editor = view.value
  if (!editor) return
  editor.dispatch({
    effects: editableCompartment.reconfigure([
      EditorState.readOnly.of(disabled),
      EditorView.editable.of(!disabled),
    ]),
  })
})

// Exposed for tests (and for any caller that needs to drive the buffer
// programmatically). Mirrors CodeMirrorViewer's getView.
defineExpose({ getView: () => view.value, getValue: () => view.value?.state.doc.toString() ?? '' })
</script>

<style scoped>
.task-script-editor {
  display: flex;
  flex-direction: column;
}
.task-script-editor.is-disabled {
  opacity: var(--opacity-muted);
}
.tse-host {
  min-width: 0;
}
</style>
