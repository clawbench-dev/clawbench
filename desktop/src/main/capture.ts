/**
 * Screen capture plumbing for the live share feature (Electron main process).
 *
 * Two things live here:
 *
 *   1. `registerCaptureHandlers` — the IPC the renderer uses to list sources,
 *      declare which one to capture, and toggle background throttling.
 *   2. `installDisplayMediaHandler` — the `setDisplayMediaRequestHandler` that
 *      actually answers `getDisplayMedia`.
 *
 * The one-shot pending source is the security boundary: `getDisplayMedia` in
 * Electron never shows a picker of its own, so without a declared source the
 * handler refuses. The pending value is cleared the moment it is consumed, which
 * means a page that later calls `getDisplayMedia` on its own gets nothing rather
 * than a silent capture of whatever was chosen before.
 */
import { desktopCapturer, ipcMain, session, type DesktopCapturerSource, type WebContents } from 'electron'
import { record } from './clientLog'

const TAG = 'Capture'

/** A source the renderer can pick, serialized for the bridge. */
export interface CaptureSourceInfo {
  id: string
  name: string
  kind: 'screen' | 'window'
  /** PNG data URL, small; '' when thumbnails were not requested. */
  thumbnail: string
}

/** What the renderer declared it wants to capture next. */
interface PendingCapture {
  sourceId: string
  systemAudio: boolean
}

let pending: PendingCapture | null = null

/** Thumbnail size for the picker. Small on purpose: this crosses IPC as base64. */
const THUMB_SIZE = { width: 320, height: 180 }

/**
 * System audio (loopback) is Windows-only. Electron's typings say so explicitly
 * for `Streams.audio: 'loopback'`; on other platforms the option is ignored or
 * rejected, so the UI must not offer it.
 */
export function audioCaptureSupport(): { systemAudio: boolean; reason: string } {
  if (process.platform === 'win32') return { systemAudio: true, reason: '' }
  if (process.platform === 'darwin') {
    return { systemAudio: false, reason: 'macOS cannot capture system audio; only this app\'s audio is available' }
  }
  return { systemAudio: false, reason: 'system audio capture is only supported on Windows' }
}

/** List screens and windows for the picker. */
export async function listCaptureSources(): Promise<CaptureSourceInfo[]> {
  const sources = await desktopCapturer.getSources({
    types: ['screen', 'window'],
    thumbnailSize: THUMB_SIZE,
    fetchWindowIcons: false,
  })
  return sources.map((s: DesktopCapturerSource) => ({
    id: s.id,
    name: s.name,
    // `screen:` ids are monitors; everything else is a window.
    kind: s.id.startsWith('screen:') ? 'screen' : 'window',
    thumbnail: s.thumbnail.isEmpty() ? '' : s.thumbnail.toDataURL(),
  }))
}

/**
 * Register the IPC the renderer uses.
 *
 * `setPublishingActive` toggles background throttling on the window that owns
 * the request. Chromium throttles timers and rAF in a minimized window, which
 * stalls the encoder and makes the stream freeze for every viewer; disabling it
 * is scoped to the publishing window and restored on stop.
 */
export function registerCaptureHandlers(getWindow: () => WebContents | null): void {
  ipcMain.handle('native:list-capture-sources', async () => {
    try {
      return JSON.stringify(await listCaptureSources())
    } catch (e) {
      record('W', TAG, `listCaptureSources failed: ${String(e)}`)
      return '[]'
    }
  })

  ipcMain.handle('native:set-pending-capture-source', (_e, sourceId: string, systemAudio: boolean) => {
    if (typeof sourceId !== 'string' || !sourceId) {
      // Clearing is a legitimate call: the renderer abandons a start.
      pending = null
      return
    }
    pending = { sourceId, systemAudio: systemAudio === true }
  })

  ipcMain.handle('native:get-audio-capture-support', () => JSON.stringify(audioCaptureSupport()))

  ipcMain.handle('native:set-publishing-active', (_e, active: boolean) => {
    const wc = getWindow()
    if (!wc) return
    try {
      wc.setBackgroundThrottling(!(active === true))
    } catch (e) {
      record('W', TAG, `setBackgroundThrottling failed: ${String(e)}`)
    }
  })
}

/**
 * Decide what to answer a getDisplayMedia request with.
 *
 * Extracted from the handler so the decision is testable without faking
 * Electron: it takes the pending declaration, whether the platform supports
 * loopback, and the available sources, and returns either an empty answer (refuse)
 * or the chosen source plus whether to attach system audio.
 */
export function decideCapture(
  choice: PendingCapture | null,
  audioRequested: boolean,
  platformSupportsLoopback: boolean,
  available: Array<{ id: string }>,
): { sourceId: string; withAudio: boolean } | null {
  if (!choice) return null
  if (!available.some((s) => s.id === choice.sourceId)) return null
  // Only forward loopback audio when the renderer asked, the platform allows it,
  // and the request actually wanted audio. Passing 'loopback' where it is
  // unsupported fails the whole capture, not just the audio track.
  const withAudio = audioRequested && choice.systemAudio && platformSupportsLoopback
  return { sourceId: choice.sourceId, withAudio }
}

/** Test seam: read the pending declaration. */
export function _pendingCaptureForTest(): PendingCapture | null {
  return pending
}

/** Test seam: declare a pending source without going through IPC. */
export function _setPendingCaptureForTest(v: PendingCapture | null): void {
  pending = v
}

/** Test seam: reset module state between tests. */
export function _resetCaptureForTest(): void {
  pending = null
}

/**
 * Install the display-media handler on the default session.
 *
 * Must be called after `app.whenReady()` (the session does not exist before).
 * The handler answers with the declared source, or with nothing — an empty
 * callback makes `getDisplayMedia` reject, which is the correct outcome for an
 * undeclared request.
 */
export function installDisplayMediaHandler(): void {
  session.defaultSession.setDisplayMediaRequestHandler(
    (request, callback) => {
      const choice = pending
      // One-shot: consume before answering so a concurrent request cannot reuse
      // the same declaration.
      pending = null

      if (!choice) {
        record('W', TAG, 'getDisplayMedia without a declared source was refused')
        callback({})
        return
      }

      void desktopCapturer
        .getSources({ types: ['screen', 'window'], thumbnailSize: { width: 0, height: 0 } })
        .then((sources) => {
          const decision = decideCapture(
            choice,
            request.audioRequested,
            audioCaptureSupport().systemAudio,
            sources,
          )
          if (!decision) {
            record('W', TAG, `declared source ${choice.sourceId} is unavailable`)
            callback({})
            return
          }
          const match = sources.find((s) => s.id === decision.sourceId)!
          callback({
            video: match,
            // Cast: the typings accept the literal, but the union also allows a
            // WebFrameMain, so a bare string needs the narrower type.
            audio: decision.withAudio ? ('loopback' as const) : undefined,
          })
        })
        .catch((e) => {
          record('E', TAG, `getSources failed: ${String(e)}`)
          callback({})
        })
    },
    { useSystemPicker: false },
  )
}
