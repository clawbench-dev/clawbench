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
// The tile language is shared with the member's screen card (same frame, same
// badge, same name line) — global CSS, not scoped. See that file's header.
import '@/assets/capture-sources.css'

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
