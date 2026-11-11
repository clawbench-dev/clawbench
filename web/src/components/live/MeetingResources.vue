<template>
  <div class="meeting-resources">
    <!-- First load: the tab is a list of file rows, so the placeholder draws
         those rows (glyph + name + meta) instead of a lone ring. -->
    <PanelSkeleton
      v-if="loading"
      class="res-skeleton"
      variant="list"
      :rows="6"
      :meta="false"
      :label="t('common.loading')"
    />

    <div v-else-if="error" class="res-center res-center--error">
      <span>{{ t('meeting.resources.loadFailed') }}</span>
      <button class="meeting-link" type="button" @click="reload">{{ t('common.retry') }}</button>
    </div>

    <template v-else>
      <!-- The meeting link outlives the room only when the owner publishes the
           record. The state and the action live HERE, next to the link, because
           this is where someone decides to send it on. Off by default — the
           token exists so people could join, which is not the same as
           publishing the minutes.

           Drawn as a card (same frame as the folder header below) because the
           old 15px checkbox at the end of a grey label row was the only
           interactive thing in the block and read as a list option, while the
           decision it makes is "anyone with this link can read the minutes
           forever". The badge states, the button acts — the badge is NOT
           clickable on purpose, so a stray tap on a status cannot publish.

           Shared state is green, not the theme accent: three of the 36 themes
           paint the accent orange, which everywhere else means "running". The
           edge stripe is absolutely positioned so it costs no layout shift. -->
      <div
        v-if="shareToken && shareLinks.length > 0"
        class="res-share-card"
        :data-state="shareEnabled ? 'shared' : 'private'"
        data-role="meeting-link"
      >
        <div class="res-share-head">
          <span class="res-share-icon"><Link2 :size="15" /></span>
          <span class="res-share-title">{{ t('meeting.resources.linkTitle') }}</span>
          <span
            class="res-share-badge"
            :class="shareEnabled ? 'res-share-badge--shared' : 'res-share-badge--private'"
            data-role="meeting-share-badge"
          >
            <Globe v-if="shareEnabled" :size="11" />
            <Lock v-else :size="11" />
            {{ shareEnabled ? t('meeting.detail.shareBadgePublic') : t('meeting.detail.shareBadgePrivate') }}
          </span>
        </div>

        <ShareAddressList :links="shareLinks" leading-icon :copy-label="t('shareAddress.copy')" />

        <!-- What a visitor actually gets. The list is the answer to "共享出去
            到底是什么", which the sentence alone never made obvious. -->
        <div class="res-share-caps" data-role="meeting-share-caps">
          <span v-for="cap in shareCaps" :key="cap.key" class="res-share-cap">
            <component :is="cap.icon" :size="10" />
            {{ cap.label }}
          </span>
        </div>

        <div class="res-share-note">
          <Info :size="11" />
          <span>{{ shareEnabled ? t('meeting.detail.sharePublicDesc') : t('meeting.detail.sharePrivateDesc') }}</span>
        </div>

        <div class="res-share-foot">
          <button
            class="fbtn"
            :class="{ 'fbtn-primary': !shareEnabled }"
            type="button"
            data-role="meeting-share-toggle"
            :disabled="shareSaving"
            @click="onToggleShare"
          >
            <span v-if="shareSaving" class="li-spinner res-share-spin" aria-hidden="true" />
            {{ shareEnabled ? t('meeting.detail.shareStop') : t('meeting.detail.sharePublish') }}
          </button>
        </div>
      </div>

      <div v-if="folderPath || total > 0" class="res-head">
        <span class="res-head__icon"><Folder :size="15" /></span>
        <div class="res-head__main">
          <div class="res-head__title">{{ t('meeting.resources.title') }}</div>
          <div v-if="folderPath" class="res-head__path" :title="folderPath">{{ folderPath }}</div>
          <div class="res-head__meta">{{ summary }}</div>
        </div>
        <div v-if="folderPath" class="res-head__actions">
          <button class="res-act" type="button" :title="t('liveShare.notes.copyPath')" @click="copy(folderPath)">
            <Copy :size="13" />
          </button>
          <button class="res-act" type="button" :title="t('meeting.resources.reveal')" @click="openFolder">
            <ExternalLink :size="13" />
          </button>
        </div>
      </div>

      <!-- Nothing at all: no meeting folder, nothing referenced. A single muted
           line left at the top of an otherwise blank pane reads as a failure,
           so the whole-tab case gets the shared empty card — filled, so it
           centres like the other three tabs' empty states. When a folder does
           exist the header above already explains the zero, and a small hint is
           the right weight. -->
      <EmptyState
        v-if="total === 0 && !folderPath"
        :icon="FolderOpen"
        :title="t('meeting.resources.emptyTitle')"
        :desc="t('meeting.resources.empty')"
      />
      <div v-else-if="total === 0" class="res-hint">
        <Info :size="12" />
        {{ t('meeting.resources.empty') }}
      </div>

      <div v-for="group in groups" :key="group.key" class="res-group">
        <div class="res-group__label">
          <Folder v-if="group.key === 'folder'" :size="11" />
          <AudioLines v-else-if="group.key === 'audio'" :size="11" />
          <ImageIcon v-else-if="group.key === 'shots'" :size="11" />
          <Link2 v-else :size="11" />
          {{ group.title }}
          <span class="res-group__count">{{ group.items.length }}</span>
        </div>

        <div
          v-for="item in group.items"
          :key="item.key"
          class="res-row"
          role="button"
          tabindex="0"
          :draggable="isWideScreen"
          :title="item.displayPath"
          @click="open(item)"
          @keydown.enter.prevent="open(item)"
          @dragstart="onDragStart($event, item)"
          @dragend="cleanupDragGhost()"
        >
          <!-- Same glyph source as the file manager: the material icon theme,
               plus the file manager's own thumbnail for decodable raster
               images. Anything the backend cannot rasterize (SVG/AVIF/…) or
               whose thumbnail fails keeps the type icon, so a row never shows
               an empty tile. -->
          <span class="res-icon">
            <img
              v-if="showsThumb(item)"
              class="res-thumb"
              :src="thumbUrl(item)"
              :alt="item.name"
              loading="lazy"
              @error="onThumbError(item)"
            />
            <FileIcon v-else :path="item.path" :is-dir="item.isDir" :size="18" />
          </span>

          <span class="res-main">
            <span class="res-name">
              {{ item.name }}
              <span v-if="item.badge" class="res-badge">{{ item.badge }}</span>
            </span>
            <span v-if="group.key === 'referenced'" class="res-path">{{ item.displayPath }}</span>
            <span class="res-meta">{{ metaFor(item) }}</span>
          </span>

          <span class="res-actions">
            <button
              class="res-act res-act--attach"
              type="button"
              :title="t('meeting.resources.attach')"
              :data-attach="item.path"
              @click.stop="attach(item)"
            >
              <Paperclip :size="13" />
            </button>
            <button
              class="res-act"
              type="button"
              :title="t('liveShare.notes.copyPath')"
              @click.stop="copy(item.displayPath || item.path)"
            >
              <Copy :size="13" />
            </button>
          </span>
        </div>

        <div v-if="group.hint" class="res-hint res-hint--group">
          <Link2 :size="12" />
          {{ group.hint }}
        </div>
      </div>

      <div v-if="total > 0" class="res-hint">
        <Info :size="12" />
        {{ t('meeting.resources.dragHint') }}
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
/**
 * The 资源 tab: every local file this meeting left behind, plus the project
 * files its minutes reference.
 *
 * Clicking opens through `openFilePath(..., 'live')` — the same pipeline the
 * file manager uses (existence check, directory branch, line targets, viewer
 * overlay) and the source the navigation coordinator needs to return here.
 * Dragging writes the shared attach payload, so the chat column accepts the
 * row exactly like a file-manager row; the paperclip is the touch fallback
 * (the chat column is not visible in the narrow layout).
 */
import { computed, reactive, toRefs, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AudioLines,
  Captions,
  Copy,
  ExternalLink,
  FileText,
  Folder,
  FolderOpen,
  Globe,
  Image as ImageIcon,
  Info,
  Link2,
  Lock,
  MessagesSquare,
  Paperclip,
  SpellCheck,
} from 'lucide-vue-next'
import FileIcon from '@/components/common/FileIcon.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import ShareAddressList from '@/components/common/ShareAddressList.vue'
import EmptyState from '@/components/common/EmptyState.vue'
// The publish action wears the shared pill. A component that uses `.fbtn`
// without going through ModalDialog loads the sheet itself — an ancestor
// happening to import it is not a contract.
import '@/assets/modal-footer-btn.css'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import type { MeetingResourceGroup, MeetingResourceItem } from '@/composables/useMeetingResources'
import type { ShareLink } from '@/composables/useShareLinks'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation'
import { requestAttachmentTarget } from '@/composables/useConversationTarget'
import { startAttachDrag, cleanupDragGhost } from '@/utils/attachDrag'
import { formatFileSize } from '@/utils/fileType'
import { buildPathThumbUrl } from '@/utils/fileIcon'
import { isThumbableExt } from '@/utils/fileManager'
import { mediaVersionFor } from '@/composables/useMediaWatch'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'

const props = defineProps<{
  groups: MeetingResourceGroup[]
  loading: boolean
  error: string
  total: number
  folderPath: string
  /** Every address the meeting link is reachable on; empty while it loads. */
  links?: ShareLink[]
  /** The record's token. Empty hides the link block entirely. */
  shareToken?: string
  /**
   * Whether the record's link stays readable after the meeting ends. Off by
   * default server-side: the token exists so people could join, which is not
   * the same as publishing the minutes.
   */
  shareEnabled?: boolean
  /** A toggle is in flight — the checkbox is disabled until the server agrees. */
  shareSaving?: boolean
}>()

const emit = defineEmits<{ reload: []; 'update:shareEnabled': [value: boolean] }>()

// Attaching a resource by dragging it onto the chat is a wide-screen gesture —
// on a narrow layout the chat column is never on screen next to this list.
// (The per-row "add to conversation" button is the touch fallback.)
const { isWideScreen } = useWideScreenLayout()

const { t, locale } = useI18n()
const toast = useToast()
const dialog = useDialog()
// Read through the composable (not a module-level named import) so tests can
// mock one module and every surface resolves the same primitive.
const { openFilePath } = useFilePathAnnotation()

const { groups, loading, error, total } = toRefs(props)
const reload = () => emit('reload')
const shareLinks = computed(() => props.links ?? [])

/**
 * What a visitor to the shared link can read. Four tabs, in the order the share
 * page shows them — the same labels, so the card and the page agree.
 */
const shareCaps = computed(() => [
  { key: 'minutes', icon: FileText, label: t('meeting.detail.tabMinutes') },
  { key: 'transcript', icon: Captions, label: t('meeting.detail.tabTranscript') },
  { key: 'revised', icon: SpellCheck, label: t('meeting.detail.tabRevised') },
  { key: 'chat', icon: MessagesSquare, label: t('meeting.detail.tabChat') },
])

/**
 * The parent owns the request; this only reports the intent.
 *
 * Publishing is confirmed first: it turns a link that dies with the meeting
 * into one that outlives it, and the only way back is pressing the same button
 * again. Unpublishing is not confirmed — it takes nothing away from the user,
 * it closes a door.
 */
async function onToggleShare(): Promise<void> {
  if (props.shareSaving) return
  if (!props.shareEnabled) {
    const ok = await dialog.confirm(t('meeting.detail.shareConfirmBody'), {
      title: t('meeting.detail.shareConfirmTitle'),
      confirmText: t('meeting.detail.sharePublish'),
      cancelText: t('common.cancel'),
    })
    if (!ok) return
  }
  emit('update:shareEnabled', !props.shareEnabled)
}

const summary = computed(() => {
  const folder = props.groups.find((group) => group.key === 'folder')?.items.length ?? 0
  const audio = props.groups.find((group) => group.key === 'audio')?.items.length ?? 0
  const shots = props.groups.find((group) => group.key === 'shots')?.items.length ?? 0
  const refs = props.groups.find((group) => group.key === 'referenced')?.items.length ?? 0
  return t('meeting.resources.summary', { total: props.total, folder: folder + audio + shots, refs })
})

// ── Row glyphs ──
// Two-tier source, same as the file manager list: a real thumbnail for images
// the backend can rasterize, the material icon theme for everything else.
const thumbErrors = reactive(new Set<string>())

function showsThumb(item: MeetingResourceItem): boolean {
  return !item.isDir && isThumbableExt(item.path) && !thumbErrors.has(item.path)
}

function thumbUrl(item: MeetingResourceItem): string {
  const url = buildPathThumbUrl(item.path, 80)
  // Versioned so a shot rewritten in place re-fetches instead of staying
  // cached. The media watcher keys on project-relative paths.
  const v = mediaVersionFor(item.displayPath || item.path)
  return v ? `${url}&t=${v}` : url
}

function onThumbError(item: MeetingResourceItem): void {
  thumbErrors.add(item.path)
}

// A reload re-lists the folder, so a path that failed before deserves a fresh
// attempt rather than a permanent icon.
watch(() => props.groups, () => thumbErrors.clear())

function open(item: MeetingResourceItem): void {
  void openFilePath(item.path, undefined, undefined, 'live')
}

function openFolder(): void {
  if (props.folderPath) void openFilePath(props.folderPath, undefined, undefined, 'live')
}

function attach(item: MeetingResourceItem): void {
  // Touch/narrow fallback for drag: the chat column is not visible there, so
  // the same "deliver to the active conversation" helper the markdown badges
  // use is the only path that works.
  requestAttachmentTarget({ path: item.path, isDir: item.isDir }, { deliverTo: 'active' })
}

function onDragStart(event: DragEvent, item: MeetingResourceItem): void {
  startAttachDrag(event, item.path, { name: item.name, isDir: item.isDir })
}

async function copy(text: string): Promise<void> {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    toast.show(t('common.copied'), { type: 'success', icon: 'copy' })
  } catch {
    // Clipboard can be unavailable (permissions / non-secure origin); the
    // title attribute still exposes the path.
  }
}

function metaFor(item: MeetingResourceItem): string {
  const parts = [item.typeLabel]
  if (typeof item.size === 'number' && item.size > 0) parts.push(formatFileSize(item.size))
  if (item.modified) {
    const date = new Date(item.modified)
    if (!Number.isNaN(date.getTime())) {
      parts.push(date.toLocaleString(locale.value, {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      }))
    }
  }
  if (item.kind === 'dir') return item.typeLabel
  return parts.join(' · ')
}
</script>

<style scoped>
.meeting-resources {
  display: flex;
  flex-direction: column;
  /* Fills the tab body so the empty card can centre in it (the card is a
     `flex: 1` block). `auto` basis, not `0`: with rows present the content must
     keep its natural height and let the body scroll. */
  flex: 1 1 auto;
  min-height: 0;
  gap: var(--space-5);
  min-width: 0;
}

.res-center {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-8) 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}
.res-center--error { color: var(--color-red); }

/* `.res-row` insets its own text by 6dp and the tab body adds no padding, so
   the placeholder repeats that inset — otherwise the list jumps sideways the
   moment the rows arrive. */
.res-skeleton { --panel-skeleton-inset: calc(6 * var(--dp)); }

.res-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
}
.res-head__icon {
  display: grid;
  place-items: center;
  width: calc(30 * var(--dp));
  height: calc(30 * var(--dp));
  flex-shrink: 0;
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  color: var(--accent-color);
}
.res-head__main { flex: 1; min-width: 0; }
.res-head__title { font-size: var(--font-size-sm); font-weight: 650; color: var(--text-primary); }
.res-head__path {
  margin-top: calc(2 * var(--dp));
  font-family: var(--font-mono);
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.res-head__meta { margin-top: calc(3 * var(--dp)); font-size: var(--font-size-2xs); color: var(--text-hint); }
.res-head__actions { display: flex; gap: var(--space-1); flex-shrink: 0; }

/* The meeting link card: same frame, icon tile and radius as the folder header
   below, so the two blocks read as siblings. */
.res-share-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  transition: border-color var(--duration-base) ease;
}
/* Edge stripe marks "this record is readable by anyone with the link". It is
   absolutely positioned, so turning sharing on shifts nothing. */
.res-share-card::before {
  content: '';
  position: absolute;
  left: 0;
  top: var(--space-5);
  bottom: var(--space-5);
  width: 3px;
  border-radius: 0 3px 3px 0;
  background: transparent;
  transition: background var(--duration-base) ease;
}
.res-share-card[data-state='shared']::before { background: var(--color-green); }
.res-share-card[data-state='shared'] .res-share-icon {
  background: color-mix(in srgb, var(--color-green) 14%, transparent);
  color: var(--color-green);
}

.res-share-head { display: flex; align-items: center; gap: var(--space-4); }
.res-share-icon {
  display: grid;
  place-items: center;
  width: calc(30 * var(--dp));
  height: calc(30 * var(--dp));
  flex-shrink: 0;
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  color: var(--accent-color);
  transition: background var(--duration-base) ease, color var(--duration-base) ease;
}
.res-share-title {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-sm);
  font-weight: 650;
  color: var(--text-primary);
}

/* Status badge: read-only. Shape + icon + word, never colour alone — three of
   the themes paint the accent orange, which would read as "warning" on its
   own. */
.res-share-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  height: calc(20 * var(--dp));
  padding: 0 var(--space-4);
  border-radius: var(--radius-full);
  font-size: var(--font-size-2xs);
  font-weight: 600;
  white-space: nowrap;
  transition: background var(--duration-base) ease, color var(--duration-base) ease;
}
.res-share-badge--private { background: var(--bg-tertiary); color: var(--text-muted); }
.res-share-badge--shared {
  background: color-mix(in srgb, var(--color-green) 16%, transparent);
  color: var(--color-green);
}

/* The address field keeps its own grey fill everywhere else; on this card the
   card itself is grey, so the field would vanish into it. */
.res-share-card :deep(.share-addr-input) { background: var(--bg-primary); }

/* What a visitor can read: the four tabs of the share page, in its order. */
.res-share-caps { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.res-share-cap {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  height: calc(20 * var(--dp));
  padding: 0 var(--space-3) 0 var(--space-2);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-xs);
  background: var(--bg-primary);
  color: var(--text-secondary);
  font-size: var(--font-size-2xs);
  font-weight: 500;
  white-space: nowrap;
}
.res-share-cap svg { flex-shrink: 0; color: var(--text-hint); }
.res-share-card[data-state='shared'] .res-share-cap svg { color: var(--color-green); }

/* The sentence that answers "how long does this keep working" — an inset block
   rather than a bare paragraph, so it reads as a note and not as body copy. */
.res-share-note {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  line-height: 1.6;
}
.res-share-note svg { flex-shrink: 0; margin-top: 1px; color: var(--text-hint); }

.res-share-foot { display: flex; justify-content: flex-end; }
/* The ring is the global `.li-spinner`; a call site only sets its size. Inside
   a pill the 28dp default would push the label out of the 30dp height. */
.res-share-spin { --li-size: calc(12 * var(--dp)); }

.res-group { display: flex; flex-direction: column; gap: 1px; }.res-group__label {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-3) var(--space-1);
  font-size: var(--font-size-2xs);
  font-weight: 650;
  letter-spacing: calc(0.3 * var(--dp));
  color: var(--text-muted);
}
.res-group__count { font-weight: 500; color: var(--text-hint); letter-spacing: 0; }

.res-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-md);
  border: 1px solid transparent;
  cursor: pointer;
  min-width: 0;
}
.res-row:hover { background: var(--bg-hover); }
.res-row:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: -2px; }
.res-row:active { background: var(--bg-active); }

.res-icon {
  display: grid;
  place-items: center;
  width: calc(30 * var(--dp));
  height: calc(30 * var(--dp));
  flex-shrink: 0;
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  overflow: hidden;
}
/* Thumbnail fills the glyph tile (it replaces the icon, so the row height must
   not change). `cover` crops rather than letterboxing — the tile is a square. */
.res-thumb {
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: inherit;
  background: var(--bg-secondary);
}

.res-main { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.res-name {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-sm);
  font-weight: 550;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.res-badge {
  flex-shrink: 0;
  height: calc(14 * var(--dp));
  padding: 0 var(--space-2);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  line-height: calc(14 * var(--dp));
  font-weight: 500;
}
.res-path {
  margin-top: 1px;
  font-family: var(--font-mono);
  font-size: var(--font-size-2xs);
  color: var(--text-hint);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.res-meta {
  margin-top: 1px;
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.res-actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
  opacity: 0;
  transform: translateX(calc(3 * var(--dp)));
  transition: opacity var(--duration-fast, 0.12s) ease, transform var(--duration-fast, 0.12s) ease;
}
.res-row:hover .res-actions,
.res-row:focus-within .res-actions { opacity: 1; transform: none; }
.res-act {
  display: grid;
  place-items: center;
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}
.res-act:hover { background: var(--bg-active); color: var(--accent-color); }
.res-act--attach:hover { color: var(--color-green); }

.res-hint {
  display: flex;
  align-items: flex-start;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  line-height: 1.55;
}
.res-hint :deep(svg) { flex-shrink: 0; margin-top: 1px; color: var(--text-hint); }
.res-hint--group { margin-top: var(--space-1); }

/* Touch devices have no hover: the row actions must be visible without it.
   The narrow layout also hides the chat column, so the paperclip is the only
   way to attach. */
@media (hover: none), (max-width: 960px) {
  .res-actions { opacity: 1; transform: none; }
}
</style>
