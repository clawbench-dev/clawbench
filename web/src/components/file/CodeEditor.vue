<template>
  <div class="code-editor-wrapper">
    <Codemirror
      v-model="code"
      :extensions="extensions"
      :autofocus="true"
      :style="{ height: '100%' }"
      placeholder=""
    />
    <div class="code-editor-actions">
      <span class="code-editor-status">{{ t('file.editor.dirty') }}</span>
      <button class="editor-btn" :disabled="saving" @click="emit('cancel')">{{ t('file.editor.cancel') }}</button>
      <button class="editor-btn primary" :disabled="saving" @click="emit('save', code)">
        {{ saving ? t('file.editor.saving') : t('file.editor.save') }}
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Codemirror } from 'vue-codemirror'
import { EditorView, lineNumbers } from '@codemirror/view'
import { history } from '@codemirror/commands'
import { oneDark } from '@codemirror/theme-one-dark'
import { buildLangExtension } from '@/utils/codeEditorLang'

const props = defineProps({
    file: Object,
    content: { type: String, default: '' },
    language: { type: String, default: 'plaintext' },
    wordWrap: { type: Boolean, default: false },
    saving: { type: Boolean, default: false },
})
const emit = defineEmits(['save', 'cancel'])

const { t } = useI18n()
const code = ref(props.content || '')

const isDark = computed(() => document.documentElement.getAttribute('data-theme') === 'dark')

const extensions = computed(() => {
    const exts = [history(), lineNumbers(), buildLangExtension(props.language)]
    if (props.wordWrap) exts.push(EditorView.lineWrapping)
    if (isDark.value) exts.push(oneDark)
    return exts
})

watch(() => props.content, (c) => {
    code.value = c || ''
})

defineExpose({ getValue: () => code.value })
</script>

<style scoped>
.code-editor-wrapper {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
    background: var(--code-bg);
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
