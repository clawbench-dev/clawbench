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
 * SCOPE IS RUNTIME, NOT COMPILE-TIME. The user picks "remember layout globally"
 * or "remember it per project" in appearance settings; `setLayoutScopeMode`
 * switches every entry that follows that switch (all of them except the
 * left-column tab, which is pinned to the project — issue #474). Because the
 * decision now lives in a mutable module variable, `layoutEpoch` exists: every
 * scope/root change bumps it, and the layout composables watch it to re-read
 * their stored values. Without that they would keep the values they read on
 * first use and the switch would appear to do nothing.
 *
 * LEAFLET: this file must not import any composable (useWideScreenLayout,
 * useProjectPanel, useTocDockPreference, useSessionSidebar). Those import THIS
 * module, so the reverse edge would create a cycle. Only `utils/splitRatio`
 * and `vue` (for the epoch ref) are imported; both are leaves.
 */
import { ref } from 'vue'
import { DEFAULT_RATIO } from '@/utils/splitRatio'

/** Where a state lives: one shared key, or one key per project root. */
export type Scope = 'global' | 'project'

/** How the user chose to remember the layout (设置 → 外观 → 布局记忆). */
export type ScopeMode = 'global' | 'project'

export interface PanelEntry<T> {
  /** Registry id, used as the argument to readPanelState/writePanelState. */
  id: PanelId
  /** Logical key fragment; the physical key is derived from it (see physicalKey). */
  key: string
  /**
   * The entry's own scope. Only consulted for entries that do NOT follow the
   * layout-memory switch (`followsLayoutMemory: false`); for the rest the
   * runtime mode decides and this field just documents the default.
   */
  scope: Scope
  /**
   * Whether this entry's scope is decided by the layout-memory setting.
   * Defaults to true. `leftTab` opts out: which panel a project was last on is
   * a property of the project itself (issue #474), so "remember layout
   * globally" must not make every project share one tab.
   */
  followsLayoutMemory?: boolean
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
  | 'fmPreviewOpen'
  | 'gitBranchCollapsed'
  | 'gitWorktreeCollapsed'
  | 'gitManageTab'
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
export const DEFAULT_GIT_MANAGE_TAB = 'branches'

export interface SessionSidebarState {
  open: boolean
  width: number
}

// ── Runtime scope ───────────────────────────────────────────────────────────
// The user's "布局记忆" choice, plus the project root currently in scope. Both
// are plain module state (not persisted here) — the setting itself is a local
// setting (`clawbench-settings-layoutMemory`) and the root is pushed by App.vue
// from the store. panelState stays a leaf: it never imports the store.

let scopeMode: ScopeMode = 'global'
let scopeRoot = ''

/**
 * Bumped whenever the effective storage location of any entry can have
 * changed — a scope-mode switch, or the project root moving. Layout
 * composables watch it and re-read; without it they would keep the values
 * loaded on first use (each of them guards its load behind `initialized`).
 *
 * A no-op call (same mode, same root) does NOT bump: the project-root watcher
 * fires more often than the root actually changes, and every bump re-reads
 * every layout composable.
 */
export const layoutEpoch = ref(0)

/** The scope mode currently in force. */
export function getLayoutScopeMode(): ScopeMode {
  return scopeMode
}

/** The project root currently used to resolve per-project keys ('' = unknown). */
export function getLayoutProjectRoot(): string {
  return scopeRoot
}

/**
 * The scope an entry is actually stored under right now.
 *
 * Entries with `followsLayoutMemory: false` keep their declared scope; the rest
 * follow the user's switch. A project-scoped entry with no known root has no
 * physical key at all (see physicalKey), so callers fall back to defaults —
 * that is the pre-project-load state, not an error.
 */
export function effectiveScope(entry: PanelEntry<unknown>): Scope {
  if (entry.followsLayoutMemory === false) return entry.scope
  return scopeMode
}

/**
 * Switch how the layout is remembered, seeding each project with the value the
 * global key holds today.
 *
 * The seed is what the user asked for: flipping to "per project" must not make
 * every project snap back to factory defaults — the layout they are looking at
 * right now is the one each project should start from. Seeding only fills
 * entries that have no project value yet, so projects that were already
 * customised (while the mode was 'project' before) keep their own values.
 */
export function setLayoutScopeMode(mode: ScopeMode): void {
  if (mode === scopeMode) return
  scopeMode = mode
  if (mode === 'project') seedProjectFromGlobal()
  layoutEpoch.value++
}

/**
 * Point the registry at a project root. Bumps the epoch so composables reload;
 * a repeat call with the same root is a no-op (project switches already call
 * this on every navigation).
 */
export function setLayoutProjectRoot(root: string): void {
  const next = root || ''
  if (next === scopeRoot) return
  scopeRoot = next
  if (scopeMode === 'project') seedProjectFromGlobal()
  layoutEpoch.value++
}

/**
 * Copy every "global value, no project value" pair into the project key.
 *
 * Read-only with respect to the global key: the global record stays as the
 * seed for projects visited later, and switching back to 'global' restores it.
 * Silent on storage failure (a quota error must not break layout).
 */
export function seedProjectFromGlobal(): void {
  if (!scopeRoot) return
  for (const entry of Object.values(PANEL_ENTRIES)) {
    if (entry.followsLayoutMemory === false) continue
    if (effectiveScope(entry) !== 'project') continue
    const globalKey = PANEL_KEY_PREFIX + entry.key
    const projectKey = PANEL_KEY_PREFIX + entry.key + ':' + scopeRoot
    try {
      if (localStorage.getItem(projectKey) !== null) continue
      const raw = localStorage.getItem(globalKey)
      if (raw === null) continue
      localStorage.setItem(projectKey, raw)
    } catch {
      // ignore — layout memory is a convenience
    }
  }
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

/**
 * Tri-state boolean: '1' / '0' / absent. Needed where "the user never chose"
 * must stay distinguishable from "the user chose false", because the component
 * has its own default that a stored false would wrongly override.
 */
function nullableBoolCodec(def: boolean | null): Pick<PanelEntry<boolean | null>, 'parse' | 'serialize'> {
  return {
    // '' is "absent", matching stringCodec: serialize(null) writes '', so
    // reading that back must yield the default rather than a recorded `false`
    // (which would look like "the user explicitly chose not-collapsed").
    parse: (raw) => (raw === null || raw === '' ? def : raw === '1'),
    serialize: (v) => (v === null ? '' : v ? '1' : '0'),
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
  // Whether the docked preview pane is expanded. Distinct from
  // `fmPreviewRatio` (how the split is divided) and from the `filePreviewMode`
  // setting (whether single-click previews at all): the pane's own close button
  // collapses it without turning the mode off, and that collapse used to be
  // in-memory only, so every reload re-opened it.
  {
    id: 'fmPreviewOpen', key: 'fm-preview-open', scope: 'global', default: false,
    ...boolCodec(false),
  },
  // Git panel: two collapsible section headers plus the active sub-tab. These
  // used to be three hand-rolled bare keys ('git-branch-collapsed',
  // 'git-worktree-collapsed', 'git-manage-active-tab') outside the registry,
  // which is why they were the one part of the layout the layout-memory switch
  // could not reach. Default null means "no record" so the component's own
  // `initialCollapsed` prop still decides on a first visit.
  {
    id: 'gitBranchCollapsed', key: 'git-branch-collapsed', scope: 'global', default: null,
    ...nullableBoolCodec(null),
  },
  {
    id: 'gitWorktreeCollapsed', key: 'git-worktree-collapsed', scope: 'global', default: null,
    ...nullableBoolCodec(null),
  },
  {
    id: 'gitManageTab', key: 'git-manage-tab', scope: 'global', default: DEFAULT_GIT_MANAGE_TAB,
    ...stringCodec(DEFAULT_GIT_MANAGE_TAB),
  },
  // The left-column tab is the ONE entry pinned to the project: which panel you
  // were on is a property of the project, not of the device (issue #474). Its
  // default is null — "this project has no record yet" — which is distinct from
  // 'browse' ("recorded as the file manager"). resolvePanelTab relies on that
  // distinction to pick the layout default on a first visit.
  //
  // This entry ALSO covers the narrow layout's active tab: App.vue persists
  // `currentPanelTab` (which is `leftTab` on a wide screen and `activeTab` on a
  // narrow one) here, and restores it through resolvePanelTab on cold start and
  // on every project switch. A separate narrow-tab entry would be a second
  // writer for the same fact and would fight resolvePanelTab's rules.
  {
    id: 'leftTab', key: 'left-tab', scope: 'project', followsLayoutMemory: false, default: null,
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
 * The effective scope comes from `effectiveScope`, so callers never pass a
 * scope decision in: the registry owns it. `projectRoot` is only the root to
 * use when the entry IS project-scoped; callers normally omit it and let the
 * registry use the root App.vue pushed.
 *
 * The root is appended verbatim. localStorage keys are opaque strings, so `/`
 * (POSIX paths) and `:` (Windows drive letters) need no escaping — and no code
 * ever parses a key back into (id, root); reads always know both already.
 */
export function physicalKey(id: PanelId, projectRoot?: string): string | null {
  const entry = PANEL_ENTRIES[id]
  if (!entry) return null
  if (effectiveScope(entry) === 'global') return PANEL_KEY_PREFIX + entry.key
  const root = projectRoot ?? scopeRoot
  if (!root) return null
  return PANEL_KEY_PREFIX + entry.key + ':' + root
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

interface LegacyKeySource {
  /** The old physical key. */
  key: string
  /**
   * Optional raw-value normalizer, for legacy keys whose encoding differs from
   * the registry's. The git collapse keys stored 'true'/'false' (they were
   * written with `String(boolean)`), while the registry's tri-state boolean
   * reads only '1'/'0' — without this the migration would silently store
   * "collapsed: false" for a user whose panel was collapsed.
   */
  normalize?: (raw: string) => string
}

const LEGACY_GLOBAL_KEYS: Readonly<Record<string, LegacyKeySource>> = {
  chatCollapsed: { key: 'clawbench-widescreen-chat-collapsed' },
  splitRatio: { key: 'clawbench-widescreen-split-ratio' },
  sessionSidebar: { key: 'clawbench-session-sidebar' },
  tocDockOpen: { key: 'clawbench.tocDock.open' },
  tocDockWidth: { key: 'clawbench.tocDock.width' },
  tocDockSide: { key: 'clawbench.tocDock.side' },
  fmPreviewRatio: { key: 'clawbench-fm-preview-split-ratio' },
  // These three were never prefixed and were written by the git components
  // themselves. Same one-time copy, same "delete only after the write landed".
  gitBranchCollapsed: { key: 'git-branch-collapsed', normalize: boolStringToFlag },
  gitWorktreeCollapsed: { key: 'git-worktree-collapsed', normalize: boolStringToFlag },
  gitManageTab: { key: 'git-manage-active-tab' },
}

/** 'true'/'false' (the git components' old encoding) → the registry's '1'/'0'. */
function boolStringToFlag(raw: string): string {
  if (raw === 'true') return '1'
  if (raw === 'false') return '0'
  return raw
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

  for (const [id, legacy] of Object.entries(LEGACY_GLOBAL_KEYS)) {
    const panelId = id as PanelId
    // Migration runs at module load, before any project root is known and while
    // the scope mode is still the default 'global' — so `physicalKey` returns
    // the global key for every entry here, including the ones that a user may
    // later switch to per-project. That is correct: the legacy value becomes
    // the global seed, which is exactly what the per-project mode copies from.
    const newKey = physicalKey(panelId)
    if (newKey === null) continue
    const legacyValue = localStorage.getItem(legacy.key)
    if (legacyValue === null) continue
    const raw = legacy.normalize ? legacy.normalize(legacyValue) : legacyValue
    // Write only when the new key is absent; then drop the legacy key only if
    // the write actually landed. Checking `getItem(newKey) !== null` instead
    // would treat a pre-existing empty string as "migrated" and delete the
    // legacy value that was never copied.
    const wrote = localStorage.getItem(newKey) === null
      ? writePanelState(panelId, PANEL_ENTRIES[panelId].parse(raw))
      : true // new key already present — nothing to copy, safe to drop legacy
    if (wrote) localStorage.removeItem(legacy.key)
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
  // This block only ever writes the GLOBAL key (it runs at module load, while
  // the scope mode is the default). Use the key prefix directly rather than
  // physicalKey, which would return null if the mode were ever project-scoped.
  const globalCollapsedKey = PANEL_KEY_PREFIX + 'left-collapsed'
  if (collapsedValues.size === 1 && localStorage.getItem(globalCollapsedKey) === null) {
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

/**
 * @internal Reset the runtime scope back to its defaults — for tests only.
 *
 * The module-level `scopeMode` / `scopeRoot` persist across test cases in a
 * file (they are module state, not refs a test can re-create), so a test that
 * switches to 'project' would otherwise leak that choice into every later
 * test. Does not touch storage.
 */
export function _resetLayoutScopeForTest(): void {
  scopeMode = 'global'
  scopeRoot = ''
  layoutEpoch.value = 0
}
