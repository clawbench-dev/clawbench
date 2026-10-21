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

  test('collapsed chip leads with the team glyph', async ({ page }) => {
    await injectTeam(page, sampleTeam)
    await expect(page.locator('.team-chip__icon')).toBeVisible({ timeout: 5000 })
    await expect(page.locator('.team-chip__icon')).toHaveClass(/team-chip__icon--running/)
    await expect(page.locator('.team-chip__dot')).toHaveCount(0)
  })

  test('clicking the chip expands to one row per member', async ({ page }) => {
    await injectTeam(page, sampleTeam)
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()

    await expect(page.locator('.team-expanded')).toBeVisible()
    const rows = page.locator('.team-member')
    await expect(rows).toHaveCount(2)
    await expect(rows.nth(0).locator('.team-member__name')).toHaveText('probe-alpha')
    await expect(rows.nth(0).locator('.team-member__status-icon')).toHaveAttribute('data-status', 'running')
    await expect(rows.nth(1).locator('.team-member__status-icon')).toHaveAttribute('data-status', 'completed')
  })

  test('running member pulses, terminated member is dimmed', async ({ page }) => {
    await injectTeam(page, sampleTeam)
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()

    const icons = page.locator('.team-member__status-icon')
    await expect(icons.nth(0)).toHaveClass(/team-member__status-icon--running/)
    await expect(icons.nth(1)).toHaveClass(/team-member__status-icon--completed/)
    await expect(page.locator('.team-member').nth(1)).toHaveClass(/team-member--terminated/)
    // Status is a glyph now, not a dot and not a word.
    await expect(page.locator('.team-member__dot')).toHaveCount(0)
    await expect(page.locator('.team-member__status')).toHaveCount(0)
  })

  test('running member uses the shared loading ring', async ({ page }) => {
    // One spinner for the whole app: the ring is declared globally and must be
    // what the roster draws, at the row's 12px size.
    await injectTeam(page, sampleTeam)
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()

    const ring = page.locator('.team-member__status-icon--running')
    await expect(ring).toHaveClass(/li-spinner/)
    await expect(ring.locator('svg')).toHaveCount(0)
    // Layout size, NOT getBoundingClientRect/boundingBox: the ring is rotating,
    // so its axis-aligned bounding box swings between 12px and 12*√2 ≈ 17px
    // depending on the animation phase — measuring it is inherently flaky.
    const size = await ring.evaluate(el => [el.offsetWidth, el.offsetHeight])
    expect(size).toEqual([12, 12])
    // It is actually animating (a static ring would read as "stuck").
    const name = await ring.evaluate(el => getComputedStyle(el).animationName)
    expect(name).toBe('li-spin')
  })

  test('tool count renders as a wrench glyph plus the number', async ({ page }) => {
    await injectTeam(page, sampleTeam)
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()

    const cell = page.locator('.team-member__tools').first()
    await expect(cell.locator('svg')).toBeVisible()
    await expect(cell).toHaveText('2')
  })

  test('the tool count and token columns line up across rows', async ({ page }) => {
    // Geometry, not markup: the ragged look came from rows whose trailing cells
    // were content-sized, so a member without tool calls pulled its neighbours
    // left. Compare the actual painted x of both columns.
    await injectTeam(page, {
      type: 'member_status_change',
      teamName: 'clawbench-probe',
      hasLiveMembers: true,
      members: [
        { name: 'a-many-tools', status: 'running', toolCallCount: 42, tokenUsage: { inputTokens: 123456, outputTokens: 7890 } },
        { name: 'b-no-tools', status: 'pending' },
        { name: 'c-one-tool', status: 'completed', toolCallCount: 1, tokenUsage: { inputTokens: 12, outputTokens: 3 } },
      ],
    })
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()
    await expect(page.locator('.team-member')).toHaveCount(3)

    const boxes = await page.locator('.team-member__tools').evaluateAll(
      els => els.map(el => Math.round(el.getBoundingClientRect().left)),
    )
    expect(boxes).toHaveLength(3)
    expect(new Set(boxes).size).toBe(1)

    const tokenBoxes = await page.locator('.team-member__tokens').evaluateAll(
      els => els.map(el => Math.round(el.getBoundingClientRect().right)),
    )
    expect(new Set(tokenBoxes).size).toBe(1)
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

  test('shows an ended chip once no member is live', async ({ page }) => {
    await injectTeam(page, {
      type: 'team_idle',
      teamName: 'clawbench-probe',
      hasLiveMembers: false,
      members: [{ name: 'probe-alpha', status: 'completed', activity: 'idle', lifecycle: 'terminated' }],
    })
    await expect(page.locator('.team-chip__text')).toBeVisible({ timeout: 5000 })
    await expect(page.locator('.team-chip__text')).toHaveText('clawbench-probe · ended')
  })

  test('labels an auto team in the expanded header', async ({ page }) => {
    await injectTeam(page, { ...sampleTeam, isAutoTeam: true })
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()
    await expect(page.locator('.team-expanded__auto')).toBeVisible()
    await expect(page.locator('.team-expanded__auto')).toHaveText('auto team')
  })

  test('keeps the member agent type out of the row and in the tooltip', async ({ page }) => {
    await injectTeam(page, {
      type: 'member_status_change',
      teamName: 'clawbench-probe',
      hasLiveMembers: true,
      members: [
        {
          name: 'probe-alpha',
          status: 'running',
          description: 'runs the probe',
          agentType: 'general-purpose',
          tokenUsage: { inputTokens: 100, outputTokens: 20, lastContextWindow: 200000 },
        },
      ],
    })
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()
    await expect(page.locator('.team-member')).toBeVisible()
    await expect(page.locator('.team-member__agent-type')).toHaveCount(0)
    const title = await page.locator('.team-member').first().getAttribute('title')
    expect(title).toContain('runs the probe')
    expect(title).toContain('type: general-purpose')
    expect(title).toContain('tokens: 100 in / 20 out')
    expect(title).toContain('context: 200000')
  })
})
