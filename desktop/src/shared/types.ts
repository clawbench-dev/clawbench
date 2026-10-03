/** Navigation target for a native notification click. */
export interface NotificationNav {
  sessionId?: string
  taskId?: string
  executionId?: string
  projectPath?: string
  /**
   * Set for forge (GitHub/GitLab) change notifications. They carry no
   * session/task id — only `projectPath` — so without this explicit
   * discriminator the click handler's sessionId/taskId branches both miss and
   * clicking the notification would silently do nothing.
   */
  forge?: boolean
  /**
   * The forge item the click should open, so the destination is that item's
   * detail rather than just the forge tab.
   *
   * Mirrors `ForgeTarget` in web/src/composables/useForgeNavigation.ts — the
   * two builds cannot share a type, so this copy must stay in step with it.
   * `itemKey` is the opaque read key: a pipeline's `number` is always 0, so its
   * identity is the run id ("pipeline/run:<id>").
   */
  forgeTarget?: ForgeTarget
}

/** The forge item a notification click deep-links to. */
export interface ForgeTarget {
  /** The project whose bound repository holds this item. Empty means "the active project". */
  projectPath?: string
  /** 'issue' | 'pr' | 'pipeline' */
  type: 'issue' | 'pr' | 'pipeline'
  /** Issue/PR number. Always 0 for a pipeline. */
  number: number
  /** CI run id; 0 for anything that is not a pipeline. */
  runId: number
  /** Opaque read key (`issue/12`, `pr/7`, `pipeline/run:555`). */
  itemKey: string
}

/** IPC channels the main process uses to deliver a notification click. */
export const NAV_CHANNELS = [
  'clawbench-open-session',
  'clawbench-open-task',
  'clawbench-open-forge',
] as const

export type NavChannel = (typeof NAV_CHANNELS)[number]

/**
 * Main → renderer channel reporting the window's maximize state.
 *
 * A frameless window has no native maximize/restore affordance, so the header's
 * cluster is the only place the user can see which of the two the window is in
 * — the button has to swap between the maximize and restore glyphs. The state
 * is owned by the window (it also changes on double-click of the drag region
 * and on OS-level snaps), so it is pushed from the main process rather than
 * inferred in the renderer.
 */
export const WINDOW_STATE_CHANNEL = 'clawbench-window-state'

/** Payload of `WINDOW_STATE_CHANNEL`. */
export interface WindowState {
  maximized: boolean
}

/**
 * Main → renderer channel reporting that a forward's local listener drifted to
 * a different port than the one the server registry knows about.
 *
 * `listenForward` binds the next free port when the requested one is taken, and
 * the SERVER registry is keyed by the port the renderer registered. The add
 * path already relays the actually-bound port back through the
 * `addForwardedPort` return value, but the rebuild path (`rebuildAllForwards`
 * on reconnect) has no such return to the renderer — so a second drift during a
 * reconnect left the server keyed to a port nothing was listening on, and the
 * mapping could not even be deleted from the UI. This event carries the same
 * information so the renderer can re-key the registry.
 *
 * Payload: `{ requested, actual }` — the port the registry used vs. the port
 * actually bound.
 */
export const PORT_REBOUND_CHANNEL = 'clawbench-port-rebound'

export interface PortReboundEvent {
  requested: number
  actual: number
}
