import { createHash } from 'node:crypto'
import path from 'node:path'

/**
 * Which instance a launch belongs to.
 *
 * The desktop shell holds ONE instance per userData directory: that is the key
 * Electron's `requestSingleInstanceLock` uses, and it also holds the settings,
 * the Chromium profile (cookies, localStorage, the single-tab lock) and the
 * shell log. So "copy the installation folder and open both" only works when a
 * copied folder resolves to its own userData — otherwise the second launch
 * simply raises the first window, which is what made a copy look inert.
 *
 * The installation directory is the identity. On first run the app records
 * which directory is the primary install (`~/.clawbench-desktop/primary-install
 * .json`); every other directory is a copy and gets its own profile. The
 * distinction between a COPY and a MOVE is made from the recorded directory:
 *
 *   - recorded directory still exists  → this one is a copy
 *   - recorded directory is gone       → the installation was moved; keep its
 *                                        settings instead of starting empty
 *
 * The rule lives here, with no Electron imports, so it can be unit-tested;
 * `instance.ts` is the glue that reads argv and calls `app.setPath`.
 */

export const DEFAULT_INSTANCE = 'default'
export const INSTANCE_FLAG = '--instance'

/**
 * Instance ids end up in a directory name, so they are restricted to a safe
 * alphabet. A hand-edited command line must not be able to point userData at
 * `..` or an absolute path.
 */
const SAFE_ID = /^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$/

type PathApi = typeof path.posix

/** Paths are parsed with the rules of the platform being described. */
function apiFor(platform: string): PathApi {
  return platform === 'win32' ? path.win32 : path.posix
}

/**
 * The instance id given on the command line, or ''.
 *
 * Both spellings are accepted (`--instance work` and `--instance=work`) because
 * the flag is only ever passed by the app itself (self-upgrade handoff) and by
 * a user-made shortcut. An invalid value is ignored rather than sanitized: a
 * malformed id is a mistake, and falling back to the default instance is the
 * safe reading.
 */
export function parseInstanceFlag(argv: readonly string[]): string {
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]
    if (arg === INSTANCE_FLAG) {
      const value = argv[i + 1] ?? ''
      return SAFE_ID.test(value) ? value : ''
    }
    if (arg.startsWith(`${INSTANCE_FLAG}=`)) {
      const value = arg.slice(INSTANCE_FLAG.length + 1)
      return SAFE_ID.test(value) ? value : ''
    }
  }
  return ''
}

/**
 * Directory that identifies the installation, given the executable path.
 *
 * On macOS that is the `ClawBench.app` bundle itself — two copies usually sit
 * side by side in `/Applications`, so the directory holding them is the same
 * and cannot be the identity. For every other platform it is the directory the
 * executable sits in. A version unpacked by the self-upgrade lives in the
 * shared store and is handled separately by the caller.
 */
export function appRootFor(execPath: string, platform: string): string {
  const p = apiFor(platform)
  const normalized = p.normalize(execPath)
  if (platform === 'darwin') {
    const parts = normalized.split(p.sep)
    // .../<name>.app/Contents/MacOS/<exe> → the <name>.app bundle
    const macos = parts.length - 2
    if (parts[macos] === 'MacOS' && parts[macos - 1] === 'Contents' && parts[macos - 2]?.endsWith('.app')) {
      return parts.slice(0, macos - 1).join(p.sep) || p.sep
    }
  }
  return p.dirname(normalized)
}

/**
 * Canonical form used for comparisons and hashing.
 *
 * Windows and macOS file systems are case-insensitive by default, so the same
 * folder reached with a different case must not look like a second copy.
 */
export function normalizeRoot(root: string, platform: string): string {
  const p = apiFor(platform)
  let out = p.normalize(root)
  if (platform === 'win32' || platform === 'darwin') out = out.toLowerCase()
  if (out.length > 1 && out.endsWith(p.sep)) out = out.slice(0, -1)
  return out
}

/** Whether `child` is `parent` itself or lies below it. */
export function isInsideDir(child: string, parent: string, platform: string): boolean {
  const p = apiFor(platform)
  const c = normalizeRoot(child, platform)
  const d = normalizeRoot(parent, platform)
  return c === d || c.startsWith(d.endsWith(p.sep) ? d : d + p.sep)
}

/** Stable, filesystem-safe id for a copy. */
export function instanceIdFor(root: string, platform: string): string {
  return createHash('sha256').update(normalizeRoot(root, platform)).digest('hex').slice(0, 8)
}

/** userData directory for a profile. The default one is left untouched. */
export function profileUserData(defaultUserData: string, id: string): string {
  return id === DEFAULT_INSTANCE ? defaultUserData : `${defaultUserData}-${id}`
}

export interface InstanceInput {
  /** Id from `--instance`, or '' when the launch has none. */
  flagId: string
  /** False for a development run (`electron .`), which is never an install. */
  isPackaged: boolean
  execPath: string
  platform: string
  /** `app.getPath('userData')` BEFORE any override. */
  defaultUserData: string
  /** Shared version store (`~/.clawbench-desktop`). */
  installRoot: string
  /** Recorded primary install root, or '' when nothing has been recorded. */
  primaryRoot: string
  /** Whether the recorded root still exists on disk. */
  primaryRootExists: boolean
}

export interface InstanceProfile {
  id: string
  userData: string
  /** Installation directory this launch was resolved from. */
  root: string
  /** Non-empty when this launch should record itself as the primary install. */
  claimPrimaryRoot: string
  isCopy: boolean
}

/**
 * Decide the profile for this launch.
 *
 * Order matters: an explicit id (a self-upgrade handoff) wins, because that
 * process runs from the shared version store where the path says nothing about
 * which copy it belongs to.
 */
export function resolveInstanceProfile(input: InstanceInput): InstanceProfile {
  const root = appRootFor(input.execPath, input.platform)
  const at = (id: string): InstanceProfile => ({
    id,
    userData: profileUserData(input.defaultUserData, id),
    root,
    claimPrimaryRoot: '',
    isCopy: id !== DEFAULT_INSTANCE,
  })

  if (input.flagId) return at(input.flagId)

  // A development run is not an installation: it must never claim the primary
  // slot or fork a profile, or every `npm start` would look like a copy.
  if (!input.isPackaged) return at(DEFAULT_INSTANCE)

  // Versions unpacked by the self-upgrade live in the shared store. Starting
  // one directly (instead of through the install that handed off) is still the
  // same installation.
  if (isInsideDir(root, input.installRoot, input.platform)) return at(DEFAULT_INSTANCE)

  const primary = input.primaryRoot
  if (!primary) return { ...at(DEFAULT_INSTANCE), claimPrimaryRoot: root }
  if (normalizeRoot(primary, input.platform) === normalizeRoot(root, input.platform)) {
    return at(DEFAULT_INSTANCE)
  }
  // The recorded install is still on disk, so this directory is a COPY: give it
  // its own settings instead of fighting the original over one profile.
  if (input.primaryRootExists) return at(instanceIdFor(root, input.platform))
  // The recorded install is gone — the folder was moved, not copied. Keeping
  // the default profile is what makes the move carry the user's settings.
  return { ...at(DEFAULT_INSTANCE), claimPrimaryRoot: root }
}

/**
 * Id of the profile this process runs as. Read by `install.ts` when it starts
 * the next version: that process lives in the shared store, so it must be told
 * which copy it belongs to.
 */
let activeId: string = DEFAULT_INSTANCE

export function setActiveInstanceId(id: string): void {
  activeId = id
}

export function activeInstanceId(): string {
  return activeId
}

/** Arguments to add when starting another copy of this executable. */
export function currentInstanceArgs(): string[] {
  return activeId === DEFAULT_INSTANCE ? [] : [INSTANCE_FLAG, activeId]
}

/** Test seam: forget the id a previous test set. */
export function _resetInstanceForTests(): void {
  activeId = DEFAULT_INSTANCE
}
