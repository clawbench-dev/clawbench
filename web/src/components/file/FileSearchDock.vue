<template>
  <!-- The resident name-search dock. Shared by the file manager (its listing
       footer) and the standalone file picker (inside the attachment sheet), so
       the two cannot drift on look or on which toggles exist.

       The component owns the CHROME only: the query, the three toggles and
       whether they are on all live in the caller's search state, because the
       two surfaces derive them differently (global implies recursive, etc.). -->
  <div class="fs-nav-bottom" @keydown.esc.stop="emit('escape')">
    <div class="fs-input-row">
      <SearchInput
        ref="inputRef"
        :model-value="query"
        :placeholder="placeholder"
        @update:model-value="emit('update:query', $event)"
        @enter="emit('enter')"
        @down="emit('down')"
        @up="emit('up')"
      />
      <button
        class="fs-toggle-btn"
        :class="{ active: exact }"
        :title="t('file.search.exact')"
        @click="emit('toggle-exact')"
      >
        <WholeWord :size="15" />
      </button>
      <!-- Recursive + global are paired: global implies recursive, so they sit
           together (recursive to the left of global). -->
      <button
        class="fs-toggle-btn"
        :class="{ active: recursiveActive }"
        :disabled="recursiveDisabled"
        :title="t('file.search.recursive')"
        @click="emit('toggle-recursive')"
      >
        <FolderTree :size="15" />
      </button>
      <button
        class="fs-toggle-btn"
        :class="{ active: globalActive }"
        :title="t('file.search.scopeGlobal')"
        @click="emit('toggle-global')"
      >
        <Globe :size="15" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { FolderTree, Globe, WholeWord } from 'lucide-vue-next'
import SearchInput from '@/components/common/SearchInput.vue'

withDefaults(defineProps<{
  query: string
  placeholder?: string
  exact: boolean
  /** Whether the recursive toggle reads as ON (callers OR in "global implies recursive"). */
  recursiveActive: boolean
  /** Recursive is forced on by global scope — shown active but not operable. */
  recursiveDisabled?: boolean
  globalActive: boolean
}>(), {
  placeholder: '',
  recursiveDisabled: false,
})

const emit = defineEmits<{
  'update:query': [value: string]
  enter: []
  down: []
  up: []
  'toggle-exact': []
  'toggle-recursive': []
  'toggle-global': []
  /** Esc inside the dock. Propagation is stopped here so it never reaches a
   *  drawer/handler that would close the whole surface. */
  escape: []
}>()

const { t } = useI18n()
const inputRef = ref<{ focus?: () => void } | null>(null)

/** Focus + select, for callers that programmatically reveal the dock. */
function focus() {
  inputRef.value?.focus?.()
}

defineExpose({ focus })
</script>

<style scoped>
/* Same material as the top toolbar (.dir-toolbar, --bg-tertiary) so the two
   bars read as a matched pair framing the listing. In the file manager it sits
   directly under the list/grid, so the docked preview pane opens below it
   rather than pushing it to the bottom of the panel. */
.fs-nav-bottom {
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  border-top: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-tertiary, #f5f5f5);
  padding: calc(3 * var(--dp)) var(--space-5);
  gap: var(--space-1);
}

.fs-input-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.fs-input-row :deep(.search-pill) {
  flex: 1;
  min-width: 0;
  /* Flat field: the dock itself is the only surface, so the pill contributes
     neither a fill nor an outline of its own. SearchInput's shared defaults
     (--bg-primary fill + 1px border) would read as a nested box inside the
     bar. The vertical padding is only there to give the text room to breathe
     — with the default font the row lands at ~26px, matching the toggle
     buttons beside it. */
  background: transparent;
  border: none;
  padding: var(--space-2) var(--space-5);
}

/* Focus is deliberately invisible: the dock is a flat bar, so an accent outline
   or a tinted fill would reintroduce exactly the box this field just shed. The
   caret is the indicator. `:deep()` carries the parent's data-v onto
   .fs-input-row, which keeps this rule specific enough to beat the child's own
   .focused rule regardless of the order rollup emits the two style blocks in. */
.fs-input-row :deep(.search-pill.focused) {
  border: none;
  box-shadow: none;
  background: transparent;
}

.fs-toggle-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  flex-shrink: 0;
  transition: background var(--duration-base), color var(--duration-base);
  padding: 0;
}

@media (hover: hover) {
  .fs-toggle-btn:hover {
    background: var(--bg-hover, rgba(0, 0, 0, 0.06));
  }
}

.fs-toggle-btn.active {
  color: var(--accent-color, #4a90d9);
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 8%, transparent);
}

/* Forced-on (global scope implies recursive) — stays visibly active but is not
   user-operable. */
.fs-toggle-btn:disabled {
  cursor: default;
  opacity: var(--opacity-muted);
}

.fs-toggle-btn:disabled:hover {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 8%, transparent);
}
</style>
