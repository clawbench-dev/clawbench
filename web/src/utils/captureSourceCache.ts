/**
 * The screen / window list the OS last handed over.
 *
 * Enumerating is not a free question: on Wayland the request itself raises the
 * desktop portal's "what do you want to share" dialog, which is why the list is
 * only asked for when the host clicks 选择共享画面 (87afcc9e5). The answer is kept
 * here, in module scope, because the two screens that ask it — 创建会议 and the
 * running meeting's 会议管理 tab — are separate components:
 *
 * - starting a meeting right after picking a source must not collapse the grid
 *   back to a single "选择共享画面" tile: the stage never enumerated, so it would
 *   otherwise have nothing to draw and the host had to ask a second time;
 * - a remount (the panel is re-created when the project changes) must not throw
 *   the tiles away either — the meeting keeps running, so its grid has to come
 *   back as the host left it, without raising the OS dialog again.
 *
 * The cached list can be stale (a window closed between visits). Clicking such
 * a tile fails with the usual capture error, and the next click on
 * 选择共享画面 replaces the list — the alternative, re-asking on every mount,
 * is the dialog this cache exists to avoid.
 */
import type { CaptureSource } from '@/composables/useLiveShare'

let lastList: CaptureSource[] | null = null

/** Remember what the OS answered. An empty list is a real answer, not "unknown". */
export function rememberCaptureSources(list: CaptureSource[]): void {
  lastList = list.map((source) => ({ ...source }))
}

/** The last answer, or null when the OS has not been asked in this session. */
export function recallCaptureSources(): CaptureSource[] | null {
  return lastList ? lastList.map((source) => ({ ...source })) : null
}

/** Test-only: module state outlives a mounted panel, so each test starts clean. */
export function _resetCaptureSources(): void {
  lastList = null
}
