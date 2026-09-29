import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: a WS reconnect must re-derive the forge unread badge.
 *
 * Reported bug: the GitLab/GitHub activity tab showed unread rows while the
 * header's "mark all read" button stayed disabled. The button is disabled from
 * `forgeUnreadCount === 0` (ForgePanelContent.vue), and that count had no
 * reconnect trigger:
 *
 *   - `handleReconnect` refreshed project/files/git/sessions/tasks/terminal
 *     but NOT the forge badge;
 *   - the badge's other refresh paths are app mount, project SWITCH (a
 *     `projectRoot` watcher), a LIVE `forge_event`, and a mark-read write;
 *   - forge events are neither persisted to `pending_events`
 *     (IsNotifiableEvent only whitelists session/task/user_message) nor
 *     re-applied from the WS replay buffer (useGlobalEvents skips
 *     `replayed` forge events) — so an event missed while disconnected is
 *     lost for the badge.
 *
 * Android disconnects the WS while backgrounded, so returning to the
 * foreground is exactly when this bites. The refresh must be chained AFTER
 * `loadProject()`: the count is project-scoped (`requireProject` answers 403
 * without the cookie), and unlike sessions/files — which self-heal on the next
 * user action — a badge that misses this refresh stays wrong until a project
 * switch.
 *
 * App.vue has no mount test (it is the whole application), so this is asserted
 * at the source level.
 */
describe('WS reconnect refreshes the forge unread badge', () => {
  const APP = 'src/App.vue'

  /** The `handleReconnect` arrow-function body, or throw if it moved. */
  function handleReconnectBody(src: string): string {
    const start = src.indexOf('const handleReconnect = () => {')
    if (start === -1) throw new Error('handleReconnect definition not found')
    const open = src.indexOf('{', start)
    let depth = 0
    for (let i = open; i < src.length; i++) {
      if (src[i] === '{') depth++
      else if (src[i] === '}') {
        depth--
        if (depth === 0) return src.slice(open, i + 1)
      }
    }
    throw new Error('handleReconnect body is not brace-balanced')
  }

  it('finds the reconnect handler (guard is actually armed)', () => {
    expect(handleReconnectBody(readWebFile(APP))).toContain('loadProject')
  })

  it('re-derives the forge badge on reconnect', () => {
    // Regression guard for the reported bug: this call did not exist, so a
    // reconnect left the badge at whatever value it had before.
    expect(handleReconnectBody(readWebFile(APP))).toContain('refreshForgeUnread')
  })

  it('refreshes the badge only AFTER the project cookie is re-established', () => {
    // `refreshForgeUnread` is project-scoped: fired before loadProject() lands
    // it answers 403 and the badge keeps its stale value.
    const body = handleReconnectBody(readWebFile(APP))
    const loadProject = body.indexOf('store.loadProject()')
    const refresh = body.indexOf('refreshForgeUnread')

    expect(loadProject, 'loadProject must be called').toBeGreaterThan(-1)
    expect(refresh, 'the forge refresh must come after it').toBeGreaterThan(loadProject)

    // …and be chained on the same statement, so it cannot run before the
    // cookie write completes. A bare sibling call would race it.
    const loadProjectLine = body
      .slice(loadProject)
      .split('\n')[0]
    expect(
      loadProjectLine,
      'the forge refresh must be chained on loadProject(), not fired in parallel',
    ).toContain('refreshForgeUnread')
  })
})
