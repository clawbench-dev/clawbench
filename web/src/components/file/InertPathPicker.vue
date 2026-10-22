<template>
  <BottomSheet
    :open="state.open"
    auto
    panel-class="inert-path-sheet"
    @close="handleClose"
  >
    <template #header>
      <Search :size="16" class="bs-header-icon" />
      <!-- The filename is its own visual element (a monospace chip) rather than
           being quoted inside the sentence, so the name reads as an identifier
           and matches the file-path chips used in chat. i18n-t keeps the
           surrounding verb translatable around the injected chip. -->
      <i18n-t keypath="file.search.inertHeading" tag="span" class="bs-header-title">
        <template #name>
          <span class="ip-name-chip">{{ state.query }}</span>
        </template>
      </i18n-t>
    </template>

    <div class="ip-body">
      <!-- The failing path, so the user can see WHAT was annotated (the panel
           is reached from a chip whose text may be truncated in a dense row). -->
      <div class="ip-source" :title="state.sourcePath">{{ state.sourcePath }}</div>

      <div class="ip-content">
        <div v-if="state.open && search.state.searching && search.state.results.length === 0" class="ip-loading">
          <PanelSkeleton variant="list" :rows="6" :meta="false" :label="t('file.search.searching')" />
        </div>

        <div v-else-if="search.state.results.length === 0" class="ip-empty">
          <FileQuestion :size="56" :stroke-width="1.25" class="ip-empty-icon" />
          <p class="ip-empty-text">
            <i18n-t keypath="file.search.inertNoResults" tag="span">
              <template #name>
                <span class="ip-name-chip">{{ state.query }}</span>
              </template>
            </i18n-t>
          </p>
          <p class="ip-empty-hint">{{ t('file.search.inertNoResultsHint') }}</p>
        </div>

        <div v-else class="ip-results">
          <button
            v-for="(file, idx) in search.state.results"
            :key="file.path"
            class="ip-item"
            :class="{ 'ip-item-active': listNav.activeIndex.value === idx }"
            :data-flat-index="idx"
            :title="file.path"
            @click="openCandidate(file.path)"
          >
            <FileIcon :path="file.path" :size="15" :is-dir="file.type === 'dir'" class="ip-item-icon" />
            <span class="ip-item-text">
              <span class="ip-item-name">{{ file.name }}</span>
              <span v-if="parentDirOf(file.path)" class="ip-item-dir">{{ parentDirOf(file.path) }}</span>
            </span>
          </button>

          <div v-if="search.state.truncated" class="ip-more">
            {{ t('file.search.resultCountPlus', { limit: search.getDisplayLimit() }) }}
          </div>
        </div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search, FileQuestion } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import { useFileSearch } from '@/composables/useFileSearch'
import { useListNav } from '@/composables/useListNav'
import { openFilePath } from '@/composables/useFilePathAnnotation'
import {
  inertPathPickerState as state,
  closeInertPathPicker,
  takeInertPathTarget,
} from '@/composables/useInertPathPicker'
import { parentDirOf } from '@/utils/contentSearchMark'
import { appLog } from '@/utils/appLog'

const { t } = useI18n()

/**
 * Bound DIRECTLY to the shared open flag, deliberately NOT through
 * `useTabDrawer`.
 *
 * An inert chip can live in chat, a task prompt / execution detail, or a forge
 * detail, so the picker is inherently cross-surface. `useTabDrawer(tabId)`
 * computes `effectiveOpen` as `currentTab === tabId && open` on narrow screens,
 * which would silently swallow the panel whenever the chip was clicked from any
 * tab other than the hard-coded one. App.vue closes the picker on a tab switch
 * instead (the equivalent of the drawer registry's tab scoping).
 */
const search = useFileSearch()

const listNav = useListNav({
  getCount: () => search.state.results.length,
  onConfirm: (index) => {
    const file = search.state.results[index]
    if (file) void openCandidate(file.path)
  },
  onActiveChange: (index) => {
    nextTick(() => {
      const el = document.querySelector<HTMLElement>(`.ip-item[data-flat-index="${index}"]`)
      el?.scrollIntoView({ block: 'nearest' })
    })
  },
})

/** Run the search for the current query, globally and exactly. */
function runSearch() {
  if (!state.query) return
  search.state.query = state.query
  search.state.scope = 'global'
  search.state.exact = true
  // `immediate` skips the composable's 300ms debounce: the query is not being
  // typed, it was derived from the clicked chip.
  search.startSearch('', true)
}

/** Open a candidate at the line the original chip pointed at, then close. */
async function openCandidate(path: string) {
  // Read the stashed target BEFORE closing — closeInertPathPicker clears it.
  const target = takeInertPathTarget()
  try {
    await openFilePath(path, target.lineStart, target.lineEnd, target.source, target.lineRanges)
  } catch (err) {
    appLog.w('InertPathPicker', 'failed to open candidate', err)
  }
}

function handleClose() {
  // Route every dismissal through the shared state so the watcher below owns
  // the teardown — otherwise a close initiated elsewhere (the click layer, a
  // tab switch) would leave the SSE walk running.
  closeInertPathPicker()
}

// The click layer only flips the shared `open` flag; the component owns the
// search lifecycle. Watching the flag (rather than doing the work in the click
// handler) keeps the search out of a code path that has no component scope, and
// makes EVERY close — BottomSheet, Escape, the click layer, a tab switch —
// release the in-flight search through one path.
watch(
  () => state.open,
  (isOpen) => {
    if (isOpen) {
      listNav.reset()
      runSearch()
    } else {
      search.cancelSearch()
      search.reset()
    }
  },
  { immediate: true },
)

// Reset the highlight whenever the result set is replaced, so Enter cannot
// confirm a row that belonged to the previous query.
watch(
  () => search.state.results,
  () => listNav.reset(),
)
</script>

<style scoped>
.ip-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  /* No horizontal padding: the result rows must reach the panel's left/right
     edges (full-bleed). The inset lives on the individual non-row elements
     (.ip-source, .ip-more, the empty/loading states) instead. */
  padding: var(--space-4) 0;
  min-height: 0;
  height: 100%;
}

.ip-source {
  padding: 0 var(--space-5);
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  color: var(--text-muted, #888);
  word-break: break-all;
}

/* The filename, as its own element: a monospace pill tinted like the inert
   path chip it was clicked from (see annotation-buttons.css). It sits inside
   the header title (and the empty-state sentence), so it must not inherit the
   title's semibold weight or primary colour — it reads as an identifier, not
   as prose. */
.ip-name-chip {
  font-family: var(--font-mono);
  font-weight: var(--font-weight-normal);
  font-size: var(--font-size-md);
  color: var(--text-primary, #1a1a1a);
  background: color-mix(in srgb, var(--text-muted, #999) 12%, transparent);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  word-break: break-all;
}

.ip-content {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

/* The loading state left-aligns like the real result rows; only the empty
   states centre. */
.ip-loading {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  padding: var(--space-4) 0;
}

.ip-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  /* The body has no horizontal padding now, so the centred states carry their
     own inset (kept equal to the row padding so the empty text lines up with
     the result names). */
  padding: var(--space-10) var(--space-5);
  text-align: center;
}

.ip-empty-icon {
  color: var(--text-muted, #999);
  opacity: var(--opacity-muted);
}

.ip-empty-text {
  margin: 0;
  color: var(--text-primary, #1a1a1a);
  font-size: var(--font-size-md);
}

.ip-empty-hint {
  margin: 0;
  color: var(--text-muted, #888);
  font-size: var(--font-size-sm);
}

.ip-results {
  display: flex;
  flex-direction: column;
}

.ip-item {
  display: flex;
  /* Top-align the icon: the row is two lines, so centring it would leave the
     icon floating between the name and the path instead of sitting on the
     identity line. */
  align-items: flex-start;
  gap: var(--space-3);
  width: 100%;
  /* Full-bleed: the hover fill spans the panel's full width with square
     corners. The inset is INTERNAL (icon/text indent) so the row still aligns
     with the source line above, but the fill itself reaches both edges. */
  padding: var(--space-3) var(--space-5);
  border: none;
  background: none;
  color: var(--text-primary, #1a1a1a);
  font-size: var(--font-size-md);
  text-align: left;
  cursor: pointer;
  border-radius: 0;
  transition: background var(--duration-fast);
}

.ip-item:hover,
.ip-item-active {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
}

.ip-item-icon {
  flex-shrink: 0;
  /* No offset needed: the icon's 15px box already centres on the name's line box
     (measured in Chromium — 0px delta; a 2px nudge pushed it visibly low). */
}

/* Name over path. The two lines get the full row width each, which matters
   because this panel is full-height — horizontal space is the scarce one. */
.ip-item-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.ip-item-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
}

/* Secondary line: muted and smaller, so the name stays the identity. Truncates
   from the LEFT (rtl) so the deepest directory — the part nearest the file, and
   therefore the most discriminating — stays visible. */
.ip-item-dir {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  direction: rtl;
  text-align: left;
  color: var(--text-muted, #888);
  font-size: var(--font-size-xs);
}

.ip-more {
  padding: var(--space-3) var(--space-5);
  color: var(--text-muted, #888);
  font-size: var(--font-size-sm);
}
</style>
