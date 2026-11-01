/**
 * The meeting resource list: the meeting folder on disk + the project files the
 * minutes reference.
 *
 * Folder listing reuses `/api/dir` (the file manager's own endpoint) rather
 * than adding a meeting-specific one: the notes folder lives inside the
 * project, and the endpoint already returns name/type/size/modified. The
 * referenced half reuses `buildMarkdownPreviewDom` — the same pipeline that
 * renders the minutes — and keeps only paths that exist as files.
 *
 * Audio deserves a note: before the merge runs, `audio/` holds one segment per
 * ten-to-twenty seconds, so a long meeting can have dozens of entries. The list
 * is grouped and sorted; the panel owns the height/scroll.
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { buildMarkdownPreviewDom } from './useMarkdownRenderPipeline.ts'
import { toProjectRelative, baseName, normalizeSlashes } from '@/utils/path.ts'
import { store } from '@/stores/app.ts'
import { appLog } from '@/utils/appLog.ts'
import type { MeetingRecord } from './useMeetingHistory.ts'

const TAG = 'MeetingResources'

export type MeetingResourceKind = 'minutes' | 'transcript' | 'revised' | 'markdown' | 'audio' | 'image' | 'code' | 'file' | 'dir'

export interface MeetingResourceItem {
  /** Stable list key (path + group). */
  key: string
  name: string
  /** Path handed to openFilePath / attachments (project-relative or absolute). */
  path: string
  /** Human-readable path, relative to the project root when possible. */
  displayPath: string
  kind: MeetingResourceKind
  /** Short label from the meeting's own vocabulary (纪要 / 原始转写 / 整场录音). */
  badge?: string
  /** "Markdown" / "WebM" / "Go" — the file's own type. */
  typeLabel: string
  size?: number
  modified?: string
  isDir: boolean
}

export interface MeetingResourceGroup {
  key: 'folder' | 'audio' | 'referenced'
  title: string
  /** Longer explanation, shown under the group label when present. */
  hint?: string
  items: MeetingResourceItem[]
}

interface DirEntry {
  name: string
  type: 'dir' | 'file' | 'image'
  size?: number
  modified?: string
}

const AUDIO_EXT = new Set(['webm', 'mp3', 'm4a', 'wav', 'ogg', 'opus', 'aac', 'flac'])
const IMAGE_EXT = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'avif'])
const CODE_EXT = new Set(['go', 'ts', 'tsx', 'js', 'jsx', 'vue', 'py', 'rs', 'java', 'c', 'cc', 'cpp', 'h', 'hpp', 'rb', 'php', 'sh', 'css', 'scss', 'html', 'json', 'yaml', 'yml', 'toml'])

function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.')
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : ''
}

function kindOf(name: string, isDir: boolean): MeetingResourceKind {
  if (isDir) return 'dir'
  if (name === 'minutes.md') return 'minutes'
  if (name === 'transcript.vtt') return 'transcript'
  if (name === 'transcript-revised.md') return 'revised'
  const ext = extensionOf(name)
  if (AUDIO_EXT.has(ext)) return 'audio'
  if (IMAGE_EXT.has(ext)) return 'image'
  if (ext === 'md') return 'markdown'
  if (ext === 'vtt' || CODE_EXT.has(ext)) return 'code'
  return 'file'
}

function typeLabelOf(name: string, isDir: boolean, fallback: string): string {
  if (isDir) return fallback
  const ext = extensionOf(name)
  const known: Record<string, string> = {
    md: 'Markdown',
    vtt: 'WebVTT',
    webm: 'WebM',
    go: 'Go',
    ts: 'TypeScript',
    tsx: 'TypeScript',
    js: 'JavaScript',
    vue: 'Vue',
    json: 'JSON',
    yaml: 'YAML',
    yml: 'YAML',
    py: 'Python',
    rs: 'Rust',
    sh: 'Shell',
  }
  return known[ext] ?? (ext ? ext.toUpperCase() : fallback)
}

/** Root files in reading order: the three artifacts first, then the rest. */
const ROOT_ORDER = ['minutes.md', 'transcript.vtt', 'transcript-revised.md']
function sortRoot(a: MeetingResourceItem, b: MeetingResourceItem): number {
  const ai = ROOT_ORDER.indexOf(a.name)
  const bi = ROOT_ORDER.indexOf(b.name)
  if (ai !== -1 || bi !== -1) {
    if (ai === -1) return 1
    if (bi === -1) return -1
    return ai - bi
  }
  return a.name.localeCompare(b.name)
}

function sortAudio(a: MeetingResourceItem, b: MeetingResourceItem): number {
  if (a.name === 'meeting.webm') return -1
  if (b.name === 'meeting.webm') return 1
  return a.name.localeCompare(b.name)
}

async function listDir(path: string): Promise<DirEntry[]> {
  try {
    const resp = await fetch(`/api/dir?path=${encodeURIComponent(path)}`)
    if (!resp.ok) return []
    const data = (await resp.json()) as { items?: DirEntry[] }
    return Array.isArray(data.items) ? data.items : []
  } catch (e) {
    appLog.w(TAG, `list ${path} failed`, e)
    return []
  }
}

/**
 * Paths the minutes mention, minus the meeting folder itself.
 *
 * `detectedPaths` already carries the resolver's decision (project-relative or
 * absolute); we only dedupe by the display form and drop the meeting's own
 * artifacts, which have their own group.
 */
function detectReferenced(
  content: string,
  minutesPath: string,
  relDir: string,
  projectRoot: string,
  homeDir: string,
): string[] {
  if (!content.trim()) return []
  const { detectedPaths } = buildMarkdownPreviewDom(
    { content, path: minutesPath, projectRoot, homeDir },
    { sanitize: true },
  )
  const seen = new Set<string>()
  const out: string[] = []
  for (const path of detectedPaths) {
    const rel = normalizeSlashes(toProjectRelative(path, projectRoot))
    if (!rel) continue
    if (relDir && (rel === relDir || rel.startsWith(relDir + '/'))) continue
    if (seen.has(rel)) continue
    seen.add(rel)
    out.push(path)
  }
  return out
}

async function keepExistingFiles(paths: string[]): Promise<string[]> {
  if (paths.length === 0) return []
  try {
    const resp = await fetch('/api/file/batch-exists', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths }),
    })
    if (!resp.ok) return []
    const data = (await resp.json()) as { results?: Record<string, string> }
    const results = data.results ?? {}
    return paths.filter((path) => results[path] === 'file')
  } catch (e) {
    appLog.w(TAG, 'batch-exists failed', e)
    return []
  }
}

export function useMeetingResources(options: {
  record: () => MeetingRecord | null
  minutesText: () => string
  minutesPath: () => string
}) {
  const { t } = useI18n()
  // The overloaded i18n `t` is not assignable to a plain `(key) => string`;
  // narrow it once for the helpers below.
  const translate = (key: string): string => t(key)
  const loading = ref(false)
  const error = ref('')
  const groups = ref<MeetingResourceGroup[]>([])
  let loadSeq = 0

  const total = computed(() => groups.value.reduce((n, group) => n + group.items.length, 0))

  async function load(): Promise<void> {
    const record = options.record()
    const seq = ++loadSeq
    if (!record) {
      groups.value = []
      return
    }
    const projectRoot = store.state.projectRoot ?? ''
    const homeDir = store.state.homeDir ?? ''
    const relDir = record.dirPath ? normalizeSlashes(toProjectRelative(record.dirPath, projectRoot)) : ''
    loading.value = true
    error.value = ''
    try {
      const folderItems: MeetingResourceItem[] = []
      const audioItems: MeetingResourceItem[] = []

      if (relDir) {
        const rootEntries = await listDir(relDir)
        if (seq !== loadSeq) return
        for (const entry of rootEntries) {
          if (entry.type === 'dir') {
            if (entry.name === 'audio') continue
            // A stray subdirectory is still a resource; it opens the manager.
            folderItems.push({
              key: `folder:${relDir}/${entry.name}`,
              name: entry.name,
              path: `${relDir}/${entry.name}`,
              displayPath: `${relDir}/${entry.name}`,
              kind: 'dir',
              typeLabel: t('meeting.resources.folder'),
              modified: entry.modified,
              isDir: true,
            })
            continue
          }
          folderItems.push({
            key: `folder:${relDir}/${entry.name}`,
            name: entry.name,
            path: `${relDir}/${entry.name}`,
            displayPath: `${relDir}/${entry.name}`,
            kind: kindOf(entry.name, false),
            badge: badgeFor(entry.name, translate),
            typeLabel: typeLabelOf(entry.name, false, t('meeting.resources.file')),
            size: entry.size,
            modified: entry.modified,
            isDir: false,
          })
        }

        if (rootEntries.some((entry) => entry.type === 'dir' && entry.name === 'audio')) {
          const audioEntries = await listDir(`${relDir}/audio`)
          if (seq !== loadSeq) return
          for (const entry of audioEntries) {
            if (entry.type === 'dir') continue
            audioItems.push({
              key: `audio:${relDir}/audio/${entry.name}`,
              name: entry.name,
              path: `${relDir}/audio/${entry.name}`,
              displayPath: `audio/${entry.name}`,
              kind: 'audio',
              badge: badgeFor(entry.name, translate),
              typeLabel: typeLabelOf(entry.name, false, t('meeting.resources.file')),
              size: entry.size,
              modified: entry.modified,
              isDir: false,
            })
          }
          audioItems.sort(sortAudio)
        }
      }

      const referencedPaths = detectReferenced(
        options.minutesText(),
        options.minutesPath(),
        relDir,
        projectRoot,
        homeDir,
      )
      const existing = await keepExistingFiles(referencedPaths)
      if (seq !== loadSeq) return

      const referencedItems: MeetingResourceItem[] = existing
        .map((path) => {
          const displayPath = normalizeSlashes(toProjectRelative(path, projectRoot))
          const name = baseName(path)
          return {
            key: `ref:${displayPath || path}`,
            name,
            path,
            displayPath,
            kind: kindOf(name, false),
            typeLabel: typeLabelOf(name, false, t('meeting.resources.file')),
            isDir: false,
          }
        })
        .sort((a, b) => a.displayPath.localeCompare(b.displayPath))

      folderItems.sort(sortRoot)
      const next: MeetingResourceGroup[] = []
      if (folderItems.length) {
        next.push({ key: 'folder', title: t('meeting.resources.groupFolder'), items: folderItems })
      }
      if (audioItems.length) {
        next.push({ key: 'audio', title: 'audio/', items: audioItems })
      }
      if (referencedItems.length) {
        next.push({
          key: 'referenced',
          title: t('meeting.resources.groupReferenced'),
          hint: t('meeting.resources.referencedHint'),
          items: referencedItems,
        })
      }
      groups.value = next
    } catch (e) {
      appLog.w(TAG, 'load failed', e)
      if (seq === loadSeq) error.value = String(e)
    } finally {
      if (seq === loadSeq) loading.value = false
    }
  }

  watch(
    () => [options.record()?.token, options.record()?.dirPath, options.minutesText(), options.minutesPath()] as const,
    () => { void load() },
    { immediate: true },
  )

  return { loading, error, groups, total, reload: load }
}

function badgeFor(name: string, t: (key: string) => string): string | undefined {
  if (name === 'minutes.md') return t('liveShare.notes.artifactMinutes')
  if (name === 'transcript.vtt') return t('liveShare.notes.artifactTranscript')
  if (name === 'transcript-revised.md') return t('liveShare.notes.artifactRevised')
  if (name === 'meeting.webm') return t('liveShare.notes.artifactAudioMerged')
  if (AUDIO_EXT.has(extensionOf(name))) return t('meeting.resources.audioChannel')
  return undefined
}
