<template>
  <div class="meeting-resources">
    <div v-if="loading" class="res-center">
      <LoadingIndicator size="sm" inline />
      {{ t('common.loading') }}
    </div>

    <div v-else-if="error" class="res-center res-center--error">
      <span>{{ t('meeting.resources.loadFailed') }}</span>
      <button class="meeting-link" type="button" @click="reload">{{ t('common.retry') }}</button>
    </div>

    <template v-else>
      <!-- The meeting link outlives the room: it resolves to this record until
           the record is deleted. It sits above the files because it is the one
           item a reader comes here to copy. Gated on `links` as well as the
           token so the address list never flashes as an empty input. -->
      <div v-if="shareToken && shareLinks.length > 0" class="res-link" data-role="meeting-link">
        <div class="res-group__label">
          <Link2 :size="11" />
          {{ t('meeting.resources.linkTitle') }}
        </div>
        <ShareAddressList :links="shareLinks" />
        <div class="res-hint">
          <Info :size="12" />
          {{ t('meeting.resources.linkHint') }}
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
      <MeetingEmpty
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
          draggable="true"
          :title="item.displayPath"
          @click="open(item)"
          @keydown.enter.prevent="open(item)"
          @dragstart="onDragStart($event, item)"
          @dragend="cleanupDragGhost()"
        >
          <span class="res-icon" :class="`res-icon--${item.kind}`">
            <FileText v-if="item.kind === 'minutes' || item.kind === 'revised' || item.kind === 'markdown'" :size="15" />
            <Captions v-else-if="item.kind === 'transcript'" :size="15" />
            <AudioLines v-else-if="item.kind === 'audio'" :size="15" />
            <ImageIcon v-else-if="item.kind === 'image'" :size="15" />
            <Code2 v-else-if="item.kind === 'code'" :size="15" />
            <Folder v-else-if="item.kind === 'dir'" :size="15" />
            <File v-else :size="15" />
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
import { computed, toRefs } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AudioLines,
  Captions,
  Code2,
  Copy,
  ExternalLink,
  File,
  FileText,
  Folder,
  FolderOpen,
  Image as ImageIcon,
  Info,
  Link2,
  Paperclip,
} from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import ShareAddressList from '@/components/common/ShareAddressList.vue'
import MeetingEmpty from './MeetingEmpty.vue'
import { useToast } from '@/composables/useToast'
import type { MeetingResourceGroup, MeetingResourceItem } from '@/composables/useMeetingResources'
import type { ShareLink } from '@/composables/useShareLinks'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation'
import { requestAttachmentTarget } from '@/composables/useConversationTarget'
import { startAttachDrag, cleanupDragGhost } from '@/utils/attachDrag'
import { formatFileSize } from '@/utils/fileType'

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
}>()

const emit = defineEmits<{ reload: [] }>()

const { t, locale } = useI18n()
const toast = useToast()
// Read through the composable (not a module-level named import) so tests can
// mock one module and every surface resolves the same primitive.
const { openFilePath } = useFilePathAnnotation()

const { groups, loading, error, total } = toRefs(props)
const reload = () => emit('reload')
const shareLinks = computed(() => props.links ?? [])

const summary = computed(() => {
  const folder = props.groups.find((group) => group.key === 'folder')?.items.length ?? 0
  const audio = props.groups.find((group) => group.key === 'audio')?.items.length ?? 0
  const shots = props.groups.find((group) => group.key === 'shots')?.items.length ?? 0
  const refs = props.groups.find((group) => group.key === 'referenced')?.items.length ?? 0
  return t('meeting.resources.summary', { total: props.total, folder: folder + audio + shots, refs })
})

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
  startAttachDrag(event, item.path, item.name, item.isDir)
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
  margin-top: 2px;
  font-family: var(--font-mono);
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.res-head__meta { margin-top: 3px; font-size: var(--font-size-2xs); color: var(--text-hint); }
.res-head__actions { display: flex; gap: var(--space-1); flex-shrink: 0; }

/* The meeting link block: same column rhythm as a resource group, but it is
   not a group — it holds one control (the address list) and the sentence that
   says how long it stays valid. */
.res-link { display: flex; flex-direction: column; gap: var(--space-2); }
.res-link .res-hint { margin-top: 0; }

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
  border-radius: var(--radius-sm);
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
}
.res-icon--minutes, .res-icon--revised, .res-icon--markdown { background: color-mix(in srgb, var(--link-color) 12%, transparent); color: var(--link-color); }
.res-icon--transcript { background: color-mix(in srgb, var(--color-purple) 12%, transparent); color: var(--color-purple); }
.res-icon--audio { background: color-mix(in srgb, var(--color-green) 12%, transparent); color: var(--color-green); }
.res-icon--image { background: color-mix(in srgb, var(--color-orange) 12%, transparent); color: var(--color-orange); }
.res-icon--dir { background: color-mix(in srgb, var(--color-orange) 12%, transparent); color: var(--color-orange); }

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
  transform: translateX(3px);
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
