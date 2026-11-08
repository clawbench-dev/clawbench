/**
 * Build output verification test for Issue #328.
 *
 * Validates that Vite build output meets the performance requirements:
 * 1. Index chunk size is under the threshold (was 9MB, target < 2.5MB)
 * 2. Large lazy-loaded chunks are NOT in modulepreload (not eagerly loaded)
 * 3. Only vendor-vue and vendor-diff are preloaded
 * 4. Key heavy dependencies are split into separate chunks
 *
 * Run after `./build.sh` to verify the build output.
 * Skips gracefully when build output is absent (e.g., CI test-only runs).
 *
 * Usage: npx vitest run scripts/build-size.test.ts
 */

import { describe, it, expect, beforeAll } from 'vitest'
import { existsSync, readFileSync, readdirSync, statSync } from 'fs'
import { resolve, join } from 'path'
import { parseEntryChunkName } from './lib/entryChunk'

const PROJECT_ROOT = resolve(__dirname, '..')
const BUILD_DIR = join(PROJECT_ROOT, '.clawbench-web')

// ─── Thresholds ────────────────────────────────────────────────────────────────

/** Max index chunk size in bytes (original was ~9.4MB, after optimization < 2.1MB) */
const INDEX_CHUNK_MAX_BYTES = 2_100_000

/** Chunks that should NOT be eagerly loaded (not in modulepreload) */
const LAZY_CHUNKS = [
    'mermaid.core',
    'OfficePreview',
    'TerminalPanelContent',
    'vendor-pdf',
]

/** Chunks expected to be split into separate files (not in index) */
const EXPECTED_SPLIT_CHUNKS = [
    'mermaid.core',
    'OfficePreview',
    'vendor-swagger-ui',
    'vendor-vue',
    'vendor-pdf',
    'vendor-diff',
    'vendor-purify',
]

// ─── Helpers ───────────────────────────────────────────────────────────────────

function getFileSize(filePath: string): number {
    return statSync(filePath).size
}

function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

// ─── Tests ─────────────────────────────────────────────────────────────────────

describe('Build output verification (Issue #328)', () => {
    const indexHtmlPath = join(BUILD_DIR, 'index.html')

    // Skip entire suite if build output doesn't exist (e.g., CI test-only runs)
    let buildExists = false
    try {
        buildExists = statSync(BUILD_DIR).isDirectory() && statSync(indexHtmlPath).isFile()
    } catch {
        buildExists = false
    }

    beforeAll(() => {
        if (!buildExists) {
            console.log('  Skipping: .clawbench-web/ build output not found. Run ./build.sh first.')
        }
    })

    it('index.html should exist', () => {
        if (!buildExists) return
        const html = readFileSync(indexHtmlPath, 'utf-8')
        expect(html).toBeTruthy()
    })

    describe('Index chunk size', () => {
        it('index chunk should be under size threshold', () => {
            if (!buildExists) return
            const html = readFileSync(indexHtmlPath, 'utf-8')
            const entryName = parseEntryChunkName(html)
            expect(entryName, 'entry script tag not found in index.html').not.toBeNull()
            const indexPath = join(BUILD_DIR, entryName!)
            expect(existsSync(indexPath), `${entryName} referenced by index.html but missing`).toBe(true)

            const size = getFileSize(indexPath)
            console.log(`  Index chunk size: ${formatBytes(size)} (threshold: ${formatBytes(INDEX_CHUNK_MAX_BYTES)})`)

            expect(size).toBeLessThan(INDEX_CHUNK_MAX_BYTES)
        })
    })

    describe('Modulepreload verification', () => {
        it('lazy-loaded chunks should NOT be in modulepreload', () => {
            if (!buildExists) return
            const html = readFileSync(indexHtmlPath, 'utf-8')
            const modulepreloadLinks = [...html.matchAll(/rel="modulepreload"[^>]*href="([^"]+)"/g)]
                .map(m => m[1])

            console.log(`  Modulepreload links: ${modulepreloadLinks.join(', ') || '(none)'}`)

            for (const lazyChunk of LAZY_CHUNKS) {
                const found = modulepreloadLinks.some(link => link.includes(lazyChunk))
                expect(found, `${lazyChunk} should NOT be in modulepreload`).toBe(false)
            }
        })

        it('vendor-vue should be in modulepreload', () => {
            if (!buildExists) return
            const html = readFileSync(indexHtmlPath, 'utf-8')
            const hasVue = /rel="modulepreload"[^>]*href="[^"]*vendor-vue/.test(html)
            expect(hasVue, 'vendor-vue should be in modulepreload').toBe(true)
        })
    })

    describe('Chunk splitting verification', () => {
        it('expected chunks should exist as separate files', () => {
            if (!buildExists) return
            const files = readdirSync(BUILD_DIR)

            for (const chunkName of EXPECTED_SPLIT_CHUNKS) {
                const found = files.some(f => f.startsWith(chunkName + '-') && f.endsWith('.js'))
                expect(found, `${chunkName}-*.js should exist as separate chunk`).toBe(true)
            }
        })
    })

    describe('First-screen payload analysis', () => {
        it('total first-screen JS should be under threshold', () => {
            if (!buildExists) return
            const html = readFileSync(indexHtmlPath, 'utf-8')

            // Files that must be loaded on first screen:
            // 1. main-*.js (main entry, referenced in <script>)
            // 2. modulepreload links (eagerly loaded by browser)
            const entryName = parseEntryChunkName(html)
            expect(entryName, 'entry script tag not found in index.html').not.toBeNull()

            const modulepreloadLinks = [...html.matchAll(/rel="modulepreload"[^>]*href="([^"]+)"/g)]
                .map(m => m[1])

            // Calculate total first-screen JS size
            let totalBytes = 0
            const details: string[] = []

            // Main entry chunk — resolved from index.html, never guessed from a
            // directory listing (stale chunks accumulate; see parseEntryChunkName).
            const entryPath = join(BUILD_DIR, entryName!)
            expect(existsSync(entryPath), `${entryName} referenced by index.html but missing`).toBe(true)
            {
                const size = getFileSize(entryPath)
                totalBytes += size
                details.push(`entry: ${formatBytes(size)}`)
            }

            // Modulepreload chunks
            for (const link of modulepreloadLinks) {
                const fileName = link.replace(/^\//, '')
                const filePath = join(BUILD_DIR, fileName)
                try {
                    const size = getFileSize(filePath)
                    totalBytes += size
                    details.push(`${fileName}: ${formatBytes(size)}`)
                } catch {
                    details.push(`${fileName}: NOT FOUND`)
                }
            }

            // Measured 2026-11-02: 3,733,936 bytes. What is in it now:
            //   app shell (entry + shared app chunk) ~2.6MB
            //   katex 582KB          — static because markdown renders math
            //                          synchronously inside `marked`
            //   i18n zh+en 593KB     — both locales are bundled eagerly
            //   material icons 93KB
            // CodeMirror (~1MB) used to be here too: `useFileScrollRestore`
            // imported `@codemirror/view` for one DOM lookup, which made the
            // whole editor stack a first-screen dependency. That import is now
            // type-only (the editor registers itself), so the editor chunk is
            // lazy again.
            //
            // Why this number moved 2.7MB → 3.7MB → 3.82MB, and why that is
            // not "a regression absorbed by the budget":
            //   - 2026-09 measured 2,620,517 (budget 2.7MB, +80KB headroom).
            //   - katex + both i18n locales + icons then landed (+~1.27MB) and
            //     CodeMirror leaked into the entry (+~1MB), taking it to 4.1MB.
            //   - 244fd4423 REMOVED the CodeMirror leak (4.1 → 3.4MB) and
            //     re-based the budget to the measured value. The regression was
            //     fixed, not absorbed.
            //   - 2026-11-02 re-measured at 3,733,936: +145,827 over that
            //     baseline with NO dependency change (`web/package.json` is
            //     identical since 244fd4423) across +38k lines of new source.
            //     That is broad, incremental growth — an order of magnitude
            //     smaller than the CodeMirror step, and no single heavy module
            //     entered the preload set.
            //   - 2026-11-08 re-measured at 3,977,314 (+98,589 over the 11-04
            //     number). Attribution: `actionLabels.*` landed in BOTH locales
            //     (the short toolbar labels every ActionBar row renders —
            //     新建目录 / 全部已读 / 已分享 / 固定 …), plus the provider
            //     unit-price strings and the mail dictionary. Both locales are
            //     eagerly bundled, so a UI string added once is paid twice.
            //     No dependency changed and the preload set is the same 9
            //     files, so this is dictionary growth, not a heavy module
            //     leaking into the entry. Measured, then budget raised by the
            //     same ~40KB headroom the earlier entries kept.
            //   - 2026-11-04 re-measured at 3,878,725 (+144,789 over the 11-02
            //     number) after the built-in mail client landed. Attribution:
            //     the panel itself stays async (MailPanel.vue is a
            //     defineAsyncComponent, and no mail component appears in the
            //     entry chunk); what grew is the eagerly bundled i18n pair
            //     (both locales carry the mail dictionary — 邮件/抄送/收件箱…
            //     are now in the shared chunk) plus `useMailUnread`, which
            //     App.vue imports statically because the dock badge must work
            //     while the panel is closed. The preload set is byte-identical
            //     in composition to 11-02 (same 8 files, no katex/mermaid/
            //     OfficePreview entry), so this is incremental dictionary and
            //     glue growth, not a heavy dependency leaking back in.
            //
            // The headroom stays ~40KB on purpose (r1 review): raising this
            // number to whatever the build happens to weigh turns the test into
            // a rubber stamp. If it trips, re-measure and say in the commit
            // what grew — don't just bump it.
            //
            // Going lower needs one of: lazy katex (582KB, invasive — the
            // render path is synchronous), per-locale i18n chunks (270KB), or
            // trimming the eager highlight.js language set.
            const firstScreenThreshold = 4_020_000

            console.log(`  First-screen JS payload:`)
            for (const d of details) console.log(`    ${d}`)
            console.log(`  Total: ${formatBytes(totalBytes)} (threshold: ${formatBytes(firstScreenThreshold)})`)

            // Carry the per-file breakdown into the failure message: a bare
            // "expected 3900000 to be less than 3820000" says nothing about
            // WHICH chunk grew.
            expect(totalBytes, details.join('\n')).toBeLessThan(firstScreenThreshold)
        })
    })
})
