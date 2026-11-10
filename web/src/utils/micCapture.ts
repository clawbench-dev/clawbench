/**
 * Microphone acquisition shared by every capture path (chat dictation and AI
 * 听记).
 *
 * Both callers open the same device the same way, and both hit the same two
 * failure modes — the one thing that must not be duplicated is the knowledge of
 * how to tell them apart. The caller keeps its own failure policy: dictation
 * aborts with a toast, a live share degrades to "no local channel" and keeps
 * going.
 */
import { appLog } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'

/** Why a microphone could not be opened, as far as the user is concerned. */
export type MicErrorKind =
  /** The permission gate refused us (`NotAllowedError`). */
  | 'denied'
  /** Permission passed but the device did not start (busy, blocked, missing). */
  | 'unavailable'

/**
 * Classifies a `getUserMedia` failure.
 *
 * Only `NotAllowedError` means "the user or the browser said no". Everything
 * else — `NotReadableError`, `NotFoundError`, `OverconstrainedError` — means the
 * OS let us through but the device did not start, so telling the user to grant
 * permission would send them the wrong way.
 */
export function classifyMicError(e: unknown): MicErrorKind {
  return (e as { name?: string } | null)?.name === 'NotAllowedError' ? 'denied' : 'unavailable'
}

/** Formats a DOMException for a log line; JSON.stringify yields "{}" for these. */
export function describeMicError(e: unknown): string {
  const err = e as { name?: string; message?: string } | null
  return `name=${err?.name ?? 'unknown'} message=${err?.message ?? String(e)}`
}

/**
 * Opens the microphone.
 *
 * `NotReadableError` means the permission gate passed but the audio device
 * refused to start (busy, or the HAL was not ready yet). One short retry covers
 * the transient case; a persistent failure is a system-level block and must not
 * be retried forever.
 */
export async function acquireMic(
  constraints: MediaStreamConstraints = { audio: true },
  /**
   * The capture call to use. Only tests pass one — the viewer page injects its
   * own `getUserMedia` seam, and routing it through here keeps ONE retry policy
   * instead of a second copy that could drift.
   */
  gum: (c: MediaStreamConstraints) => Promise<MediaStream> = (c) =>
    navigator.mediaDevices.getUserMedia(c),
): Promise<MediaStream> {
  try {
    return await gum(constraints)
  } catch (e) {
    if ((e as { name?: string } | null)?.name !== 'NotReadableError') throw e
    await new Promise((resolve) => setTimeout(resolve, 400))
    return gum(constraints)
  }
}

/**
 * The origin's microphone permission, as the Permissions API reports it.
 *
 * `unknown` means the API cannot answer (Firefox has no 'microphone' entry,
 * older WebViews lack `permissions` at all) — callers must treat that as "may
 * still ask", never as "denied". It lives here, next to the other microphone
 * helpers, because a DOM type assertion inside a `.vue` file trips the
 * `no-undef` lint rule.
 */
export async function mediaPermission(
  name: 'microphone' | 'camera',
): Promise<'granted' | 'denied' | 'unknown'> {
  try {
    const status = await navigator.permissions?.query({ name: name as PermissionName })
    return (status?.state as 'granted' | 'denied' | undefined) ?? 'unknown'
  } catch {
    return 'unknown'
  }
}

/** @see mediaPermission */
export function microphonePermission(): Promise<'granted' | 'denied' | 'unknown'> {
  return mediaPermission('microphone')
}

/**
 * Logs the environment a failed `getUserMedia` ran in.
 *
 * Without this the only trace is `getUserMedia failed {}` — a DOMException has
 * no enumerable own properties, so the error name and the installed app version
 * are both lost, and "no permission" cannot be told apart from "the device is
 * blocked by the OS".
 */
export async function logMicEnv(tag: string): Promise<void> {
  let appVersion = 'n/a'
  const native = getNative()
  if (native?.getAppVersion) {
    try {
      appVersion = (await native.getAppVersion()) ?? 'n/a'
    } catch { /* bridge unavailable */ }
  }

  // Permissions API state tells "the browser thinks it may capture" apart from
  // "the device refuses to start" — the two failures need different fixes.
  let permState: string
  try {
    const status = await navigator.permissions?.query({ name: 'microphone' as PermissionName })
    permState = status?.state ?? 'unknown'
  } catch {
    permState = 'error'
  }

  // Android only: record natively to find out whether the OS lets this app use
  // the mic at all. A WebView-side failure with a working native probe means
  // the fault is inside Chromium's audio stack, not the permission.
  let nativeMic = 'n/a'
  if (native?.probeMicrophone) {
    try {
      nativeMic = (await native.probeMicrophone()) ?? 'n/a'
    } catch {
      nativeMic = 'error'
    }
  }

  // Enumerate inputs: 0 devices means the OS exposes no microphone; missing
  // labels means capture permission is not really in effect even though the
  // settings page shows it granted.
  let inputs: string
  try {
    const devices = await navigator.mediaDevices.enumerateDevices()
    const audioInputs = devices.filter((d) => d.kind === 'audioinput')
    inputs = `${audioInputs.length}[${audioInputs.map((d) => d.label || '<no-label>').join('|')}]`
  } catch {
    inputs = 'error'
  }

  appLog.i(
    tag,
    `env secure=${window.isSecureContext} proto=${window.location.protocol} host=${window.location.host} ` +
      `mediaDevices=${typeof navigator.mediaDevices} permission=${permState} inputs=${inputs} ` +
      `nativeMic=${nativeMic} app=${appVersion}`,
  )
}
