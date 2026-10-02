import { test, expect } from '../fixtures'

/**
 * E2E tests for the Agent Team roster panel (CodeBuddy Agent Teams).
 *
 * Strategy: inject team snapshots through the window.__clawbench E2E bridge
 * rather than driving a real team over ACP. This validates the parts the
 * bridge controls deterministically:
 *   - Vue reactivity (team state → TeamPanel rendering)
 *   - collapsed/expanded interaction
 *   - status → dot class mapping, member colouring, token formatting
 *   - team_deleted clearing the panel
 *
 * The wire → StreamEvent → WS path is covered by Go unit tests
 * (internal/ai/codebuddy_team_bridge_test.go, internal/ws/stream_hub_test.go)
 * and by the integration probe
 * (internal/ai/codebuddy_acp_team_probe_integration_test.go).
 */
test.describe('Agent Team Panel', () => {
  test.beforeEach(async ({ page }) => {
    await page.waitForFunction(() => !!(window as any).__clawbench?.updateTeamState, undefined, { timeout: 10000 })
    // Team state is module-level and survives navigation; clear it so a prior
    // test's roster does not leak into the "no team" case.
    await page.evaluate(() => {
      const bridge = (window as any).__clawbench
      if (bridge?.clearTeamState) bridge.clearTeamState()
    })
  })

  async function injectTeam(page: any, state: Record<string, unknown>) {
    await page.evaluate((state) => {
      const bridge = (window as any).__clawbench
      if (bridge?.updateTeamState) bridge.updateTeamState(state)
    }, state)
  }

  const sampleTeam = {
    type: 'member_status_change',
    teamName: 'clawbench-probe',
    isAutoTeam: false,
    hasLiveMembers: true,
    members: [
      { name: 'probe-alpha', color: 'blue', status: 'running', activity: 'working', lifecycle: 'alive', toolCallCount: 2, tokenUsage: { inputTokens: 56763, outputTokens: 229 } },
      { name: 'probe-beta', color: 'green', status: 'completed', activity: 'idle', lifecycle: 'terminated', toolCallCount: 1 },
    ],
  }

  test('panel is hidden when no team exists', async ({ page }) => {
    await expect(page.locator('.team-panel')).not.toBeVisible()
  })

  test('panel appears collapsed after injecting a team', async ({ page }) => {
    await injectTeam(page, sampleTeam)
    await expect(page.locator('.team-panel')).toBeVisible({ timeout: 5000 })
    await expect(page.locator('.team-chip')).toBeVisible()
    await expect(page.locator('.team-expanded')).not.toBeVisible()
  })

  test('collapsed chip shows active/total count', async ({ page }) => {
    await injectTeam(page, sampleTeam)
    await expect(page.locator('.team-chip__count')).toBeVisible({ timeout: 5000 })
    await expect(page.locator('.team-chip__count')).toHaveText('1/2')
  })

  test('clicking the chip expands to one row per member', async ({ page }) => {
    await injectTeam(page, sampleTeam)
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()

    await expect(page.locator('.team-expanded')).toBeVisible()
    const rows = page.locator('.team-member')
    await expect(rows).toHaveCount(2)
    await expect(rows.nth(0).locator('.team-member__name')).toHaveText('probe-alpha')
    await expect(rows.nth(0).locator('.team-member__status')).toHaveText('running')
    await expect(rows.nth(1).locator('.team-member__status')).toHaveText('completed')
  })

  test('running member pulses, terminated member is dimmed', async ({ page }) => {
    await injectTeam(page, sampleTeam)
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()

    const dots = page.locator('.team-member__dot')
    await expect(dots.nth(0)).toHaveClass(/team-member__dot--running/)
    await expect(dots.nth(1)).toHaveClass(/team-member__dot--completed/)
    await expect(page.locator('.team-member').nth(1)).toHaveClass(/team-member--terminated/)
  })

  test('panel disappears when the team is deleted', async ({ page }) => {
    await injectTeam(page, sampleTeam)
    await expect(page.locator('.team-panel')).toBeVisible({ timeout: 5000 })

    await injectTeam(page, { type: 'team_deleted', teamName: 'clawbench-probe', members: [] })
    await expect(page.locator('.team-panel')).not.toBeVisible()
  })

  test('roster replaces wholesale on a new snapshot', async ({ page }) => {
    await injectTeam(page, sampleTeam)
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()
    await expect(page.locator('.team-member')).toHaveCount(2)

    await injectTeam(page, {
      type: 'member_status_change',
      teamName: 'clawbench-probe',
      members: [{ name: 'probe-alpha', status: 'completed', lifecycle: 'terminated' }],
    })
    await expect(page.locator('.team-member')).toHaveCount(1)
  })
})
