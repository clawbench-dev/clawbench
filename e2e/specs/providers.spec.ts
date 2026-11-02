import { test, expect } from '../fixtures'
import type { Page } from '@playwright/test'
import { SettingsPage } from '../pages/settings.page'

/**
 * Model providers: the whole cross-component flow.
 *
 * This is the one screen where a user-defined endpoint + credential is created,
 * refused, bound and deleted — every step involves the real backend
 * (`POST/PATCH/DELETE /api/providers`, the name-uniqueness 409, the "still in
 * use" guard, and `ai_summary.provider_id` in `PATCH /api/config`). Component
 * tests cover the dialog's own logic; only this spec proves the wiring between
 * the list, the dialog, the toast/error surface and the server.
 *
 * The endpoint points at a closed port on purpose: nothing here may depend on
 * an upstream gateway answering, and the "in use" refusal must happen before
 * any connection is attempted.
 *
 * Cleanup is mandatory — all three browser projects share one server and one
 * database, so a leftover provider would show up in every later run.
 */

const RUN = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
const NAME = `E2E Provider ${RUN}`
const ENDPOINT = 'http://127.0.0.1:1'
const PASSWORD = process.env.E2E_PASSWORD || 'e2e-test-password'

/** Providers this spec created (ids), so `afterAll` can remove any leftover. */
const createdIds: number[] = []
/** `ai_summary` before this spec touched it; `undefined` = untouched. */
let summaryBefore: { provider_id: number | null; model: string } | undefined

/** Put the summary-model binding back exactly as it was found. */
async function restoreSummary(page: Page): Promise<void> {
  if (!summaryBefore) return
  await page.request
    .patch('/api/config', {
      data: { ai_summary: { provider_id: summaryBefore.provider_id, model: summaryBefore.model } },
    })
    .catch(() => undefined)
}

interface ProviderRow {
  id: number
  name: string
}

async function listProviders(page: Page): Promise<ProviderRow[]> {
  const res = await page.request.get('/api/providers')
  expect(res.ok(), `GET /api/providers failed: ${res.status()}`).toBe(true)
  const body = (await res.json()) as { providers?: ProviderRow[] }
  return body.providers ?? []
}

/** The id of the one provider carrying our run-unique name, once it exists. */
async function findOurProvider(page: Page): Promise<number> {
  let id = 0
  await expect
    .poll(
      async () => {
        const rows = await listProviders(page)
        id = rows.filter((r) => r.name === NAME)[0]?.id ?? 0
        return id
      },
      { timeout: 10000, message: 'the created provider should appear in GET /api/providers' },
    )
    .toBeGreaterThan(0)
  return id
}

async function openProviders(page: Page): Promise<SettingsPage> {
  const settings = new SettingsPage(page)
  await settings.openSettings()
  // Stable hook: the settings index rows carry their category id (added for
  // this spec), so the assertion does not depend on the UI language.
  await page.locator('[data-category="providers"]').click()
  await expect(page.locator('.provider-setting')).toBeVisible({ timeout: 10000 })
  return settings
}

/** Fill the create dialog and submit it. Leaves the dialog open on failure. */
async function createProvider(page: Page, name: string): Promise<void> {
  await page.locator('[data-action="provider-add"]').click()
  const dialog = page.locator('.modal-dialog').filter({ has: page.locator('.provider-form') })
  await expect(dialog).toBeVisible({ timeout: 5000 })
  await dialog.locator('[data-role="name"]').fill(name)
  await dialog.locator('[data-role="base-url"]').fill(ENDPOINT)
  await dialog.locator('[data-action="provider-save"]').click()
}

/** Click 删除 on our row and confirm the shared dialog. */
async function confirmRemove(page: Page): Promise<void> {
  await page.locator('.provider-row').filter({ hasText: NAME }).locator('[data-action="provider-remove"]').click()
  await expect(page.locator('.dlg-box')).toBeVisible({ timeout: 5000 })
  await page.locator('.dlg-ok').click()
}

test.describe('Model providers', () => {
  test.afterAll(async ({ browser }) => {
    if (createdIds.length === 0 && summaryBefore === undefined) return
    // The test's own `finally` normally leaves nothing to do. This is the net
    // for a run that was aborted after the provider was created: it logs in on
    // a fresh context (the test's page is already gone by now).
    const context = await browser.newContext()
    try {
      const page = await context.newPage()
      const login = await page.request.post('/login', { data: { password: PASSWORD } })
      expect(login.ok(), `cleanup login failed: ${login.status()}`).toBe(true)
      // Restore the summary binding first: while it points at our provider the
      // DELETE below is refused.
      await restoreSummary(page)
      for (const id of createdIds) {
        await page.request.delete(`/api/providers/${id}`)
      }
    } finally {
      await context.close()
    }
  })

  test('creates, refuses a duplicate, refuses to delete while in use, then deletes', async ({ page }) => {
    await openProviders(page)

    // Capture the pre-existing summary binding so cleanup can put it back
    // (a developer machine may already have one configured).
    const configRes = await page.request.get('/api/config')
    expect(configRes.ok()).toBe(true)
    const config = (await configRes.json()) as { ai_summary?: { provider_id?: number | null; model?: string } }
    summaryBefore = {
      provider_id: config.ai_summary?.provider_id ?? null,
      model: config.ai_summary?.model ?? '',
    }

    let createdId = 0
    try {
      // ── 1. Create ────────────────────────────────────────────────────────
      await createProvider(page, NAME)
      const dialog = page.locator('.modal-dialog').filter({ has: page.locator('.provider-form') })
      await expect(dialog).toBeHidden({ timeout: 10000 })

      const row = page.locator('.provider-row').filter({ hasText: NAME })
      await expect(row).toBeVisible({ timeout: 10000 })
      // The endpoint we typed is what the row shows (the list is re-read from
      // the server after saving, not patched locally).
      await expect(row.locator('.provider-summary')).toHaveText(ENDPOINT)

      createdId = await findOurProvider(page)
      createdIds.push(createdId)

      // ── 2. Duplicate name is refused ─────────────────────────────────────
      await createProvider(page, NAME)
      await expect(dialog).toBeVisible()
      // The server answers 409 ProviderNameTaken; the dialog stays open with the
      // typed values and shows the reason (the list-level error is a sibling,
      // so this scopes to the dialog's own error surface).
      await expect(dialog.locator('.provider-error')).toBeVisible({ timeout: 10000 })
      await dialog.locator('[data-action="provider-cancel"]').click()
      await expect(dialog).toBeHidden({ timeout: 5000 })
      // Nothing was added: exactly one row still carries this name.
      await expect(page.locator('.provider-row').filter({ hasText: NAME })).toHaveCount(1)

      // ── 3. In use ⇒ delete refused ───────────────────────────────────────
      // An agent binding is not reachable in this environment: the E2E server
      // only has acp-mock agents, and `model.ProviderEnvSupportedProvider`
      // rejects every backend without a providerEnvTable row (`acp-mock` has
      // none, `custom-acp` is empty), so PATCH /api/agents would answer 400
      // ProviderUnsupported. The summary model is the other real in-use guard
      // on the same DELETE, and it is reachable here.
      const bind = await page.request.patch('/api/config', {
        data: { ai_summary: { provider_id: createdId, model: 'e2e-model' } },
      })
      expect(bind.ok(), `binding the summary model failed: ${bind.status()} ${await bind.text()}`).toBe(true)

      await confirmRemove(page)
      await expect(page.locator('.provider-setting > .provider-error')).toBeVisible({ timeout: 10000 })
      // Still there — the guard refused, the list was not optimistically edited.
      await expect(page.locator('.provider-row').filter({ hasText: NAME })).toHaveCount(1)

      // ── 4. Unbind, then delete ───────────────────────────────────────────
      const unbind = await page.request.patch('/api/config', {
        data: { ai_summary: { provider_id: null } },
      })
      expect(unbind.ok(), `unbinding the summary model failed: ${unbind.status()} ${await unbind.text()}`).toBe(true)

      await confirmRemove(page)
      await expect(page.locator('.provider-row').filter({ hasText: NAME })).toHaveCount(0, { timeout: 10000 })
      await expect
        .poll(async () => (await listProviders(page)).filter((r) => r.id === createdId).length, { timeout: 10000 })
        .toBe(0)
      // Deleted through the UI: nothing left for the cleanup paths to remove.
      createdIds.length = 0
    } finally {
      // Restore the summary binding first: while it points at our provider the
      // DELETE below is refused.
      await restoreSummary(page)
      for (const id of createdIds) {
        await page.request.delete(`/api/providers/${id}`).catch(() => undefined)
      }
      createdIds.length = 0
    }
  })
})
