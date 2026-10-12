/**
 * Panel/layout UI state — the single registry of what gets persisted to
 * localStorage, where, and in what encoding.
 *
 * Why this exists: the app persists several independent "panel" states (left
 * column collapse, chat collapse, split ratio, session sidebar, TOC dock, file
 * manager preview ratio, and the left-column tab). Each grew its own key name,
 * its own scope decision (global vs per-project) and its own read/write code,
 * in five different naming styles. There was no rule for where a new state
 * should go, so the same "panel open/closed" concept ended up global in one
 * module and per-project in another.
 *
 * This module centralizes only the PERSISTENCE semantics — key composition,
 * scope, encoding, defaults, migration. Business logic stays in its own module:
 * the mutual-exclusion guards in useWideScreenLayout, the edit-mode hiding in
 * useTocDockPreference and the width clamping in useSessionSidebar are all
 * untouched. Callers keep their Vue refs and simply read/write through here.
 *
 * LEAFLET: this file must not import any composable (useWideScreenLayout,
 * useProjectPanel, useTocDockPreference, useSessionSidebar). Those import THIS
 * module, so the reverse edge would create a cycle. Only `utils/splitRatio` is
 * imported, and that is itself import-free.
 */
import { DEFAULT_RATIO } from '@/utils/splitRatio'

/** Where a state lives: one shared key, or one key per project root. */
export type Scope = 'global' | 'project'

export interface PanelEntry<T> {
  /** Registry id, used as the argument to readPanelState/writePanelState. */
  id: PanelId
  /** Logical key fragment; the physical key is derived from it (see physicalKey). */
  key: string
  scope: Scope
  /** Value returned when nothing is stored, or storage is unreadable. */
  default: T
  /** Decode a stored raw string. Receives `null` when the key is absent. */
  parse(raw: string | null): T
  /** Encode a value for storage. */
  serialize(value: T): string
}

/**
 * The registered ids. A union rather than `string` so a typo like
 * `readPanelState('tocDockOpn')` is a compile error instead of a silent
 * `undefined` — the registry's whole premise is that this one list owns the
 * ids. `panelState.test.ts` pins it against PANEL_ENTRIES so the two cannot
 * drift.
 */
export type PanelId =
  | 'leftCollapsed'
  | 'chatCollapsed'
  | 'splitRatio'
  | 'sessionSidebar'
  | 'tocDockOpen'
  | 'tocDockWidth'
  | 'tocDockSide'
  | 'fmPreviewRatio'
  | 'leftTab'

/** Physical key prefix. Every panel-state key starts with this. */
export const PANEL_KEY_PREFIX = 'clawbench-panel.'

/**
 * Default state of the wide-screen left column: hidden.
 *
 * A fresh view opens on chat alone; the user opts into the left column by
 * clicking a dock tab. This lives here (not in useWideScreenLayout) so the
 * registry owns every default and no composable needs to import another just
 * for a constant.
 */
export const DEFAULT_LEFT_COLLAPSED = true

export const DEFAULT_SESSION_SIDEBAR_WIDTH = 280
export const DEFAULT_TOC_DOCK_WIDTH = 260
export const DEFAULT_TOC_DOCK_SIDE = 'right'
export const DEFAULT_FM_PREVIEW_RATIO = 0.6

export interface SessionSidebarState {
  open: boolean
  width: number
}

// ── Encoding helpers ────────────────────────────────────────────────────────
// Each returns the parse/serialize pair for one value shape, so the entry table
// below stays declarative and the encodings are defined exactly once.

function boolCodec(def: boolean): Pick<PanelEntry<boolean>, 'parse' | 'serialize'> {
  return {
    // Only the literal '1' is true. Note this means a corrupt value parses to
    // `false`, NOT to `def` — which is invisible for a default-false entry but
    // observable for `leftCollapsed` (default true): garbage yields *expanded*.
    // That matches the pre-registry code (`stored === '1'`), so it is preserved
    // deliberately rather than "fixed".
    parse: (raw) => (raw === null ? def : raw === '1'),
    serialize: (v) => (v ? '1' : '0'),
  }
}

function numberCodec(def: number): Pick<PanelEntry<number>, 'parse' | 'serialize'> {
  return {
    parse: (raw) => {
      if (raw === null) return def
      const n = Number(raw)
      return Number.isFinite(n) ? n : def
    },
    serialize: (v) => String(v),
  }
}

function stringCodec(def: string | null): Pick<PanelEntry<string | null>, 'parse' | 'serialize'> {
  return {
    // An empty string is treated as "absent" — writing null serializes to '',
    // and reading that back must yield the default rather than '' (which would
    // look like a remembered-but-unknown tab).
    parse: (raw) => (raw === null || raw === '' ? def : raw),
    serialize: (v) => v ?? '',
  }
}

function sessionSidebarCodec(def: SessionSidebarState): Pick<PanelEntry<SessionSidebarState>, 'parse' | 'serialize'> {
  // Always hand back a fresh object: `def` is a module-level constant, and
  // returning it directly would let a caller's mutation leak into every future
  // default read.
  const copy = (v: SessionSidebarState): SessionSidebarState => ({ open: v.open, width: v.width })
  return {
    parse: (raw) => {
      if (raw === null) return copy(def)
      try {
        const parsed = JSON.parse(raw)
        if (parsed && typeof parsed === 'object') {
          return {
            open: parsed.open === true,
            width: Number(parsed.width) || def.width,
          }
        }
      } catch {
        // corrupted storage → keep defaults
      }
      return copy(def)
    },
    serialize: (v) => JSON.stringify({ open: v.open, width: v.width }),
  }
}

const DEFAULT_SESSION_SIDEBAR: SessionSidebarState = {
  open: true,
  width: DEFAULT_SESSION_SIDEBAR_WIDTH,
}

// ── Registry ────────────────────────────────────────────────────────────────
// The single source of truth. Adding a panel state means adding one entry here
// and reading/writing it by id — no new key literal, no new scope decision
// buried in a component.

const entries: PanelEntry<unknown>[] = [
  {
    id: 'leftCollapsed', key: 'left-collapsed', scope: 'global', default: DEFAULT_LEFT_COLLAPSED,
    ...boolCodec(DEFAULT_LEFT_COLLAPSED),
  },
  {
    id: 'chatCollapsed', key: 'chat-collapsed', scope: 'global', default: false,
    ...boolCodec(false),
  },
  {
    id: 'splitRatio', key: 'split-ratio', scope: 'global', default: DEFAULT_RATIO,
    ...numberCodec(DEFAULT_RATIO),
  },
  {
    id: 'sessionSidebar', key: 'session-sidebar', scope: 'global', default: DEFAULT_SESSION_SIDEBAR,
    ...sessionSidebarCodec(DEFAULT_SESSION_SIDEBAR),
  },
  {
    id: 'tocDockOpen', key: 'toc-dock-open', scope: 'global', default: false,
    ...boolCodec(false),
  },
  {
    id: 'tocDockWidth', key: 'toc-dock-width', scope: 'global', default: DEFAULT_TOC_DOCK_WIDTH,
    ...numberCodec(DEFAULT_TOC_DOCK_WIDTH),
  },
  {
    id: 'tocDockSide', key: 'toc-dock-side', scope: 'global', default: DEFAULT_TOC_DOCK_SIDE,
    ...stringCodec(DEFAULT_TOC_DOCK_SIDE),
  },
  {
    id: 'fmPreviewRatio', key: 'fm-preview-ratio', scope: 'global', default: DEFAULT_FM_PREVIEW_RATIO,
    ...numberCodec(DEFAULT_FM_PREVIEW_RATIO),
  },
  // The left-column tab is the ONE per-project entry: which panel you were on
  // is a property of the project, not of the device (issue #474). Its default
  // is null — "this project has no record yet" — which is distinct from
  // 'browse' ("recorded as the file manager"). resolvePanelTab relies on that
  // distinction to pick the layout default on a first visit.
  {
    id: 'leftTab', key: 'left-tab', scope: 'project', default: null,
    ...stringCodec(null),
  },
]

export const PANEL_ENTRIES: Readonly<Record<string, PanelEntry<unknown>>> = Object.freeze(
  Object.fromEntries(entries.map((e) => [e.id, e])),
)

/**
 * The physical localStorage key for `id` under `projectRoot`.
 *
 * - global: `clawbench-panel.<key>`
 * - project: `clawbench-panel.<key>:<root>` — returns null when the root is
 *   empty/unknown, so callers fall back to the default instead of writing to a
 *   key like `...left-tab:` that no reader would ever look up.
 *
 * The root is appended verbatim. localStorage keys are opaque strings, so `/`
 * (POSIX paths) and `:` (Windows drive letters) need no escaping — and no code
 * ever parses a key back into (id, root); reads always know both already.
 */
export function physicalKey(id: PanelId, projectRoot?: string): string | null {
  const entry = PANEL_ENTRIES[id]
  if (!entry) return null
  if (entry.scope === 'global') return PANEL_KEY_PREFIX + entry.key
  if (!projectRoot) return null
  return PANEL_KEY_PREFIX + entry.key + ':' + projectRoot
}

/**
 * Read the persisted value for `id`. Falls back to the entry's default when
 * nothing is stored, the key is unreachable (empty project root), or storage
 * throws (private mode / disabled).
 */
export function readPanelState<T>(id: PanelId, projectRoot?: string): T {
  const entry = PANEL_ENTRIES[id] as PanelEntry<T> | undefined
  if (!entry) return undefined as T
  const key = physicalKey(id, projectRoot)
  if (key === null) return entry.default
  try {
    return entry.parse(localStorage.getItem(key))
  } catch {
    return entry.default
  }
}

/**
 * Persist `value` for `id`. Returns whether the write landed — false when there
 * is no physical key (a per-project entry with an unknown root) or storage
 * threw. The boolean exists so the migration can tell "the new key now holds
 * the value" from "the new key happens to be present", which is what lets it
 * delete the legacy key safely.
 */
export function writePanelState<T>(id: PanelId, value: T, projectRoot?: string): boolean {
  const entry = PANEL_ENTRIES[id] as PanelEntry<T> | undefined
  if (!entry) return false
  const key = physicalKey(id, projectRoot)
  if (key === null) return false
  try {
    localStorage.setItem(key, entry.serialize(value))
    return true
  } catch {
    // ignore — panel state is a convenience, never worth throwing over
    return false
  }
}

// ── One-time migration from the legacy key names ────────────────────────────
// Each state used to live under its own hand-rolled key (five naming styles).
// We copy each legacy value to its new key once, then drop the old key. Guarded
// by "new key absent AND old key present", so it is idempotent and safe to run
// on every load — no sentinel flag needed.

const LEGACY_GLOBAL_KEYS: Readonly<Record<string, string>> = {
  chatCollapsed: 'clawbench-widescreen-chat-collapsed',
  splitRatio: 'clawbench-widescreen-split-ratio',
  sessionSidebar: 'clawbench-session-sidebar',
  tocDockOpen: 'clawbench.tocDock.open',
  tocDockWidth: 'clawbench.tocDock.width',
  tocDockSide: 'clawbench.tocDock.side',
  fmPreviewRatio: 'clawbench-fm-preview-split-ratio',
}

/** Legacy per-project prefix that held the left-column tab. */
const LEGACY_PROJECT_PREFIX = 'clawbench-project-panel:'
/**
 * Legacy per-project prefix that held the left-column collapse. That state is
 * global now, so these records are not a valid per-key source. If they all
 * agree they are adopted as the initial global value (see migrateLegacyKeys);
 * otherwise they are deleted as dead weight and the global default applies.
 */
const LEGACY_COLLAPSED_PREFIX = 'clawbench-project-panel-collapsed:'

/**
 * Copy every legacy-key value to its new key, then drop the old key. Exported
 * so tests can drive it against a seeded localStorage — the module-load call
 * below runs before any test's `beforeEach` can seed, so an unexported function
 * would only be reachable by reimplementing it (which would test nothing).
 * Idempotent, so calling it again in a test is safe.
 */
export function migrateLegacyKeys(): void {
  // Snapshot keys first: we remove entries as we go, and mutating while
  // iterating Object.keys(localStorage) is not reliable across engines.
  const allKeys = Object.keys(localStorage)

  for (const [id, legacyKey] of Object.entries(LEGACY_GLOBAL_KEYS)) {
    const panelId = id as PanelId
    const newKey = physicalKey(panelId)
    if (newKey === null) continue
    const legacyValue = localStorage.getItem(legacyKey)
    if (legacyValue === null) continue
    // Write only when the new key is absent; then drop the legacy key only if
    // the write actually landed. Checking `getItem(newKey) !== null` instead
    // would treat a pre-existing empty string as "migrated" and delete the
    // legacy value that was never copied.
    const wrote = localStorage.getItem(newKey) === null
      ? writePanelState(panelId, PANEL_ENTRIES[panelId].parse(legacyValue))
      : true // new key already present — nothing to copy, safe to drop legacy
    if (wrote) localStorage.removeItem(legacyKey)
  }

  // Adopt the legacy per-project collapse only when every record agrees, so a
  // user who had one project (the common case) keeps the column state they
  // last saw. Conflicting records have no correct global answer, so the default
  // applies instead of inventing one.
  const collapsedValues = new Set<string>()
  for (const key of allKeys) {
    if (!key.startsWith(LEGACY_COLLAPSED_PREFIX)) continue
    const v = localStorage.getItem(key)
    if (v !== null) collapsedValues.add(v)
  }
  if (collapsedValues.size === 1 && localStorage.getItem(physicalKey('leftCollapsed')!) === null) {
    writePanelState('leftCollapsed', [...collapsedValues][0] === '1')
  }

  for (const key of allKeys) {
    if (key.startsWith(LEGACY_PROJECT_PREFIX)) {
      const root = key.slice(LEGACY_PROJECT_PREFIX.length)
      const legacyValue = localStorage.getItem(key)
      if (legacyValue === null) continue
      const newKey = physicalKey('leftTab', root)
      // An empty root (a bare `clawbench-project-panel:`) has no physical key —
      // drop the dead key rather than leaving it to be re-scanned every load.
      if (newKey === null) {
        localStorage.removeItem(key)
        continue
      }
      const wrote = localStorage.getItem(newKey) === null
        ? writePanelState('leftTab', PANEL_ENTRIES.leftTab.parse(legacyValue), root)
        : true
      if (wrote) localStorage.removeItem(key)
    } else if (key.startsWith(LEGACY_COLLAPSED_PREFIX)) {
      localStorage.removeItem(key)
    }
  }
}

// Run once on module load. Guarded because localStorage can be absent (SSR) or
// throw (private mode) — migration is best-effort.
try {
  if (typeof localStorage !== 'undefined') migrateLegacyKeys()
} catch {
  // ignore
}
