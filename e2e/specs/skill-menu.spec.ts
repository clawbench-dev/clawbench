import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { apiFetch } from '../helpers/auth'

/**
 * E2E tests for the "#" skill menu.
 *
 * A skill is a directory containing SKILL.md. ClawBench scans several sources
 * (user dirs, git repos, native agent dirs) and the chat input offers them via a
 * dedicated "#" trigger — separate from "/" (built-in + agent commands) and "@"
 * (files/members). Selecting one inserts "#name "; the backend then injects an
 * instruction to read that SKILL.md.
 *
 * The injection itself is covered by Go unit tests
 * (internal/handler/skill_command_test.go), which can assert the rendered
 * prompt. What only a real browser can verify is the MENU: that "#" opens it,
 * that it is not confused with "/" or "@", and that selecting inserts the
 * reference. That is what this spec covers.
 *
 * SKILLS DISABLED BY DEFAULT IN E2E: the temp config sets no `skills` section,
 * and the default user skills dir is the real ~/.agents/skills (non-hermetic).
 * The spec PATCHes `skills.dirs` to a directory it seeds itself, then restores
 * it in afterAll so no other spec inherits the setting.
 */
test.describe.serial('Skill menu', () => {
  test.setTimeout(60000)

  let chat: ChatPage

  /** A skill dir seeded for this spec; restored (cleared) in afterAll. */
  const SEEDED_DIR = '/tmp/clawbench-e2e-skills'

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  test.afterAll(async () => {
    // Clear the seeded dir so a later spec that opens settings does not list it.
    await apiFetch('/api/config', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ skills: { dirs: [] } }),
    })
  })

  test('seeds a skill via skills.dirs and offers it in the # menu', async ({ page }) => {
    // Seed a skill through the filesystem (the server scans it) and point the
    // config at the directory. PATCH performs a synchronous local rescan, so
    // the skill is visible immediately (no git sync involved).
    const fs = await import('node:fs')
    const path = await import('node:path')
    const skillDir = path.join(SEEDED_DIR, 'e2e-demo-skill')
    fs.mkdirSync(skillDir, { recursive: true })
    fs.writeFileSync(
      path.join(skillDir, 'SKILL.md'),
      '---\nname: e2e-demo-skill\ndescription: A skill seeded by the E2E test\n---\n\nDo the demo.\n',
    )

    const patch = await apiFetch('/api/config', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ skills: { dirs: [SEEDED_DIR] } }),
    })
    expect(patch.ok).toBe(true)

    await page.goto('/')
    await expect(chat.textarea).toBeVisible({ timeout: 10000 })

    // Typing "#" opens the skill menu (its own source class), not the "/" one.
    await chat.textarea.click()
    await chat.textarea.fill('#')

    const skillItems = page.locator('.completion-item--skill')
    await expect(skillItems.first()).toBeVisible({ timeout: 5000 })

    // The row shows the bare name; the "#" is only in the inserted text.
    const demo = skillItems.filter({ hasText: 'e2e-demo-skill' })
    await expect(demo).toBeVisible()

    // The per-row source tag is omitted for skills (it stole width).
    await expect(demo.locator('.completion-source')).not.toBeVisible()

    // The "/" menu must NOT be showing at the same time.
    await expect(page.locator('.completion-item--clawbench').first()).not.toBeVisible()

    // Selecting inserts "#e2e-demo-skill " into the input.
    await demo.click()
    await expect(chat.textarea).toHaveValue('#e2e-demo-skill ')
  })

  test('opens the skill menu for a # that starts a token mid-message', async ({ page }) => {
    // Issue #548: describing the problem first and naming the skill afterwards
    // must work — "#" is ClawBench's own protocol, so the server scans tokens
    // anywhere in the message.
    await page.goto('/')
    await expect(chat.textarea).toBeVisible({ timeout: 10000 })

    await chat.textarea.click()
    await chat.textarea.fill('summarize this file #e2e')

    const demo = page.locator('.completion-item--skill').filter({ hasText: 'e2e-demo-skill' })
    await expect(demo).toBeVisible({ timeout: 5000 })

    // Selecting replaces only the "#e2e" token, keeping the leading prose.
    await demo.click()
    await expect(chat.textarea).toHaveValue('summarize this file #e2e-demo-skill ')
  })

  test('does not open the skill menu for a # inside a word', async ({ page }) => {
    await page.goto('/')
    await expect(chat.textarea).toBeVisible({ timeout: 10000 })

    await chat.textarea.click()
    await chat.textarea.fill('use#e2e')

    await expect(page.locator('.completion-item--skill').first()).not.toBeVisible({ timeout: 1500 })
  })

  test('does not open the skill menu when typing / or @', async ({ page }) => {
    await page.goto('/')
    await expect(chat.textarea).toBeVisible({ timeout: 10000 })

    await chat.textarea.click()
    await chat.textarea.fill('/')
    await expect(page.locator('.completion-item--skill').first()).not.toBeVisible({ timeout: 1000 })

    await chat.textarea.fill('@')
    await expect(page.locator('.completion-item--skill').first()).not.toBeVisible({ timeout: 1000 })
  })
})
