import { test, expect } from '../fixtures'
import { NavigationPage } from '../pages/navigation.page'

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

  test('shows the member agent type inline when the row has room', async ({ page }) => {
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
    // A wide viewport has room, so the type is visible inline.
    await expect(page.locator('.team-member__agent-type')).toBeVisible()
    await expect(page.locator('.team-member__agent-type')).toHaveText('general-purpose')
    // The tooltip keeps carrying it too, which is what remains on a narrow panel.
    const title = await page.locator('.team-member').first().getAttribute('title')
    expect(title).toContain('runs the probe')
    expect(title).toContain('type: general-purpose')
    expect(title).toContain('tokens: 100 in / 20 out')
    expect(title).toContain('context: 200000')
  })

  test('hides the agent type before the member name when the panel is narrow', async ({ page }) => {
    // The name is the only thing distinguishing two rows; the type is metadata
    // that is identical across an auto-team. So on a narrow panel the TYPE must
    // go and the NAME must stay.
    await page.setViewportSize({ width: 360, height: 780 })
    // On a narrow screen the app is a drawer layout: the chat panel is only
    // mounted while its tab is active, so the roster cannot be asserted before
    // switching to it. Go through the page object: a bare
    // `.filter({ visible: true })` latches onto the WIDE dock's chat toggle,
    // which `v-show` hides one frame later, and the click then times out on
    // "element is not visible" (measured on WebKit in CI).
    await new NavigationPage(page).switchToChat()
    await injectTeam(page, {
      type: 'member_status_change',
      teamName: 'clawbench-probe',
      hasLiveMembers: true,
      members: [
        { name: 'probe-alpha', status: 'running', agentType: 'general-purpose' },
        { name: 'probe-beta', status: 'completed', agentType: 'specialised-reviewer' },
      ],
    })
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()
    await expect(page.locator('.team-member').first()).toBeVisible()

    // Type hidden by the container query. It stays in the DOM (CSS, not v-if),
    // so the assertion is on VISIBILITY — that is also what the user sees.
    await expect(page.locator('.team-member__agent-type').first()).toBeHidden()
    const name = page.locator('.team-member__name').first()
    await expect(name).toBeVisible()
    await expect(name).toHaveText('probe-alpha')
    // The name keeps real width — it must not be the thing that collapsed.
    const box = await name.boundingBox()
    expect(box!.width).toBeGreaterThan(20)
  })

  test('member detail sheet is wide and renders the role as Markdown', async ({ page }) => {
    // The sheet carries the member's full instructions, which are long-form
    // Markdown. Two things must hold in a REAL browser (jsdom has no layout and
    // no Markdown pipeline): the card is wider than the 624px default, and the
    // text renders as Markdown rather than a literal dump.
    const role = [
      '你是 ClawBench 仓库的代码审查员。',
      '',
      '## 任务',
      '审查媒体缩放相关的改动：`web/src/utils/inlineZoom.ts:181`',
      '',
      '| 严重度 | 位置 | 问题 |',
      '|---|---|---|',
      '| P1 | `inlineZoom.ts:181` | refitHook 早于 is-zooming 移除 |',
      '',
      '```go',
      'func main() { fmt.Println("hi") }',
      '```',
    ].join('\n')

    await injectTeam(page, {
      type: 'member_status_change',
      teamName: 'clawbench-review',
      hasLiveMembers: true,
      members: [{
        name: 'review-media',
        agentType: 'general-purpose',
        color: 'blue',
        status: 'completed',
        description: role,
        toolCallCount: 17,
        tokenUsage: { inputTokens: 136675, outputTokens: 253 },
      }],
    })
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()
    await page.locator('[data-action="team-member-detail"]').first().click()
    await expect(page.locator('.team-member-detail__name')).toBeVisible()

    const card = await page.evaluate(() => {
      const panel = document.querySelector('.bs-panel') as HTMLElement
      const md = document.querySelector('.team-member-detail__markdown') as HTMLElement
      return {
        panelWidth: Math.round(panel.getBoundingClientRect().width),
        h2: md.querySelectorAll('h2').length,
        table: md.querySelectorAll('table').length,
        pre: md.querySelectorAll('pre').length,
        // A plain-text dump would leave the raw '## ' in the rendered text.
        literalHeading: (md.textContent || '').includes('## 任务'),
        pathChips: md.querySelectorAll('.chat-file-path').length,
      }
    })

    // Wider than modal-card.css's 624px default — that is what "wider" means.
    expect(card.panelWidth).toBeGreaterThan(624)
    expect(card.h2).toBe(1)
    expect(card.table).toBe(1)
    expect(card.pre).toBe(1)
    expect(card.literalHeading).toBe(false)
    expect(card.pathChips).toBeGreaterThan(0)
  })

  test('the role section title lines up with the fact labels above it', async ({ page }) => {
    // The title used to start at the sheet edge (2px) while the fact labels sit
    // 16px in, so it read as glued to the border. Alignment is a GLYPH position,
    // not a box position: a block element's padding does not move its box, so
    // this must be measured with a text Range — getBoundingClientRect on the
    // element reports the box and would say they differ when they do not.
    await injectTeam(page, {
      type: 'member_status_change',
      teamName: 'clawbench-review',
      hasLiveMembers: true,
      members: [{
        name: 'review-media',
        agentType: 'general-purpose',
        color: 'blue',
        status: 'completed',
        description: '## 任务\n正文内容',
        toolCallCount: 3,
      }],
    })
    await expect(page.locator('.team-chip')).toBeVisible({ timeout: 5000 })
    await page.locator('.team-chip').click()
    await page.locator('[data-action="team-member-detail"]').first().click()
    await expect(page.locator('.team-member-detail__name')).toBeVisible()

    const pos = await page.evaluate(() => {
      const textLeft = (el: Element) => {
        const range = document.createRange()
        range.selectNodeContents(el)
        return range.getBoundingClientRect().left
      }
      const label = document.querySelector('.team-member-detail__fact-label')!
      const title = document.querySelector('.team-member-detail__section-title')!
      const body = document.querySelector('.team-member-detail__body')!
      const md = document.querySelector('.team-member-detail__markdown')!
      return {
        labelLeft: textLeft(label),
        titleLeft: textLeft(title),
        bodyLeft: body.getBoundingClientRect().left,
        mdLeft: md.getBoundingClientRect().left,
      }
    })

    // Same glyph column as the labels above.
    expect(Math.abs(pos.titleLeft - pos.labelLeft)).toBeLessThanOrEqual(1)
    // No longer hugging the edge: the title sits ~9px in (body padding 2px +
    // grid padding 8px, minus the glyph's own left bearing), versus 2px before.
    expect(pos.titleLeft - pos.bodyLeft).toBeGreaterThan(6)
    // The Markdown body stays flush — long instructions want the width.
    expect(pos.mdLeft - pos.bodyLeft).toBeLessThan(pos.titleLeft - pos.bodyLeft)
  })
})
