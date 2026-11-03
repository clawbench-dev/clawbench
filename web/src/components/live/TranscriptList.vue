<template>
  <div ref="rootRef" class="transcript" :class="{ 'transcript--fill': fill }" data-media-gallery>
    <div
      v-for="b in blocks"
      :key="b.key"
      class="transcript__row"
      :class="{ 'transcript__row--file': b.kind === 'file', 'transcript__row--shot': b.kind === 'shot' }"
    >
      <!-- A file event has no speaker: an avatar with the character 文 would
           read as a participant. The glyph says "this is not a person". -->
      <FileText v-if="b.kind === 'file'" class="transcript__avatar transcript__file-icon" :size="16" aria-hidden="true" />
      <MonitorUp v-else-if="b.kind === 'screen'" class="transcript__avatar transcript__file-icon" :size="16" aria-hidden="true" />
      <ImageIcon v-else-if="b.kind === 'shot'" class="transcript__avatar transcript__file-icon" :size="16" aria-hidden="true" />
      <!-- A continuation is the same person still talking, split off only to
           give a monologue paragraph breaks. Repeating the avatar and the name
           would read as a new turn; the empty cells keep the grid's columns
           aligned with every other row. -->
      <span v-else-if="b.continuation" class="transcript__avatar" aria-hidden="true" />
      <SpeakerAvatar v-else class="transcript__avatar" :name="b.speaker" />
      <span class="transcript__speaker">{{ b.continuation ? '' : speakerOf(b) }}</span>
      <div v-if="b.kind === 'shot' && b.shot" class="transcript__text transcript__text--shot">
        <span class="transcript__shot-head">
          <span class="transcript__shot-meta">{{ labelOf(b) }}</span>
          <button
            v-if="!readonly"
            class="transcript__shot-del"
            type="button"
            :title="i18n.global.t('meeting.shots.delete')"
            :aria-label="i18n.global.t('meeting.shots.delete')"
            data-role="shot-delete"
            @click="emit('shot-delete', b)"
          >
            <Trash2 :size="13" />
          </button>
        </span>
        <!-- The SAME figure a markdown image renders as: 1px frame + floating
             view button, thumbnail inline, lightbox on click (touch opens from
             the image body, desktop from the view button — the document-level
             listener in Lightbox.vue drives both, so this component carries no
             viewer logic of its own). `data-full-src` points at the viewing
             size; the lightbox loads that instead of the thumbnail. The reset
             button is hidden at rest and revealed by the shared stylesheet
             while the figure holds an inline zoom — same as every markdown
             image; the reset gesture itself is inlineZoom's delegated listener. -->
        <div
          class="transcript__shot image-block-wrapper image-block-wrapper--thumb"
          data-role="shot-open"
        >
          <div class="image-block-header">
            <span class="image-block-header-actions">
              <button
                class="image-block-view-btn"
                type="button"
                data-action="view"
                :title="i18n.global.t('imageBlock.view')"
                :aria-label="i18n.global.t('imageBlock.view')"
              >
                <Maximize :size="14" />
              </button>
              <button
                class="image-block-reset-btn"
                type="button"
                data-action="reset-zoom"
                :title="i18n.global.t('imageBlock.resetZoom')"
                :aria-label="i18n.global.t('imageBlock.resetZoom')"
              >
                <Minimize2 :size="14" />
              </button>
            </span>
          </div>
          <span class="lightbox-img-wrap">
            <img
              class="lightbox-img"
              :src="b.shot.thumbUrl"
              :data-full-src="b.shot.fullUrl"
              :alt="labelOf(b)"
              loading="lazy"
            />
          </span>
        </div>
      </div>
      <span v-else class="transcript__text">{{ textOf(b) }}</span>
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
 * panel pads its own body — through `--transcript-inset`, which defaults to 0
 * here so the token is declared where it is read.
 */
import { nextTick, ref, watch } from 'vue'
import { FileText, ImageIcon, Maximize, Minimize2, MonitorUp, Trash2 } from 'lucide-vue-next'
import i18n from '@/i18n'
import { shotLabel } from '@/utils/meetingShots'
import SpeakerAvatar from '@/components/common/SpeakerAvatar.vue'
import type { NoteBlock } from '@/composables/liveNotes'

const props = withDefaults(
  defineProps<{
    blocks: NoteBlock[]
    fill?: boolean
    /** The member's record: same rows, but deleting is the publisher's alone. */
    readonly?: boolean
  }>(),
  { fill: false, readonly: false },
)
/**
 * No `shot-open`: opening a frame is the Lightbox's own document-level click
 * handler (`.image-block-view-btn` on desktop, the image body on touch), the
 * same path a markdown image takes — so this component carries no viewer
 * wiring, and the share SPA (whose Lightbox is a sibling, not an ancestor)
 * works with the identical markup.
 */
const emit = defineEmits<{
  'shot-delete': [block: NoteBlock]
}>()

/**
 * Localization goes through the global instance rather than `useI18n()`.
 *
 * This component is mounted by the public share SPA and by three panels, and
 * one of the parity tests mounts it without an i18n plugin. Reading
 * `i18n.global.t` inside a render function still tracks the locale ref, so a
 * language switch re-renders it like any other translated text.
 */
function speakerOf(b: NoteBlock): string {
  if (b.speaker) return b.speaker
  if (b.kind === 'shot') return i18n.global.t('meeting.shots.speaker')
  return b.kind === 'file' ? i18n.global.t('liveShare.fileSpeaker') : ''
}

/** "打开 architecture.md" / "切换共享源" — why the frame was taken. */
function labelOf(b: NoteBlock): string {
  return b.shot ? shotLabel(b.shot.reason, b.shot.ref) : ''
}

/** A live file row carries a bare path; a stored transcript carries a sentence. */
function textOf(b: NoteBlock): string {
  if (b.kind === 'file' && b.fileAction) {
    return i18n.global.t(
      b.fileAction === 'open' ? 'liveShare.fileOpened' : 'liveShare.fileClosed',
      { path: b.text },
    )
  }
  return b.text
}

const rootRef = ref<HTMLElement | null>(null)

/**
 * Follows the conversation, but only when the reader is already at the bottom.
 *
 * The publisher's pane used to scroll unconditionally, so scrolling up to
 * re-read an earlier line was undone by the next segment. "At the bottom" is
 * measured before the new row is laid out, which is why it happens first.
 *
 * The signature is the row count AND the length of the last row's text: one
 * speaker talking without a pause extends the last block instead of adding a
 * row, and watching only the count left the panel frozen while the words grew
 * below the fold.
 */
watch(
  () => {
    const last = props.blocks[props.blocks.length - 1]
    return `${props.blocks.length}:${last?.text.length ?? 0}`
  },
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
  /* Declared here, at its only reader: nothing sets it today, and an
     undeclared token is what the design-token guard exists to catch. A host
     that ever needs an inset overrides the variable instead of copying the
     list. */
  --transcript-inset: 0;
  padding: var(--transcript-inset);
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

/* A file event is a marker on the timeline, not something somebody said: it
   sits back so the conversation still reads as the conversation. */
.transcript__file-icon {
  color: var(--text-muted, #656d76);
}

.transcript__row--file .transcript__speaker,
.transcript__row--file .transcript__text {
  color: var(--text-muted, #656d76);
  font-style: italic;
}

/* A frame row: the label is the caption, the figure is the content. */
.transcript__text--shot {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
}

.transcript__shot-head {
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
}

.transcript__shot-meta {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-muted, #656d76);
}

/* The only destructive control in this list; sized for a fingertip, not a
   cursor (a 14px icon with a 22px hit area). */
.transcript__shot-del {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(22 * var(--dp));
  height: calc(22 * var(--dp));
  margin-left: auto;
  padding: 0;
  border: 0;
  border-radius: var(--radius-sm, 6px);
  background: transparent;
  color: var(--text-muted, #656d76);
  cursor: pointer;
}

.transcript__shot-del:hover {
  background: var(--bg-tertiary, rgba(0, 0, 0, 0.06));
  color: var(--text-primary, #1f2328);
}
</style>
