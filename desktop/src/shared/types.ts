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
