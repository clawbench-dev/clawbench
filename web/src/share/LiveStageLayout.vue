<template>
  <!-- Picture column + side panel, side by side.
       The panel is a SIBLING of the picture, never an overlay: opening it
       squeezes the stage, so nothing the presenter shows ends up hidden behind
       it. This file is the ONE implementation of that layout — the member's own
       share page and the host's view of a member's screen both render it, so the
       chat/transcript area cannot drift between the two ends of a call. -->
  <div ref="rootRef" class="live-shell">
    <div class="live-stage-col">
      <slot />
    </div>

    <!-- The same divider the app's split panes use (App.vue, the file manager):
         one shared implementation of the 1px line, the accent hover, the
         pointer capture that survives a fast drag off the line, and the touch
         grab band. Only the geometry of the panes on either side is ours. -->
    <SplitDivider
      v-if="sideOpen"
      class="live-side-divider"
      orientation="horizontal"
      @dragmove="onSideDrag"
    />

    <!-- `v-show` (not `v-if`) keeps the chat list, its scroll position and an
         unsent draft alive while the panel is closed. -->
    <aside
      v-show="sideOpen"
      class="live-side"
      :style="sideWidth > 0 ? { '--live-side-w': `${sideWidth}px` } : undefined"
      data-panel="side"
    >
      <div class="live-side-tabs" role="tablist">
        <button
          class="live-side-tab"
          :class="{ 'live-side-tab--on': activeTab === 'chat' }"
          type="button"
          role="tab"
          :aria-selected="activeTab === 'chat'"
          data-tab="chat"
          @click="selectTab('chat')"
        >
          {{ t('share.live.chat') }}
        </button>
        <button
          v-if="notesAvailable"
          class="live-side-tab"
          :class="{ 'live-side-tab--on': activeTab === 'notes' }"
          type="button"
          role="tab"
          :aria-selected="activeTab === 'notes'"
          data-tab="notes"
          @click="selectTab('notes')"
        >
          {{ t('share.live.notes') }}
        </button>
        <span class="live-side-gap" />
        <button
          class="live-side-close"
          type="button"
          :aria-label="t('common.close')"
          data-action="side-close"
          @click="emit('close')"
        >
          <X :size="16" />
        </button>
      </div>

      <div
        v-show="activeTab === 'chat'"
        class="live-chat-panel__body"
        role="tabpanel"
        data-panel="chat"
      >
        <slot name="chat" />
      </div>

      <div
        v-if="notesAvailable"
        v-show="activeTab === 'notes'"
        class="live-notes-body"
        role="tabpanel"
        data-panel="notes"
      >
        <slot name="notes" />
      </div>
    </aside>

    <!-- Page-level modals that must cover the panel too (the client-mode
         source picker). A child of the shell, not of the picture column. -->
    <slot name="overlay" />
  </div>
</template>

<script setup lang="ts">
/**
 * The live-share layout: the picture on the left, the 聊天 / 听记 panel on the
 * right, and the draggable boundary between them.
 *
 * Both ends of a call render this component — the member's own page
 * (`LiveShareView`) and the host looking at a member's shared screen
 * (`MemberScreenSurface`). Before it existed the host got a floating 280×320
 * chat box while the member got a full-height panel, so the same conversation
 * had two different shapes depending on which side you were on.
 *
 * The width is CSS px, not a ratio: a side panel is a width, not a share — a
 * wider window must not widen the chat. The stylesheet still caps it
 * (`max-width`), which is what keeps the picture usable when the window shrinks
 * after a wide drag.
 */
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { X } from 'lucide-vue-next'
import SplitDivider from '@/components/common/SplitDivider.vue'
import { designToCss } from '@/utils/uiScale'

const props = withDefaults(
  defineProps<{
    sideOpen: boolean
    /** Which pane the panel shows. The panel keeps both mounted. */
    activeTab: 'chat' | 'notes'
    /** Whether the 听记 tab exists at all (no recorder ⇒ no tab). */
    notesAvailable?: boolean
  }>(),
  { notesAvailable: false },
)

const emit = defineEmits<{
  (e: 'update:activeTab', tab: 'chat' | 'notes'): void
  (e: 'close'): void
  /** The picture column changed width — ink overlays must re-measure. */
  (e: 'resize'): void
}>()

const { t } = useI18n()

const rootRef = ref<HTMLElement | null>(null)

/** Panel width after a drag, in CSS px. `0` = the stylesheet's clamp decides. */
const sideWidth = ref(0)

/** Design-px bounds for the drag. */
const SIDE_MIN_DP = 260
/** A very wide window must not turn the chat into half the screen. */
const SIDE_MAX_DP = 560
/** The picture keeps at least this much, however far the divider is dragged. */
const STAGE_MIN_DP = 280

function clampSideWidth(px: number, shellPx: number): number {
  const min = designToCss(SIDE_MIN_DP)
  const max = Math.max(min, Math.min(designToCss(SIDE_MAX_DP), shellPx - designToCss(STAGE_MIN_DP)))
  return Math.min(max, Math.max(min, px))
}

/**
 * The divider rides the panel's left edge, so the distance from the pointer to
 * the shell's right edge IS the panel's new width — no need to know where the
 * drag started.
 */
function onSideDrag(clientX: number): void {
  const rect = rootRef.value?.getBoundingClientRect()
  if (!rect) return
  sideWidth.value = clampSideWidth(rect.right - clientX, rect.width)
}

function selectTab(tab: 'chat' | 'notes'): void {
  if (tab === 'notes' && !props.notesAvailable) return
  emit('update:activeTab', tab)
}

// Opening/closing/dragging changes the picture's WIDTH without a window resize,
// so `resize` never fires. Anyone painting on top of the picture (the ink layer)
// has to re-measure; the caller decides whether that means anything.
watch([() => props.sideOpen, sideWidth], () => {
  void nextTick(() => emit('resize'))
})

/** The shell element — fullscreen targets it, so it must not be duplicated. */
defineExpose({ rootEl: computed(() => rootRef.value) })
</script>

<style scoped>
/* Row shell: the picture column and the side panel share the width.
   `position: relative` makes it the containing block for a page-level modal
   (the client-mode source picker) that must cover the open panel too. */
.live-shell {
  position: relative;
  display: flex;
  height: 100%;
  background: var(--bg-primary, #0d1117);
}

/* The picture column. `min-width: 0` is what lets it actually shrink when the
   side panel opens: a flex item's default `min-width: auto` refuses to go below
   its content, and the video would push the panel off the screen instead.
   `position: relative` anchors the slot's own absolute chrome (the dock, the
   overlays) — both callers rely on it. */
.live-stage-col {
  position: relative;
  display: flex;
  flex-direction: column;
  flex: 1 1 0;
  min-width: 0;
  min-height: 0;
}

/* ── Side panel: 聊天 + 听记 ─────────────────────────────────────────────── */

/*
 * Full-height column beside the picture. A flex SIBLING of the stage, never an
 * absolutely positioned card: `position: absolute` is exactly what made the old
 * panels cover the presentation. Width is a range so a wide window does not
 * hand the chat half the screen, and a narrow one still gets a readable column.
 */
.live-side {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  /* `--live-side-w` is written by the divider drag; without a drag the range
     below decides, so a wide window does not hand the chat half the screen and
     a narrow one still gets a readable column. */
  width: var(--live-side-w, clamp(calc(280 * var(--dp)), 26vw, calc(400 * var(--dp))));
  /* A drag must never starve the picture: the cap is applied by CSS, so a
     window that shrinks AFTER a wide drag is handled without a resize listener
     (the inline width would otherwise win and the picture would collapse). */
  max-width: calc(100% - calc(280 * var(--dp)));
  min-height: 0;
  /* No border-left: the divider draws the boundary (and thickens on hover). */
  background: var(--bg-secondary, #161b22);
  padding-right: env(safe-area-inset-right, 0);
  padding-bottom: env(safe-area-inset-bottom, 0);
}

.live-side-tabs {
  display: flex;
  align-items: stretch;
  flex-shrink: 0;
  height: calc(38 * var(--dp));
  border-bottom: 1px solid var(--border-color, #30363d);
  background: var(--bg-secondary, #161b22);
}

.live-side-tab {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2, 4px);
  padding: 0 var(--space-5, 10px);
  border: none;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-secondary, #8b949e);
  font-size: var(--font-size-sm, 13px);
  cursor: pointer;
}

.live-side-tab:hover {
  color: var(--text-primary, #e6edf3);
}

.live-side-tab--on {
  color: var(--accent-color, #4a90d9);
  border-bottom-color: var(--accent-color, #4a90d9);
  font-weight: 600;
}

/* Pushes the close button to the far edge, so the tabs stay left-aligned like
   every other tab strip in the app. */
.live-side-gap {
  flex: 1 1 auto;
}

.live-side-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  align-self: center;
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  margin-right: var(--space-3, 6px);
  border: none;
  border-radius: var(--radius-sm, 4px);
  background: transparent;
  color: var(--text-secondary, #8b949e);
  cursor: pointer;
}

.live-side-close:hover {
  background: var(--bg-hover, rgba(255, 255, 255, 0.06));
  color: var(--text-primary, #e6edf3);
}

/*
 * The chat body carries no inset of its own: it is flat so it can sit on a
 * pane that already insets its content (the meeting panel does). The side
 * panel is not such a pane, so the inset is supplied here, otherwise the
 * bubbles, the input and the send button run into the border.
 */
.live-chat-panel__body {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
  padding: var(--space-4, 8px) var(--space-5, 10px);
}

.live-notes-body {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
  padding: var(--space-4, 8px) var(--space-5, 10px);
}

/*
 * Narrow windows (a phone, a split-screen tablet): a right-hand column would
 * leave the picture a sliver. Stack instead — the panel keeps a real height by
 * taking the bottom share, and the stage gives up height rather than width.
 * Still a squeeze, still no overlap.
 */
@media (max-width: 720px) {
  .live-shell {
    flex-direction: column;
  }

  /* A horizontal divider has nothing to separate once the panes are stacked. */
  .live-side-divider {
    display: none;
  }

  .live-side {
    width: 100%;
    /* The wide-layout cap would keep the stacked panel narrower than the
       screen; the drag that wrote `--live-side-w` cannot happen here anyway. */
    max-width: none;
    flex: 0 0 46%;
    border-top: 1px solid var(--border-color, #30363d);
  }
}
</style>
