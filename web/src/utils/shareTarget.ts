import { appLog } from '@/utils/appLog'

const TAG = 'ShareTarget'

/** Query parameter the share-target handler redirects back with. */
export const SHARE_TARGET_PARAM = 'share_target'

/** Companion parameter: how many files in the batch could not be stored. */
export const SHARE_TARGET_FAILED_PARAM = 'share_target_failed'

/** Failure reasons the backend can report. Anything else is treated as generic. */
export type ShareTargetFailure = 'empty' | 'save' | 'invalid'

export interface ShareTargetResult {
  /** Project-relative paths of the files the server stored. */
  paths: string[]
  /** Set when the share failed outright; `paths` is empty in that case. */
  failure?: ShareTargetFailure
  /** How many files in the batch could not be stored (0 when none failed). */
  failed: number
}

/**
 * Read and consume the result of a Web Share Target POST.
 *
 * The browser navigates a fresh document to `/?share_target=...` after the user
 * shares files into the installed app, so this runs on cold start. The value is
 * either a JSON array of stored paths or a bare failure reason.
 *
 * The parameter is stripped from the URL (replaceState, not pushState) so a
 * reload does not re-attach the same files — the files are already on disk and
 * already in the composer by then, and re-adding them would look like a
 * duplicate. Returns null when there is nothing to do.
 */
export function consumeShareTargetResult(): ShareTargetResult | null {
  if (typeof window === 'undefined') return null

  let raw: string | null
  try {
    raw = new URLSearchParams(window.location.search).get(SHARE_TARGET_PARAM)
  } catch {
    return null
  }
  if (raw === null) return null

  const failedRaw = new URLSearchParams(window.location.search).get(SHARE_TARGET_FAILED_PARAM)

  // Strip the parameters first: everything below can fail, and a failed parse
  // must not leave the value in the URL for the next load to trip over.
  stripShareTargetParam()

  const failed = parseFailedCount(failedRaw)
  const trimmed = raw.trim()
  if (trimmed === '') return { paths: [], failure: 'empty', failed: 0 }

  if (trimmed.startsWith('[')) {
    try {
      const parsed: unknown = JSON.parse(trimmed)
      if (Array.isArray(parsed)) {
        const paths = parsed.filter((p): p is string => typeof p === 'string' && p !== '')
        if (paths.length > 0) return { paths, failed }
      }
      // An array that parsed to nothing usable means the server stored nothing.
      return { paths: [], failure: 'empty', failed: 0 }
    } catch (err) {
      appLog.w(TAG, 'failed to parse shared paths', err)
      return { paths: [], failure: 'invalid', failed: 0 }
    }
  }

  if (trimmed === 'empty' || trimmed === 'save' || trimmed === 'invalid') {
    return { paths: [], failure: trimmed, failed: 0 }
  }
  return { paths: [], failure: 'invalid', failed: 0 }
}

/** Parse the failed-file count; anything unusable means "none". */
function parseFailedCount(raw: string | null): number {
  if (raw === null) return 0
  const n = Number.parseInt(raw, 10)
  return Number.isFinite(n) && n > 0 ? n : 0
}

/** Remove the share_target parameter without adding a history entry. */
function stripShareTargetParam(): void {
  try {
    const url = new URL(window.location.href)
    if (!url.searchParams.has(SHARE_TARGET_PARAM) && !url.searchParams.has(SHARE_TARGET_FAILED_PARAM)) return
    url.searchParams.delete(SHARE_TARGET_PARAM)
    url.searchParams.delete(SHARE_TARGET_FAILED_PARAM)
    const next = url.pathname + (url.searchParams.toString() ? `?${url.searchParams}` : '') + url.hash
    window.history.replaceState(window.history.state, '', next)
  } catch (err) {
    appLog.w(TAG, 'failed to strip share_target param', err)
  }
}

/** Toast variant the caller feeds straight into `toast.show`. */
export interface ShareTargetToastVariant {
  icon: 'attach' | 'warning'
  type: 'success' | 'info' | 'error'
  duration: number
}

export interface ShareTargetStaging {
  toastKey: string
  toastParams: Record<string, unknown>
  variant: ShareTargetToastVariant
}

/**
 * Decide what to attach and what to tell the user, given a parsed share result.
 *
 * Kept here rather than inline in App.vue so the decision table is testable:
 * `attach` is the only side effect (it returns false for an already-attached
 * path), and the rest is pure. Every branch must produce a toast — the share
 * happened in another app, so with no visible feedback the user cannot tell a
 * success from a silent no-op.
 */
export function stageShareTargetResult(
  result: ShareTargetResult,
  attach: (path: string) => boolean,
): ShareTargetStaging {
  if (result.failure) {
    // The files never made it to disk, so there is nothing to attach.
    const toastKey = result.failure === 'invalid'
      ? 'chat.attach.shareTargetInvalid'
      : result.failure === 'save'
        ? 'chat.attach.shareTargetSaveFailed'
        : 'chat.attach.shareTargetEmpty'
    return { toastKey, toastParams: {}, variant: { icon: 'warning', type: 'error', duration: 3000 } }
  }

  const added = result.paths.filter((p) => attach(p)).length
  if (added === 0) {
    if (result.failed > 0) {
      // Nothing was attached AND some files were dropped: reporting only
      // "already attached" would hide the failure entirely.
      return {
        toastKey: 'chat.attach.shareTargetPartial',
        toastParams: { n: 0, failed: result.failed },
        variant: { icon: 'warning', type: 'error', duration: 3000 },
      }
    }
    // Already attached (e.g. the same file shared twice in a row).
    return {
      toastKey: 'chat.attach.alreadyAttached',
      toastParams: {},
      variant: { icon: 'attach', type: 'info', duration: 1500 },
    }
  }
  if (result.failed > 0) {
    // A partial batch must not read as full success — the user would assume
    // every shared file made it into the composer.
    return {
      toastKey: 'chat.attach.shareTargetPartial',
      toastParams: { n: added, failed: result.failed },
      variant: { icon: 'warning', type: 'error', duration: 3000 },
    }
  }
  return {
    toastKey: 'chat.attach.shareTargetAdded',
    toastParams: { n: added },
    variant: { icon: 'attach', type: 'success', duration: 2000 },
  }
}
