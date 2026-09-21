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
}

/** IPC channels the main process uses to deliver a notification click. */
export const NAV_CHANNELS = [
  'clawbench-open-session',
  'clawbench-open-task',
  'clawbench-open-forge',
] as const

export type NavChannel = (typeof NAV_CHANNELS)[number]
