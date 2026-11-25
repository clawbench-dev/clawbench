import { app } from 'electron'
import fs from 'node:fs'
import path from 'node:path'
import { installRoot } from './install'
import { record } from './clientLog'
import {
  DEFAULT_INSTANCE,
  parseInstanceFlag,
  resolveInstanceProfile,
  setActiveInstanceId,
  type InstanceProfile,
} from './instanceProfile'

/**
 * Apply the instance profile before anything else runs.
 *
 * `app.setPath('userData')` must happen before `requestSingleInstanceLock`
 * (the lock is keyed by userData) and before `initStore` (the settings file
 * lives there), which is why `index.ts` calls this at module scope, above both.
 *
 * `sessionData` is set explicitly as well. Electron keeps the Chromium profile
 * (cookies, localStorage, the single-tab Web Lock) there, and leaving it on the
 * old path would let two windows fight over one `client_id` even though their
 * settings are separate.
 *
 * Best-effort throughout: a copy that cannot read or write the primary marker
 * still starts — it just falls back to the default profile, which is exactly
 * what a single install does.
 */

const PRIMARY_FILE = 'primary-install.json'

/** Where the profile keeps its own display name (folder basename on first run). */
const PROFILE_FILE = 'instance.json'

let label = ''

function readPrimaryRoot(): string {
  try {
    const raw = fs.readFileSync(path.join(installRoot(), PRIMARY_FILE), 'utf8')
    const parsed = JSON.parse(raw) as { root?: unknown }
    return typeof parsed.root === 'string' ? parsed.root : ''
  } catch {
    return ''
  }
}

/** Record which directory is the primary install (atomic replace). */
function writePrimaryRoot(root: string): void {
  try {
    fs.mkdirSync(installRoot(), { recursive: true })
    const target = path.join(installRoot(), PRIMARY_FILE)
    const tmp = `${target}.tmp-${process.pid}`
    fs.writeFileSync(tmp, JSON.stringify({ root }), 'utf8')
    fs.renameSync(tmp, target)
  } catch (err) {
    record('W', 'Instance', `cannot record the primary install: ${(err as Error).message}`)
  }
}

/**
 * Display name for a copy, persisted in its own profile.
 *
 * The first launch names it after the folder the user copied (that is the name
 * they chose); later launches — including a self-upgrade handoff, which runs
 * from the shared version store — read it back instead of deriving a name from
 * a path that no longer identifies the copy.
 */
function profileLabel(profile: InstanceProfile): string {
  if (!profile.isCopy) return ''
  const file = path.join(profile.userData, PROFILE_FILE)
  try {
    const parsed = JSON.parse(fs.readFileSync(file, 'utf8')) as { label?: unknown }
    if (typeof parsed.label === 'string' && parsed.label) return parsed.label
  } catch {
    // First launch of this copy.
  }
  const fallback = path.basename(profile.root).replace(/\.app$/i, '') || profile.id
  try {
    fs.mkdirSync(profile.userData, { recursive: true })
    fs.writeFileSync(file, JSON.stringify({ id: profile.id, label: fallback }), 'utf8')
  } catch (err) {
    record('W', 'Instance', `cannot record the instance name: ${(err as Error).message}`)
  }
  return fallback
}

/**
 * Point Electron at the profile's directories, or stay on the default one.
 *
 * `app.setPath` throws when the directory does not exist, so it is created
 * first. A copy whose directory cannot be created (a read-only home, a full
 * disk) degrades to the default profile instead of failing to start — the user
 * loses the second window, but the app still opens and the log says why.
 */
function useProfile(profile: InstanceProfile, defaultUserData: string): InstanceProfile {
  if (profile.userData === defaultUserData) return profile
  try {
    fs.mkdirSync(profile.userData, { recursive: true })
    app.setPath('userData', profile.userData)
    app.setPath('sessionData', profile.userData)
    return profile
  } catch (err) {
    record('E', 'Instance', `cannot use ${profile.userData}: ${(err as Error).message}; staying on the default profile`)
    return { ...profile, id: DEFAULT_INSTANCE, userData: defaultUserData, isCopy: false, claimPrimaryRoot: '' }
  }
}

export function applyInstanceProfile(): InstanceProfile {
  const defaultUserData = app.getPath('userData')
  const primaryRoot = readPrimaryRoot()
  const profile = useProfile(
    resolveInstanceProfile({
      flagId: parseInstanceFlag(process.argv),
      isPackaged: app.isPackaged,
      execPath: process.execPath,
      platform: process.platform,
      defaultUserData,
      installRoot: installRoot(),
      primaryRoot,
      primaryRootExists: !!primaryRoot && fs.existsSync(primaryRoot),
    }),
    defaultUserData,
  )

  if (profile.claimPrimaryRoot) writePrimaryRoot(profile.claimPrimaryRoot)
  setActiveInstanceId(profile.id)
  label = profileLabel(profile)
  record(
    'I',
    'Instance',
    profile.isCopy
      ? `running as copy "${label}" (${profile.id}) from ${profile.root}`
      : `running as the default instance from ${profile.root}`,
  )
  return profile
}

/**
 * Name shown next to the tray counters and in the window title, or '' for the
 * default instance. Copies need this: two identical tray icons and two windows
 * with the same title are indistinguishable.
 */
export function instanceLabel(): string {
  return label
}
