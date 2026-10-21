<template>
  <div class="search-bar" :class="{ 'is-open': open }">
    <div class="cm-search">
      <span class="search-input-wrap">
        <input
          ref="inputRef"
          name="search"
          class="cm-textfield"
          :value="modelValue"
          :placeholder="labels.find"
          spellcheck="false"
          autocomplete="off"
          @input="emit('input', ($event.target as HTMLInputElement).value)"
          @keydown="onKeydown"
        />
        <span class="search-input-actions">
          <button
            name="case"
            class="search-opt-btn"
            :class="{ active: localCase }"
            :title="labels.matchCase"
            :aria-label="labels.matchCase"
            :aria-pressed="localCase"
            @click="toggle('case')"
          >Aa</button>
          <button
            name="word"
            class="search-opt-btn search-opt-btn-word"
            :class="{ active: localWord }"
            :title="labels.byWord"
            :aria-label="labels.byWord"
            :aria-pressed="localWord"
            @click="toggle('word')"
          >ab</button>
          <button
            name="regexp"
            class="search-opt-btn"
            :class="{ active: localRegexp }"
            :title="labels.regexp"
            :aria-label="labels.regexp"
            :aria-pressed="localRegexp"
            @click="toggle('regexp')"
          >.*</button>
        </span>
      </span>
      <button name="prev" class="cm-button" :disabled="!canNav" :title="labels.previous" aria-label="Previous match" @click="emit('prev')"></button>
      <button name="next" class="cm-button" :disabled="!canNav" :title="labels.next" aria-label="Next match" @click="emit('next')"></button>
      <span v-if="matchText" class="cm-search-match-info">{{ matchText }}</span>
      <button name="close" class="cm-button" :title="labels.close" aria-label="Close search" @click="emit('close')">
        <svg class="search-close-icon" width="12" height="12" viewBox="0 0 10 10" fill="none" aria-hidden="true">
          <path d="M1 1l8 8M9 1L1 9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
      </button>
      <span v-if="showReplace" class="cm-search-replace">
        <input
          name="replace"
          class="cm-textfield"
          :value="replaceText"
          :placeholder="labels.replace"
          spellcheck="false"
          autocomplete="off"
          @input="emit('replace-input', ($event.target as HTMLInputElement).value)"
          @keydown="onReplaceKeydown"
        />
        <button name="replace" class="cm-button" @click="emit('replace')">{{ labels.replaceAction }}</button>
        <button name="replaceAll" class="cm-button" @click="emit('replace-all')">{{ labels.replaceAll }}</button>
      </span>
    </div>
  </div>
</template>

<script lang="ts">
export interface SearchBarLabels {
  find: string
  replace: string
  previous: string
  next: string
  matchCase: string
  regexp: string
  byWord: string
  replaceAction: string
  replaceAll: string
  close: string
}
</script>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'

const props = withDefaults(defineProps<{
  open: boolean
  modelValue?: string
  canNav: boolean
  matchText?: string
  labels?: Partial<SearchBarLabels>
  caseSensitive?: boolean
  regexp?: boolean
  wholeWord?: boolean
  showReplace?: boolean
  replaceText?: string
}>(), {
  modelValue: '',
  matchText: '',
  labels: () => ({}),
  caseSensitive: false,
  regexp: false,
  wholeWord: false,
  showReplace: false,
  replaceText: '',
})

// English fallbacks (used when a host omits some label; hosts pass localized
// labels — SearchBar itself is i18n-agnostic so the CodeMirror plugin can
// render it without a Vue i18n instance).
const labels = computed<SearchBarLabels>(() => ({
  find: props.labels.find || 'Find',
  replace: props.labels.replace || 'Replace',
  previous: props.labels.previous || 'Previous match',
  next: props.labels.next || 'Next match',
  matchCase: props.labels.matchCase || 'Match case',
  regexp: props.labels.regexp || 'Regexp',
  byWord: props.labels.byWord || 'By word',
  replaceAction: props.labels.replaceAction || 'Replace',
  replaceAll: props.labels.replaceAll || 'Replace all',
  close: props.labels.close || 'Close',
}))

const emit = defineEmits<{
  (e: 'input', value: string): void
  (e: 'replace-input', value: string): void
  (e: 'prev'): void
  (e: 'next'): void
  (e: 'close'): void
  (e: 'enter', shiftKey: boolean): void
  (e: 'escape'): void
  (e: 'case-change', checked: boolean): void
  (e: 'regexp-change', checked: boolean): void
  (e: 'word-change', checked: boolean): void
  (e: 'replace'): void
  (e: 'replace-all'): void
}>()

const inputRef = ref<HTMLInputElement | null>(null)

// Local mirrors of the option props. Clicking toggles these immediately and
// emits the new value — relying on `!prop` directly would read a stale prop
// when the user double-clicks faster than the parent re-renders, wedging the
// toggle. The props stay the source of truth; the mirrors resync via watch.
const localCase = ref(props.caseSensitive)
const localRegexp = ref(props.regexp)
const localWord = ref(props.wholeWord)

watch(() => props.caseSensitive, (v) => { localCase.value = v })
watch(() => props.regexp, (v) => { localRegexp.value = v })
watch(() => props.wholeWord, (v) => { localWord.value = v })

function toggle(which: 'case' | 'regexp' | 'word') {
  if (which === 'case') {
    localCase.value = !localCase.value
    emit('case-change', localCase.value)
  } else if (which === 'regexp') {
    localRegexp.value = !localRegexp.value
    emit('regexp-change', localRegexp.value)
  } else {
    localWord.value = !localWord.value
    emit('word-change', localWord.value)
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    e.preventDefault()
    emit('enter', e.shiftKey)
  } else if (e.key === 'Escape') {
    e.preventDefault()
    emit('escape')
  }
}

function onReplaceKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    e.preventDefault()
    emit('replace')
  } else if (e.key === 'Escape') {
    e.preventDefault()
    emit('escape')
  }
}

watch(
  () => props.open,
  (val) => {
    if (val) {
      // Wait a tick so v-show makes the input visible/focusable. The open state
      // is re-checked inside the callback: closing within that frame (Escape,
      // the ✕ button) must not have focus yanked back into a hidden bar.
      requestAnimationFrame(() => {
        if (!props.open) return
        inputRef.value?.focus()
        inputRef.value?.select()
      })
    }
  },
)

defineExpose({
  focus() {
    inputRef.value?.focus()
    inputRef.value?.select()
  },
})
</script>

<style scoped>
/* Host-independent container. The controls' shared styles come from the
   global search-bar.css import below (NOT scoped — both CodeMirror's panel
   and this markdown bar must receive them). */
.search-bar {
  display: none;
}
.search-bar.is-open {
  display: block;
}
</style>

<style>
@import '@/assets/search-bar.css';
</style>
