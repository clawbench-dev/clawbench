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
import { desktopCapturer, ipcMain, session, type DesktopCapturerSource, type Streams, type WebContents } from 'electron'
import { record } from './clientLog'

const TAG = 'Capture'

/** One line for a thrown value; Electron's errors stringify to "{}" otherwise. */
function describe(e: unknown): string {
  if (e instanceof Error) return `${e.name}: ${e.message}`
  return String(e)
}

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

/**
 * The listing the picker last drew, and when it was taken.
 *
 * The handler used to re-enumerate in order to answer `getDisplayMedia`, and on
 * Wayland enumerating screens IS the system's "what do you want to share"
 * dialog — so choosing a screen and pressing start asked the user twice, and the
 * second answer replaced the first. Remembering the listing the picker showed
 * lets the handler answer from the source the user actually picked.
 */
let lastListing: { sources: DesktopCapturerSource[]; at: number } | null = null

/**
 * How long a remembered listing may answer a capture request.
 *
 * Long enough for a form the user left open while typing, short enough that a
 * window closed in the meantime is far more likely to be re-listed than
 * silently captured. Past this the handler falls back to a fresh enumeration.
 */
const LISTING_TTL_MS = 5 * 60 * 1000

/**
 * Display the current capture is showing, as reported by desktopCapturer.
 *
 * The annotation overlay has to cover THAT screen, not the primary one — a
 * two-monitor host would otherwise draw on the wrong desktop. Empty when the
 * platform does not say (Wayland hands out a nameless placeholder).
 *
 * Cleared when publishing stops. Nothing else ever overwrites it, so a stale
 * value would survive the session and send the next annotation overlay to a
 * screen that is no longer being shared (visible only on a multi-monitor host,
 * as "the ink went to the monitor I cannot see").
 */
let capturedDisplayId = ''

/**
 * Whether the renderer says it is publishing right now.
 *
 * `setDisplayMediaRequestHandler` gives no handle on the resulting stream, so
 * the stream's end is not observable here. The publishing toggle the renderer
 * already sends (it exists to keep background throttling off) is the signal
 * that the session started and stopped.
 */
let publishing = false

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

/**
 * Sources the compositor will not describe, only stand in for.
 *
 * A Wayland client cannot enumerate other applications' windows, so Electron
 * answers `getSources` with a single placeholder that has no name and no
 * thumbnail. Its id is a per-call index (`window:1:0:s`, then `window:2:0:s`, …)
 * rather than a handle, so it cannot be used to re-identify the same source
 * later. The real choice happens in the desktop portal's own dialog when the
 * stream is requested, which is why the id carries no meaning there.
 */
export function arePlaceholderSources(sources: Array<{ name: string }>): boolean {
  return sources.length > 0 && sources.every((s) => !s.name)
}

/** List screens and windows for the picker. */
export async function listCaptureSources(): Promise<CaptureSourceInfo[]> {
  const query = {
    types: ['screen', 'window'] as Array<'screen' | 'window'>,
    thumbnailSize: THUMB_SIZE,
    fetchWindowIcons: false,
  }
  let sources = await desktopCapturer.getSources(query)
  if (sources.length === 0) {
    // Window enumeration can fail on its own — an X11 session without a window
    // manager, or a locked-down desktop — and Electron then answers the WHOLE
    // combined request with an empty list. The picker would say "no sources"
    // while screens are perfectly capturable, so ask for the subset instead.
    // Screens-only cannot mask a real permission problem: it is strictly less.
    record('W', TAG, 'combined source query was empty; retrying with screens only')
    sources = await desktopCapturer.getSources({ ...query, types: ['screen'] })
  }
  // Remember what the picker drew: the capture request that follows must answer
  // with one of these, and re-enumerating would raise the system picker again.
  lastListing = { sources, at: Date.now() }
  return sources.map((s: DesktopCapturerSource) => ({
    id: s.id,
    name: s.name,
    // `screen:` ids are monitors; everything else is a window. Placeholders are
    // screens in practice (the portal asks which one), so they read as screens.
    kind: s.id.startsWith('screen:') || !s.name ? 'screen' : 'window',
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
    const isPublishing = active === true
    publishing = isPublishing
    // Stopping the share ends the session: forget which screen it was, so the
    // annotation overlay falls back to the primary display instead of covering
    // whatever monitor happened to be shared last.
    if (!isPublishing) capturedDisplayId = ''
    const wc = getWindow()
    if (!wc) return
    try {
      wc.setBackgroundThrottling(!isPublishing)
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
 *
 * `volatileIds` says the source ids are per-call indexes (Wayland), so matching
 * the declared id against a fresh listing can never succeed — the listing has to
 * be trusted as-is. The declaration is still one-shot and still comes only from
 * our own picker; on Wayland the portal picks the actual screen afterwards, so
 * the id was never a handle to begin with.
 */
export function decideCapture(
  choice: PendingCapture | null,
  audioRequested: boolean,
  platformSupportsLoopback: boolean,
  available: Array<{ id: string }>,
  volatileIds = false,
): { sourceId: string; withAudio: boolean } | null {
  if (!choice) return null
  const exact = available.find((s) => s.id === choice.sourceId)
  const target = exact ?? (volatileIds ? available[0] : undefined)
  if (!target) return null
  // Only forward loopback audio when the renderer asked, the platform allows it,
  // and the request actually wanted audio. Passing 'loopback' where it is
  // unsupported fails the whole capture, not just the audio track.
  const withAudio = audioRequested && choice.systemAudio && platformSupportsLoopback
  return { sourceId: target.id, withAudio }
}

/**
 * Display id of the screen currently being captured ('' when unknown).
 *
 * Only meaningful while a share is running: after the session ends the id is
 * cleared, and a consumer that asks in between gets '' rather than the last
 * screen that happened to be shared.
 */
export function currentCaptureDisplayId(): string {
  return publishing ? capturedDisplayId : ''
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
  capturedDisplayId = ''
  publishing = false
  lastListing = null
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

      /**
       * Answer the request exactly once.
       *
       * `callback` throws when it is called a second time, and that throw used
       * to land inside the promise chain below — where the catch answered AGAIN
       * — so the only thing the log ever said was "one-time callback was called
       * more than once". That sentence describes the retry, not the failure:
       * whatever went wrong first (a conversion error, a missing source) was
       * lost, and the renderer saw a refused request with no explanation.
       */
      let answered = false
      const answer = (streams: Streams): void => {
        if (answered) return
        answered = true
        try {
          callback(streams)
        } catch (e) {
          // A conversion failure inside Electron is a real defect and must not
          // be swallowed: record it, and leave it in the terminal too — the
          // file log only exists while debug capture is switched on.
          record('E', TAG, `answering getDisplayMedia failed: ${describe(e)}`)
          console.error('[Capture] answering getDisplayMedia failed:', e)
        }
      }

      if (!choice) {
        record('W', TAG, 'getDisplayMedia without a declared source was refused')
        answer({})
        return
      }

      const streamsFor = (match: DesktopCapturerSource, withAudio: boolean): Streams => {
        // `audio` is only present when loopback was actually asked for. An
        // explicit `audio: undefined` still creates the key, and this object is
        // converted on Electron's side — a key with nothing in it is not the
        // same thing as an absent key.
        const streams: Streams = { video: match }
        if (withAudio) streams.audio = 'loopback'
        return streams
      }

      // Answer from the listing the picker just drew. Enumerating here instead
      // asks the system a second time — on Wayland that is a second "what do you
      // want to share" dialog, which is why choosing a screen and pressing start
      // used to require choosing twice.
      const remembered =
        lastListing && Date.now() - lastListing.at <= LISTING_TTL_MS ? lastListing.sources : null
      const picked = remembered?.find((s) => s.id === choice.sourceId)
      if (picked) {
        const decision = decideCapture(
          choice,
          request.audioRequested,
          audioCaptureSupport().systemAudio,
          [picked],
          false,
        )
        capturedDisplayId = picked.display_id ?? ''
        answer(streamsFor(picked, decision?.withAudio ?? false))
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
            arePlaceholderSources(sources),
          )
          if (!decision) {
            record('W', TAG, `declared source ${choice.sourceId} is unavailable`)
            answer({})
            return
          }
          const match = sources.find((s) => s.id === decision.sourceId)
          if (!match) {
            record('W', TAG, `source ${decision.sourceId} vanished between listing and answer`)
            answer({})
            return
          }
          capturedDisplayId = match.display_id ?? ''
          answer(streamsFor(match, decision.withAudio))
        })
        .catch((e) => {
          record('E', TAG, `getSources failed: ${describe(e)}`)
          console.error('[Capture] getSources failed:', e)
          answer({})
        })
    },
    { useSystemPicker: false },
  )
}
