import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { writeFileSync, mkdtempSync, mkdirSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'

/**
 * Knowledge base, end to end through the real UI + real Go backend:
 *
 *   create collection → upload → index → search → duplicate/unsupported errors
 *   → reindex → per-document delete → chat with/without auto-injection
 *   → citation chip + source expansion → reload persistence → delete collection
 *
 * The server config has RAG disabled, so indexing stays on the keyword path —
 * that is the intended degraded mode, not a failure. The chat turn uses the
 * real ACP mock agent, so the citation metadata travels the same path a real
 * reply does.
 */
test.describe('Knowledge base end to end', () => {
  test('full panel flow, chat citations and persistence', async ({ page }) => {
    test.setTimeout(180_000)

    const chat = new ChatPage(page)
    await chat.waitForIdle()

    const stamp = `${process.pid}-${Date.now()}`
    const colName = `E2E制度-${stamp}`
    const workDir = mkdtempSync(join(tmpdir(), 'clawbench-kb-e2e-'))
    const mainName = `handbook-${stamp}.md`
    const mainPath = join(workDir, mainName)
    const secondName = `temp-${stamp}.md`
    const secondPath = join(workDir, secondName)
    const legacyName = `legacy-${stamp}.doc`
    const legacyPath = join(workDir, legacyName)
    writeFileSync(mainPath, '报销流程：先提交申请，再等财务审批。\n年假天数按工龄计算。\n')
    writeFileSync(secondPath, '这是一个稍后会被删除的临时文件。\n')
    writeFileSync(legacyPath, 'legacy word file')

    // A real folder INSIDE the project: the picker browses the project root, and
    // a folder pick means "import its whole subtree".
    const dirName = `kb-dir-${stamp}`
    const dirPath = join(process.cwd(), dirName)
    const dirFileA = `folder-a-${stamp}.md`
    const dirFileB = `folder-b-${stamp}.md`
    mkdirSync(join(dirPath, 'nested'), { recursive: true })
    writeFileSync(join(dirPath, dirFileA), '差旅标准：市内交通每日上限 100 元。\n')
    writeFileSync(join(dirPath, 'nested', dirFileB), '年假：满一年 5 天。\n')
    writeFileSync(join(dirPath, `skip-${stamp}.png`), 'png')

    let collectionId = ''
    let sessionId = ''

    // The dock's active tab click TOGGLES the left column, and opening a chat
    // session can leave the column collapsed. Make "show me the KB panel"
    // idempotent instead of assuming one click always expands.
    const openKBPanel = async () => {
      const panel = page.locator('.kb-panel')
      if (await panel.isVisible().catch(() => false)) return
      // One click is enough: the tab is either a different tab (switch +
      // expand) or the active one while collapsed (toggle open). A second
      // click would collapse it again — and on Firefox/WebKit the first
      // click's DOM update is not synchronous, which is exactly how the
      // "click twice" version ended up hidden.
      await page.locator('[data-tab="kb"]').click()
      await expect(panel).toBeVisible({ timeout: 15000 })
    }

    // "Add file" opens the SAME attachment sheet the chat composer uses, so the
    // test drives the real flow: pick/upload into the draft, then confirm.
    const openAddSheet = async () => {
      await page.locator('[data-action="kb-add"]').click()
      const sheet = page.locator('.bs-panel').filter({ has: page.locator('.ad-header') })
      await expect(sheet).toBeVisible({ timeout: 10000 })
      return sheet
    }

    const uploadIntoSheet = async (sheet: import('@playwright/test').Locator, filePath: string) => {
      await sheet.locator('input[type="file"]:not([accept])').setInputFiles(filePath)
      // The file is committed to the draft once its transfer finishes; only
      // then does OK mean "add this".
      await expect(sheet.locator('[data-role="attach-selected-count"]')).toBeVisible({ timeout: 30000 })
      await sheet.locator('[data-action="attach-ok"]').click()
    }

    const cleanup = async () => {
      try {
        const cols = await page.evaluate(async () => {
          const resp = await fetch('/api/kb/collections')
          if (!resp.ok) return []
          const data = await resp.json()
          return (data.collections || []) as Array<{ id: number; name: string }>
        })
        for (const c of cols.filter((c) => c.name.startsWith('E2E制度-'))) {
          await page.evaluate(async (id) => {
            await fetch(`/api/kb/collections?id=${id}`, { method: 'DELETE' })
          }, c.id)
        }
      } catch {
        // Cleanup is best effort; a failed delete must not hide the real result.
      }
      if (sessionId) {
        try {
          await page.evaluate(async (sid) => {
            await fetch(`/api/share/session?session_id=${encodeURIComponent(sid)}`, { method: 'DELETE' })
          }, sessionId)
        } catch {
          // Best effort.
        }
      }
      rmSync(workDir, { recursive: true, force: true })
      rmSync(dirPath, { recursive: true, force: true })
    }

    try {
      await test.step('open the knowledge base tab', async () => {
        await openKBPanel()
        // Wide panel: list and detail share the width (the mail-style split).
        await expect(page.locator('.kb-split')).toHaveClass(/split-view--active/)
        await expect(page.locator('.kb-rail')).toBeVisible()
        await expect(page.locator('.kb-main')).toBeVisible()
      })

      await test.step('create a global collection', async () => {
        await page.locator('[data-action="kb-new"]').click()
        const form = page.locator('.kb-form')
        await expect(form).toBeVisible()
        await form.locator('[data-role="kb-name"]').fill(colName)
        await form.locator('[data-role="kb-description"]').fill('E2E 自动测试')
        // Scope is a segmented control; the buttons live in the shared modal
        // footer, outside the form body.
        await form.locator('[data-role="kb-scope-global"]').click()
        await page.locator('.modal-footer .fbtn-primary').click()
        await expect(form).toBeHidden({ timeout: 10000 })

        const row = page.locator('.kb-col').filter({ hasText: colName })
        await expect(row).toBeVisible({ timeout: 10000 })
        collectionId = (await row.getAttribute('data-kb-collection')) || ''
        expect(collectionId).not.toBe('')

        await row.click()
        await expect(page.locator('.kb-main-name')).toContainText(colName)
        await expect(page.locator('.kb-main-name .kb-scope')).toContainText(/全局|Global/)
      })

      await test.step('upload and index a markdown file', async () => {
        const sheet = await openAddSheet()
        await uploadIntoSheet(sheet, mainPath)
        await expect(sheet).toBeHidden({ timeout: 20000 })

        const doc = page.locator('.kb-doc').filter({ hasText: mainName })
        await expect(doc).toBeVisible({ timeout: 20000 })
        // RAG is disabled in the E2E config, so "ready" is the keyword-only
        // state (class kb-status--fts), not the fully vectorised one.
        await expect(doc.locator('.kb-status')).toHaveClass(/kb-status--(fts|ready)/, { timeout: 30000 })
        await expect(doc).toContainText(/段|chunk/i)
        await expect(page.locator('.kb-col').filter({ hasText: colName })).toContainText(/1 个文件|1 file/)
      })

      await test.step('search returns the highlighted passage with its location', async () => {
        await page.locator('.kb-search input').fill('报销')
        await page.locator('[data-action="kb-search"]').click()

        const hit = page.locator('[data-kb-hit]').first()
        await expect(hit).toBeVisible({ timeout: 15000 })
        await expect(hit.locator('.kb-result-title')).toContainText(mainName)
        await expect(hit.locator('mark')).toContainText('报销')
        await expect(hit.locator('.kb-result-loc')).not.toBeEmpty()
        await expect(hit.locator('.kb-result-col')).toContainText(colName)

        // Switching the search mode re-runs the query; keyword-only mode must
        // return the same passage.
        await page.locator('.kb-search-row select').nth(1).selectOption('fts')
        await expect(page.locator('[data-kb-hit]').first()).toBeVisible({ timeout: 15000 })
        await page.locator('.kb-results .kb-link').click()
        await expect(page.locator('.kb-doc').filter({ hasText: mainName })).toBeVisible()
      })

      await test.step('open a hit in the document drawer', async () => {
        await page.locator('.kb-search input').fill('报销')
        await page.locator('[data-action="kb-search"]').click()
        const hit = page.locator('[data-kb-hit]').first()
        await expect(hit).toBeVisible({ timeout: 15000 })
        await hit.click()

        const drawer = page.locator('.bs-panel').filter({ has: page.locator('.kb-drawer') })
        await expect(drawer).toBeVisible({ timeout: 10000 })
        await expect(drawer.locator('.kb-drawer-title')).toContainText(mainName)
        await expect(drawer.locator('[data-kb-chunk]').first()).toBeVisible()
        // The clicked passage is the one highlighted in the drawer.
        await expect(drawer.locator('[data-kb-chunk].is-focus')).toHaveCount(1)

        await drawer.locator('.modal-close-btn').click()
        await expect(drawer).toBeHidden({ timeout: 10000 })

        // Back to the document list: the next steps need the collection header.
        await page.locator('.kb-results .kb-link').click()
        await expect(page.locator('.kb-doc').filter({ hasText: mainName })).toBeVisible()
      })

      await test.step('duplicate and unsupported uploads are rejected with a reason', async () => {
        // A rejection keeps the sheet open with the selection intact, so the
        // user can drop the bad file and retry without browsing again.
        const dup = await openAddSheet()
        await uploadIntoSheet(dup, mainPath)
        await expect(page.locator('.toast-text')).toContainText(/已经|already/i, { timeout: 15000 })
        await dup.locator('[data-action="attach-cancel"]').click()
        await expect(dup).toBeHidden({ timeout: 10000 })

        const legacy = await openAddSheet()
        await uploadIntoSheet(legacy, legacyPath)
        await expect(page.locator('.toast-text')).toContainText(/docx|旧版|Legacy/i, { timeout: 15000 })
        await legacy.locator('[data-action="attach-cancel"]').click()
        await expect(legacy).toBeHidden({ timeout: 10000 })
      })

      await test.step('delete one document without touching the collection', async () => {
        const sheet = await openAddSheet()
        await uploadIntoSheet(sheet, secondPath)
        await expect(sheet).toBeHidden({ timeout: 20000 })

        const second = page.locator('.kb-doc').filter({ hasText: secondName })
        await expect(second).toBeVisible({ timeout: 20000 })
        await expect(second.locator('.kb-status')).toHaveClass(/kb-status--(fts|ready)/, { timeout: 30000 })

        await second.locator('[data-action="kb-delete-doc"]').click()
        await page.locator('.dlg-ok').click()
        await expect(second).toHaveCount(0, { timeout: 15000 })
        await expect(page.locator('.kb-doc').filter({ hasText: mainName })).toBeVisible()
        await expect(page.locator('.kb-col').filter({ hasText: colName })).toContainText(/1 个文件|1 file/)
      })

      await test.step('add a whole folder in one pick', async () => {
        const sheet = await openAddSheet()
        // The sheet remembers the tab it was last on, and the previous steps
        // uploaded files — folders live on the Files tab.
        await sheet.locator('.ad-tab').first().click()
        const row = sheet.locator(`[data-path="${dirName}"]`)
        await expect(row).toBeVisible({ timeout: 10000 })
        await row.locator('[data-action="fp-pick-dir"]').click()
        await expect(sheet.locator('[data-role="attach-selected-count"]')).toBeVisible()
        await sheet.locator('[data-action="attach-ok"]').click()

        // Folders are previewed first: the counts are shown before anything is
        // copied, and the import starts only after this confirmation.
        const preview = page.locator('.dlg-msg')
        await expect(preview).toContainText(/将加入|to add/, { timeout: 15000 })
        await page.locator('.dlg-ok').click()
        await expect(sheet).toBeHidden({ timeout: 30000 })

        // Both nested files landed; the image was skipped and the summary says so.
        // TODO(flaky): one observed run imported only the top-level file (the
        // nested one never appeared, and the list showed N-1 docs). The walk is
        // synchronous and covered by TestImportDirectory_AddsSupportedFilesRecursively,
        // so the suspicion is an environment hiccup, not a selector. If it
        // recurs, capture the import summary toast first — its added/skipped
        // counts say whether the server or the UI lost the file.
        await expect(page.locator('.kb-doc').filter({ hasText: dirFileA })).toBeVisible({ timeout: 30000 })
        await expect(page.locator('.kb-doc').filter({ hasText: dirFileB })).toBeVisible({ timeout: 30000 })
      })

      await test.step('reindex the collection and wait until it is searchable again', async () => {
        await page.locator('[data-action="kb-reindex-collection"]').click()
        await page.locator('.dlg-ok').click()

        const doc = page.locator('.kb-doc').filter({ hasText: mainName })
        await expect(doc.locator('.kb-status')).toHaveClass(/kb-status--(fts|ready)/, { timeout: 30000 })
      })

      await test.step('turn on the global auto-injection switch', async () => {
        // kb.auto_inject is opt-in (default off); the per-collection toggle only
        // refines it, so the global switch must be on before either branch below
        // can prove anything about the per-collection gate.
        const status = await page.evaluate(async () => {
          const resp = await fetch('/api/config', {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ kb: { auto_inject: true } }),
          })
          return resp.status
        })
        expect(status).toBe(200)
      })

      await test.step('with auto-injection off, a reply carries no citation marker', async () => {
        await page.locator('[data-action="kb-toggle-auto-inject"]').click()
        await expect(page.locator('[data-action="kb-toggle-auto-inject"]')).toHaveClass(/is-off/)

        await chat.createSessionWithAgent('acp-mock')
        await expect(chat.textarea).toBeVisible({ timeout: 10000 })
        await chat.sendAndAwaitACPReply('报销流程是什么', 45000)

        const last = chat.getLastAssistantMessage()
        await expect(last.locator('[data-action="toggle-kb-refs"]')).toHaveCount(0)
      })

      await test.step('with auto-injection on, the reply cites the knowledge base', async () => {
        await openKBPanel()
        await page.locator('.kb-col').filter({ hasText: colName }).click()
        await page.locator('[data-action="kb-toggle-auto-inject"]').click()
        await expect(page.locator('[data-action="kb-toggle-auto-inject"]')).not.toHaveClass(/is-off/)

        await chat.sendAndAwaitACPReply('再问一次：报销流程是什么', 45000)

        const last = chat.getLastAssistantMessage()
        const chip = last.locator('[data-action="toggle-kb-refs"]')
        await expect(chip).toBeVisible({ timeout: 30000 })
        await expect(chip).toContainText(/参考了知识库|Referenced/)

        await chip.click()
        const sources = last.locator('.chat-kb-ref')
        await expect(sources).toHaveCount(1)
        await expect(sources.first().locator('.chat-kb-ref-title')).toContainText(mainName)
        await expect(sources.first().locator('.chat-kb-ref-text')).toContainText('财务审批')
        await expect(sources.first().locator('.chat-kb-ref-location')).not.toBeEmpty()
      })

      await test.step('the citation survives a full page reload', async () => {
        sessionId = await page.evaluate(async () => {
          const resp = await fetch('/api/ai/sessions')
          if (!resp.ok) return ''
          const data = await resp.json()
          return ((data.sessions || []) as Array<{ id: string }>)[0]?.id || ''
        })
        expect(sessionId).not.toBe('')

        await page.reload()
        await expect(chat.textarea).toBeVisible({ timeout: 20000 })
        await page.waitForFunction(() => !!(window as any).__clawbench?.switchSession)
        await page.evaluate(async (sid) => {
          await (window as any).__clawbench.switchSession(sid)
        }, sessionId)
        await expect(chat.textarea).toBeVisible({ timeout: 15000 })

        const chip = page.locator('.chat-message.assistant [data-action="toggle-kb-refs"]').last()
        await expect(chip).toBeVisible({ timeout: 30000 })
        await chip.click()
        await expect(page.locator('.chat-message.assistant .chat-kb-ref').last()).toContainText(mainName)
      })

      await test.step('a public share link does not leak the knowledge base snippets', async () => {
        const share = await page.evaluate(async (sid) => {
          const resp = await fetch('/api/share/session', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ sessionId: sid }),
          })
          if (!resp.ok) return { token: '' }
          return (await resp.json()) as { token?: string }
        }, sessionId)
        expect(share.token).toBeTruthy()

        const sharePage = await page.context().newPage()
        try {
          await sharePage.goto(`/share/${share.token}`)
          await expect(sharePage.locator('.chat-message.assistant').first()).toBeVisible({ timeout: 30000 })
          await expect(sharePage.locator('[data-action="toggle-kb-refs"]')).toHaveCount(0)
        } finally {
          await sharePage.close()
        }
      })

      await test.step('delete the collection from the panel', async () => {
        await openKBPanel()
        const row = page.locator('.kb-col').filter({ hasText: colName })
        await expect(row).toBeVisible({ timeout: 15000 })
        await row.click()
        await page.locator('[data-action="kb-delete-collection"]').click()
        await page.locator('.dlg-ok').click()
        await expect(row).toHaveCount(0, { timeout: 15000 })
      })
    } finally {
      await cleanup()
    }
  })
})

/**
 * Narrow layout: the KB panel becomes a two-level drill-down (rail → detail)
 * instead of the side-by-side desktop split. The search row must stay usable
 * at 390px, and the back button must return to the rail.
 */
test.describe('Knowledge base on a phone-width viewport', () => {
  test('drill-down list → detail → search → back', async ({ page }) => {
    test.setTimeout(90_000)
    const stamp = `${process.pid}-${Date.now()}`
    const colName = `E2E手机-${stamp}`
    const fileName = `mobile-${stamp}.md`

    const created = await page.request.post('/api/kb/collections', {
      data: { name: colName, description: 'narrow', scope: 'global' },
    })
    expect(created.ok()).toBeTruthy()
    const col = (await created.json()) as { id: number }

    try {
      const uploaded = await page.request.post('/api/kb/documents/upload', {
        multipart: {
          collection_id: String(col.id),
          file: {
            name: fileName,
            mimeType: 'text/markdown',
            buffer: Buffer.from('差旅报销：市内交通每日上限 100 元。\n', 'utf8'),
          },
        },
      })
      expect(uploaded.ok()).toBeTruthy()

      // Wait for the keyword index (RAG is off in this server config).
      await expect
        .poll(async () => {
          const resp = await page.request.get(`/api/kb/documents?collection_id=${col.id}`)
          const data = (await resp.json()) as { documents?: Array<{ status: string }> }
          return data.documents?.[0]?.status
        }, { timeout: 30000, intervals: [200, 500, 1000] })
        .toBe('ready')

      await page.setViewportSize({ width: 390, height: 844 })
      await page.reload()
      await expect(page.locator('.bottom-dock')).toBeVisible({ timeout: 20000 })

      // KB is a secondary tab on a phone: it may live in the dock's overflow
      // menu instead of being rendered directly.
      const direct = page.locator('.bottom-dock [data-tab="kb"]')
      if (await direct.count()) {
        await direct.first().click()
      } else {
        await page.locator('.dock-overflow-btn').click()
        await page.locator('.dock-overflow-item').filter({ hasText: /知识库|Knowledge base/ }).click()
      }
      await expect(page.locator('.kb-panel')).toBeVisible({ timeout: 15000 })
      // Phone: one pane at a time (drill down), not the split.
      await expect(page.locator('.kb-split')).not.toHaveClass(/split-view--active/)
      await expect(page.locator('.kb-rail')).toBeVisible()

      const row = page.locator('.kb-col').filter({ hasText: colName })
      await expect(row).toBeVisible({ timeout: 15000 })
      await row.click()

      // Detail view replaces the rail; the back button is the only way out.
      await expect(page.locator('.kb-main-name')).toContainText(colName)
      await expect(page.locator('.kb-rail')).toBeHidden()
      await expect(page.locator('.kb-back')).toBeVisible()

      // The name must stay on ONE line: the action bar used to win the width
      // contest and squeezed it into a vertical one-character column.
      const nameBox = await page.locator('.kb-main-name-text').boundingBox()
      expect(nameBox?.width ?? 0).toBeGreaterThan(80)
      expect(nameBox?.height ?? 99).toBeLessThan(40)

      // "Add file" opens the shared attachment sheet: the same upload entry and
      // the two tabs (files / recent uploads) the chat composer has. The
      // camera/photo entries are host-gated, not width-gated, so they only
      // appear in a real mobile UA — covered by AttachmentPicker.test.ts.
      await page.locator('[data-action="kb-add"]').click()
      const sheet = page.locator('.bs-panel').filter({ has: page.locator('.ad-header') })
      await expect(sheet).toBeVisible({ timeout: 10000 })
      await expect(sheet.locator('.ad-header-upload-btn')).toBeVisible()
      await expect(sheet.locator('.ad-tab')).toHaveCount(2)
      await expect(sheet.locator('.ad-tab').first()).toContainText(/文件|Files/)
      await sheet.locator('[data-action="attach-cancel"]').click()
      await expect(sheet).toBeHidden({ timeout: 10000 })

      await page.locator('.kb-search input').fill('报销')
      await page.locator('[data-action="kb-search"]').click()
      const hit = page.locator('[data-kb-hit]').first()
      await expect(hit).toBeVisible({ timeout: 15000 })
      await expect(hit.locator('.kb-result-title')).toContainText(fileName)
      await expect(hit.locator('mark')).toContainText('报销')

      // Search results replace the detail pane (there is one pane at a time on
      // a phone), so the way back is: close the results, then leave the
      // collection.
      await page.locator('.kb-results .kb-link').click()
      await expect(page.locator('.kb-main-name')).toContainText(colName)

      await page.locator('.kb-back').click()
      await expect(page.locator('.kb-rail')).toBeVisible()
      await expect(page.locator('.kb-main-name')).toHaveCount(0)
    } finally {
      await page.request.delete(`/api/kb/collections?id=${col.id}`)
    }
  })
})
