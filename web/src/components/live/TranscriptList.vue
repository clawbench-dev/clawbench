<template>
  <div
    ref="rootRef"
    class="transcript"
    :class="{ 'transcript--fill': fill }"
    data-media-gallery
    @scroll.passive="onScroll"
    @click="interactions.handleClick"
  >
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
          <span class="transcript__shot-meta" v-html="shotMetaHtml(b)" />
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
        <!-- The figure is produced by the SHARED media-block factory — the same
             one that wraps markdown images — so the chrome (view + reset
             buttons) cannot drift from markdown preview. Opening is the
             Lightbox's document-level click handler (view button on desktop,
             image body on touch); the reset gesture is inlineZoom's delegated
             listener. `data-full-src` points at the viewing size. -->
        <div class="transcript__shot" data-role="shot-open" v-html="shotFigure(b)" />
      </div>
      <span v-else class="transcript__text" v-html="textHtml(b)" />
    </div>

    <!-- The inline file preview (desktop card / touch bottom sheet) and its
         click wiring come from the SAME interaction layer the markdown preview
         and the minutes use, so a path chip in a transcript row behaves
         identically on every reading surface.

         It stays INSIDE the list so this component keeps a single root element
         (hosts pass `data-role` to it, which a fragment root cannot inherit).
         Both of its shapes are out of flow — the card is `position: fixed` and
         the sheet teleports to <body> — so it does not become a grid cell. -->
    <CodeLinkPreview
      v-if="interactions.codeLinkPreview.enabled.value"
      :preview="interactions.codeLinkPreview"
    />
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
import { onMounted, ref, watch } from 'vue'
import { FileText, ImageIcon, MonitorUp, Trash2 } from 'lucide-vue-next'
import i18n from '@/i18n'
import { shotLabel, shotRefName } from '@/utils/meetingShots'
import { annotateMediaBlocks } from '@/utils/mediaBlockFactory'
import { escapeHtml } from '@/utils/html.ts'
import { annotatePlainText } from '@/composables/usePlainTextAnnotation.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { useMarkdownDocumentInteractions } from '@/composables/useMarkdownDocumentInteractions.ts'
import { useStickToBottom } from '@/composables/useStickToBottom.ts'
import CodeLinkPreview from '@/components/file/CodeLinkPreview.vue'
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
 * This component is mounted by the public share SPA and by three panels.
 * Reading `i18n.global.t` inside a render function still tracks the locale
 * ref, so a language switch re-renders it like any other translated text.
 * (The inline preview card it hosts does call `useI18n()`, so every host —
 * tests included — has to install the plugin.)
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

/** Figures already built for a (thumb, full, label) triple — the transcript
 *  re-renders on every live poll, and each build pays a DOMParser round trip. */
const figureCache = new Map<string, string>()

/**
 * One key frame as the shared media-block factory renders it — the SAME figure
 * a markdown image gets, chrome included (view + reset buttons), so this list
 * cannot drift from markdown preview. The `--thumb` modifier goes THROUGH the
 * factory rather than being stamped onto a hand-copied wrapper.
 */
function shotFigure(b: NoteBlock): string {
  const shot = b.shot
  if (!shot) return ''
  const label = labelOf(b)
  const key = `${shot.thumbUrl}\n${shot.fullUrl}\n${label}`
  let html = figureCache.get(key)
  if (html === undefined) {
    const src = `<img src="${escapeHtml(shot.thumbUrl)}" data-full-src="${escapeHtml(shot.fullUrl)}" alt="${escapeHtml(label)}" loading="lazy">`
    html = annotateMediaBlocks(src, { wrapperClass: 'image-block-wrapper--thumb' })
    if (figureCache.size > 200) figureCache.clear()
    figureCache.set(key, html)
  }
  return html
}

/** The wording the server used before the label was unified to "打开文件". */
const LEGACY_FILE_EVENT_RE = /^(打开|Opened|关闭|Closed)\s+(.+)$/i

/** A live file row carries a bare path; a stored transcript carries a sentence. */
function textOf(b: NoteBlock): string {
  if (b.kind === 'file' && b.fileAction) {
    return i18n.global.t(
      b.fileAction === 'open' ? 'liveShare.fileOpened' : 'liveShare.fileClosed',
      { path: b.text },
    )
  }
  // A stored row is the sentence the server wrote at the time ("打开 X" /
  // "Opened X"). Re-say it in today's wording so an old meeting reads the same
  // as a new one; the path (and the "（21 秒）" tail) is carried over as-is.
  const legacy = LEGACY_FILE_EVENT_RE.exec(b.text)
  if (b.kind === 'file' && legacy) {
    const opening = /^open/i.test(legacy[1]) || legacy[1] === '打开'
    return i18n.global.t(
      opening ? 'liveShare.fileOpened' : 'liveShare.fileClosed',
      { path: legacy[2] },
    )
  }
  return b.text
}

const rootRef = ref<HTMLElement | null>(null)

/**
 * The same click/drag layer the markdown preview and the minutes use.
 *
 * A transcript row names files and URLs as often as a document does, so the
 * affordances must not be a second implementation: desktop click on a chip
 * opens the inline preview card, touch tap opens the bottom sheet, the open
 * button opens the file viewer, a directory chip opens the file manager, and
 * an external link goes through the shared confirm dialog (that one is a
 * document-level layer, so it needs no wiring here).
 *
 * `source: 'live'` records the meeting as the navigation origin, so Back from
 * the viewer returns to this panel — exactly like the minutes.
 */
const interactions = useMarkdownDocumentInteractions({
  containerRef: rootRef,
  // No markdown document: relative paths are resolved by the annotator (against
  // the project root), and there are no relative anchors to resolve here.
  filePath: () => '',
  source: 'live',
  enableCommitLinks: false,
})

const { verifyFilePaths } = useFilePathAnnotation()

/** Annotated HTML per utterance — the list re-renders on every live poll, and
 *  each build pays a DOMParser round trip plus two annotation passes. */
interface AnnotatedBlockText {
  html: string
  paths: string[]
}
const textCache = new Map<string, AnnotatedBlockText>()

function textHtml(b: NoteBlock): string {
  return rowAnnotation(b).html
}

function annotatedText(text: string): AnnotatedBlockText {
  let hit = textCache.get(text)
  if (hit === undefined) {
    const annotation = annotatePlainText(text)
    hit = { html: annotation.html, paths: annotation.detectedPaths }
    // A guard, not a working set: the whole list re-renders on every live poll,
    // so dropping entries that are still on screen would re-parse all of them
    // at once. Real meetings stay far below this (an 8-hour meeting is a few
    // thousand utterances), and the cache dies with the component.
    if (textCache.size > 20000) textCache.clear()
    textCache.set(text, hit)
  }
  return hit
}

/**
 * "打开 AGENTS.md" / "关闭 README.md（21 秒）" — a file event whose path sits at
 * the project root, so the recorded path is a bare name with no directory.
 *
 * The path annotator deliberately skips a single-segment name (it cannot tell
 * `AGENTS.md` from an identifier like `build.sh`), which left exactly the most
 * common file in a meeting — a root-level README — as dead text. A `./` prefix
 * makes it unambiguous for the annotator; the chip's visible text is then
 * restored, so the row reads exactly as before and the click opens the file.
 */
const FILE_EVENT_NAME_RE = /(^|[\s（(])([^\s/\\（）()]+?\.[A-Za-z0-9]{1,10})(?=[\s（()）]|$)/
const fileEventCache = new Map<string, AnnotatedBlockText>()

function fileEventText(text: string): AnnotatedBlockText {
  let hit = fileEventCache.get(text)
  if (hit === undefined) {
    const source = text.replace(FILE_EVENT_NAME_RE, (_m, pre: string, name: string) => `${pre}./${name}`)
    const annotation = annotatePlainText(source)
    hit = { html: annotation.html, paths: annotation.detectedPaths }
    if (annotation.detectedPaths.length > 0) {
      const doc = new DOMParser().parseFromString(annotation.html, 'text/html')
      for (const chip of doc.querySelectorAll<HTMLElement>('.chat-file-path')) {
        if (chip.textContent?.startsWith('./')) chip.textContent = chip.textContent.slice(2)
      }
      hit = { html: doc.body.innerHTML, paths: annotation.detectedPaths }
    }
    if (fileEventCache.size > 2000) fileEventCache.clear()
    fileEventCache.set(text, hit)
  }
  return hit
}

/** The annotated form of one row: a frame annotates its caption, a file event
 *  annotates its bare name, everything else annotates the utterance. */
function rowAnnotation(b: NoteBlock): AnnotatedBlockText {
  if (b.kind === 'shot') return shotMeta(b)
  const text = textOf(b)
  return b.kind === 'file' ? fileEventText(text) : annotatedText(text)
}

/**
 * The frame's caption ("打开文件 main.ts"), annotated like every other path.
 *
 * A caption that names a file IS a reference to it — the row already shows the
 * picture, and the file it was taken for must be openable from the same line.
 *
 * The annotation source uses the whole `ref`, not the displayed base name: the
 * path annotator deliberately ignores a bare `main.ts` (it cannot tell a
 * filename from an identifier like `build.sh`), and a root-level file's ref IS
 * a bare name. So a single-segment ref gets the same `./` prefix the file
 * event uses, and the chip's text is then shortened back to the base name —
 * the row reads the same, the click opens the file.
 */
const shotMetaCache = new Map<string, AnnotatedBlockText>()

function shotMeta(b: NoteBlock): AnnotatedBlockText {
  const shot = b.shot
  if (!shot) return { html: '', paths: [] }
  const ref = shot.ref ?? ''
  const name = shotRefName(ref)
  // Nothing to annotate: no file reference at all, a path with spaces (the
  // annotator stops at whitespace and would chip a fragment), or share mode.
  // Keep the short caption the row has always shown.
  if (!name || /\s/.test(ref)) return { html: escapeHtml(labelOf(b)), paths: [] }

  // The caption's own wording, with the base name swapped for something the
  // annotator can see (full path, or `./name` for a root-level file).
  const display = labelOf(b)
  const target = /[\\/]/.test(ref) ? ref : `./${ref}`
  const at = display.lastIndexOf(name)
  const source = at < 0 ? display : `${display.slice(0, at)}${target}${display.slice(at + name.length)}`
  let hit = shotMetaCache.get(source)
  if (hit === undefined) {
    const annotation = annotatePlainText(source)
    if (annotation.detectedPaths.length === 0) {
      hit = { html: escapeHtml(display), paths: [] }
    } else {
      // Shorten the visible text back to the base name; the chip keeps the
      // whole path as its target.
      const doc = new DOMParser().parseFromString(annotation.html, 'text/html')
      for (const chip of doc.querySelectorAll<HTMLElement>('.chat-file-path')) {
        chip.textContent = name
      }
      hit = { html: doc.body.innerHTML, paths: annotation.detectedPaths }
    }
    if (shotMetaCache.size > 20000) shotMetaCache.clear()
    shotMetaCache.set(source, hit)
  }
  return hit
}

function shotMetaHtml(b: NoteBlock): string {
  return shotMeta(b).html
}

/**
 * Stamp `data-path-type` on the chips once their HTML is in the DOM.
 *
 * That attribute is what makes a chip interactive (preview card / open
 * button), so skipping this leaves dead-looking chips. Runs on mount and after
 * every list change: the live pane appends a row at a time, and
 * `verifyFilePaths` keeps its own cache, so an unchanged conversation costs one
 * map lookup.
 */
function verifyRenderedPaths(): void {
  const el = rootRef.value
  if (!el) return
  const paths = new Set<string>()
  for (const b of props.blocks) {
    for (const p of rowAnnotation(b).paths) paths.add(p)
  }
  if (paths.size > 0) void verifyFilePaths([...paths], el)
}

// `post` so the annotated HTML is already in the DOM: verification queries the
// chips it just stamped, not the previous render.
watch(() => props.blocks, verifyRenderedPaths, { flush: 'post' })
onMounted(verifyRenderedPaths)

/**
 * Follows the conversation while the reader is at the bottom, and lands on the
 * newest line when the list (re)appears — a 转写 tab opened mid-meeting, or a
 * `v-show` panel being reopened, both used to start at the top of the meeting
 * and never catch up. The mechanics live in the shared composable so the live
 * chat beside it cannot drift.
 *
 * The signature is the row count AND the length of the last row's text: one
 * speaker talking without a pause extends the last block instead of adding a
 * row, and watching only the count left the panel frozen while the words grew
 * below the fold.
 */
const { onScroll } = useStickToBottom(rootRef, () => {
  const last = props.blocks[props.blocks.length - 1]
  return `${props.blocks.length}:${last?.text.length ?? 0}`
})
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

/* Same size and colour as the text it introduces: a frame caption is a line of
   the transcript, and a smaller/dimmer one made the two marker rows (file
   event, frame) read as different kinds of thing. */
.transcript__shot-meta {
  font-size: var(--font-size-md, 13px);
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
