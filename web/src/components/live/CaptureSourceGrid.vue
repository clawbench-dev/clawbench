<template>
  <div class="capture-sources" :class="{ 'capture-sources--compact': compact }">
    <!-- "Share nothing" is a picture choice like any other, so it is a tile in
         the same grid rather than a checkbox above it: the host compares
         "no picture" against the actual screens, side by side. -->
    <button
      v-if="allowOff"
      class="capture-source capture-source--off"
      :class="{ 'capture-source--on': selectedId === '' }"
      type="button"
      :disabled="disabled"
      data-source="__off__"
      @click="emit('select', '')"
    >
      <span class="capture-source__thumb">
        <MonitorOff :size="20" />
        <span v-if="selectedId === ''" class="capture-source__check"><Check :size="10" /></span>
      </span>
      <span class="capture-source__name">{{ t('meeting.create.noShare') }}</span>
    </button>

    <button
      v-for="src in sources"
      :key="src.id"
      class="capture-source"
      :class="{ 'capture-source--on': src.id === selectedId }"
      type="button"
      :disabled="disabled"
      :data-source="src.id"
      @click="emit('select', src.id)"
    >
      <!-- The still frame is the only thing that tells two windows of the same
           app apart, so a host that can supply one always gets it shown. A host
           that cannot (Wayland placeholders, no icon) keeps the tile's shape
           with an icon instead. -->
      <span class="capture-source__thumb">
        <img v-if="src.thumbnail" :src="src.thumbnail" :alt="src.name" />
        <Monitor v-else :size="20" />
        <span v-if="src.id === selectedId" class="capture-source__check"><Check :size="10" /></span>
        <!-- Screen vs window is what decides whether remote control can point
             at the right place, and the thumbnail alone cannot say it. It sits
             on the frame so the tile needs no third text line. -->
        <span class="capture-source__kind">
          {{ src.kind === 'window' ? t('meeting.create.window') : t('meeting.create.screen') }}
        </span>
      </span>
      <span class="capture-source__name" :title="src.name">{{ src.name }}</span>
    </button>

    <!-- A host that has not handed over a list yet: one empty frame that asks
         for it. Clicking re-runs the probe, which is what makes the OS raise
         its own permission dialog (macOS screen recording, Wayland portal). -->
    <button
      v-if="pickLabel"
      class="capture-source capture-source--pick"
      type="button"
      :disabled="pickBusy || disabled"
      data-source="__pick__"
      @click="emit('pick')"
    >
      <span class="capture-source__thumb">
        <span v-if="pickBusy" class="capture-source__spinner" />
        <Plus v-else :size="20" />
      </span>
      <span class="capture-source__name">{{ pickLabel }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
/**
 * The list of screens and windows a host can share.
 *
 * One component because the SAME choice is asked twice — when creating a
 * meeting and when swapping the source mid-meeting — and the two copies had
 * already drifted: the create page showed a still frame per source, the swap
 * list showed bare names, so the later pick was made blind.
 */
import { Check, Monitor, MonitorOff, Plus } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import type { CaptureSource } from '@/composables/useLiveShare'

withDefaults(
  defineProps<{
    sources: CaptureSource[]
    /** Empty means nothing is selected (yet). */
    selectedId: string
    /** Tiles are not clickable while a switch is in flight. */
    disabled?: boolean
    /** Narrower tiles, for the meeting panel. */
    compact?: boolean
    /**
     * Offer a "share nothing" tile first.
     *
     * Only the create page asks for it: mid-meeting, stopping the picture is
     * the screen toggle's job, not a source choice.
     */
    allowOff?: boolean
    /** Label of the "ask the host for a list" tile; empty hides it. */
    pickLabel?: string
    /** That probe is in flight. */
    pickBusy?: boolean
  }>(),
  { disabled: false, compact: false, allowOff: false, pickLabel: '', pickBusy: false },
)

const emit = defineEmits<{ select: [id: string]; pick: [] }>()

const { t } = useI18n()
</script>

<style scoped>
/* No padding: the first tile must start on the same vertical line as the
   section heading and the hint lines around it. Even 2px of inset reads as a
   misaligned block next to a heading, and there is no scroll container left
   whose edges the selected tile's ring would need room inside. */
.capture-sources {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(calc(150 * var(--dp)), 1fr));
  gap: var(--space-4);
}

/* The meeting panel is narrow: smaller tiles, more per row. The grid itself
   does NOT scroll — every tile is laid out inline and the panel body is the
   only scroll container. A nested `overflow` would also zero the grid's
   automatic minimum size, letting the body squash it to one clipped row. */
.capture-sources--compact {
  grid-template-columns: repeat(auto-fill, minmax(calc(120 * var(--dp)), 1fr));
}

.capture-source {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-3);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  text-align: left;
  cursor: pointer;
  min-width: 0;
}

.capture-source:hover {
  border-color: var(--accent-color);
}

.capture-source:disabled {
  cursor: default;
  opacity: 0.6;
}

.capture-source--on {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 1px var(--accent-color);
}

.capture-source__thumb {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  aspect-ratio: 16 / 9;
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  color: var(--text-hint);
  overflow: hidden;
}

/* An empty dashed frame reads as "no picture" without a word of explanation. */
.capture-source--off .capture-source__thumb {
  border: 1px dashed var(--border-color);
  background: transparent;
}

.capture-source--off.capture-source--on .capture-source__thumb {
  border-style: solid;
  border-color: var(--accent-color);
  color: var(--accent-color);
}

.capture-source--pick .capture-source__thumb {
  border: 1px dashed var(--border-color);
  background: transparent;
  color: var(--text-muted);
}

.capture-source--pick:hover .capture-source__thumb {
  color: var(--accent-color);
}

.capture-source__spinner {
  width: calc(14 * var(--dp));
  height: calc(14 * var(--dp));
  border: 2px solid var(--border-color);
  border-top-color: var(--accent-color);
  border-radius: var(--radius-full);
  animation: capture-source-spin 0.8s linear infinite;
}

@keyframes capture-source-spin {
  to { transform: rotate(360deg); }
}

.capture-source__thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.capture-source__check {
  position: absolute;
  top: calc(4 * var(--dp));
  right: calc(4 * var(--dp));
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(15 * var(--dp));
  height: calc(15 * var(--dp));
  border-radius: var(--radius-full);
  background: var(--accent-color);
  color: var(--bg-secondary);
  box-shadow: 0 0 0 1.5px var(--bg-secondary);
}

.capture-source__kind {
  position: absolute;
  left: calc(4 * var(--dp));
  bottom: calc(4 * var(--dp));
  padding: 0 var(--space-2);
  border-radius: var(--radius-xs);
  background: rgba(0, 0, 0, 0.55);
  color: rgba(255, 255, 255, 0.85);
  font-size: var(--font-size-2xs);
  line-height: 1.7;
}

.capture-source__name {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.capture-source--on .capture-source__name {
  color: var(--text-primary);
}
</style>
