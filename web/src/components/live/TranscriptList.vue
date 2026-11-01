<template>
  <div ref="rootRef" class="transcript" :class="{ 'transcript--fill': fill }">
    <div v-for="b in blocks" :key="b.key" class="transcript__row">
      <SpeakerAvatar class="transcript__avatar" :name="b.speaker" />
      <span class="transcript__speaker">{{ b.speaker }}</span>
      <span class="transcript__text">{{ b.text }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * The transcript, as rows: [avatar | speaker | text].
 *
 * Three surfaces show the same conversation — the publisher's 转写 tab, the
 * viewer's overlay and the meeting record opened later — so all three draw it
 * here. They used to be three implementations: two copies of the row markup
 * (kept aligned by a parity test) and, on the record, a `<pre>` of flattened
 * text with no speaker column at all.
 *
 * One grid for the whole list is what makes the speaker column work: it is
 * sized by its own content (a fixed column left ~60px of blank space for a
 * one-character name), and `display: contents` on each row lets both cells join
 * that one grid, so every line's text starts at the same x.
 *
 * The host supplies the inset — the pane already pads its surfaces, the viewer
 * panel pads its own body — through `--transcript-inset`.
 */
import { nextTick, ref, watch } from 'vue'
import SpeakerAvatar from '@/components/common/SpeakerAvatar.vue'
import type { NoteBlock } from '@/composables/liveNotes'

const props = withDefaults(defineProps<{ blocks: NoteBlock[]; fill?: boolean }>(), { fill: false })

const rootRef = ref<HTMLElement | null>(null)

/**
 * Follows the conversation, but only when the reader is already at the bottom.
 *
 * The publisher's pane used to scroll unconditionally, so scrolling up to
 * re-read an earlier line was undone by the next segment. "At the bottom" is
 * measured before the new row is laid out, which is why it happens first.
 */
watch(
  () => props.blocks.length,
  async () => {
    const el = rootRef.value
    if (!el) return
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 48
    if (!nearBottom) return
    await nextTick()
    el.scrollTop = el.scrollHeight
  },
)
</script>

<style scoped>
.transcript {
  display: grid;
  grid-template-columns: auto max-content minmax(0, 1fr);
  align-content: start;
  align-items: start;
  gap: var(--space-5, 10px) var(--space-4, 8px);
  padding: var(--transcript-inset, 0);
}

/* The list owns the pane's height, so an hour-long meeting scrolls inside it
   instead of growing the panel. The record's tab is inside the page's own
   scroller, so it stays a plain grid. */
.transcript--fill {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
}

/* The row generates no box: both cells join the list's grid. That is the only
   way the column can be content-sized AND shared by every line — a per-row
   flex would size each speaker cell on its own and every line's text would
   start at a different x. */
.transcript__row {
  display: contents;
}

/* Optical alignment with the first line of the text beside it: the text line
   box is ~21.5px for a 20px icon, so one design pixel down is centred. */
.transcript__avatar {
  margin-top: calc(1 * var(--dp));
}

/* One size below the text: the words are the content, the name is a signpost. */
.transcript__speaker {
  max-width: calc(96 * var(--dp));
  padding-top: calc(2 * var(--dp));
  font-size: var(--font-size-xs, 11px);
  font-weight: 600;
  line-height: 1.6;
  color: var(--text-muted, #656d76);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.transcript__text {
  min-width: 0;
  font-size: var(--font-size-md, 13px);
  line-height: 1.65;
  color: var(--text-primary, #1f2328);
  overflow-wrap: anywhere;
}
</style>
