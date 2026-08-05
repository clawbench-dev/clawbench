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
import { readFileSync, readdirSync, statSync } from 'fs'
import { resolve, join } from 'path'

const PROJECT_ROOT = resolve(__dirname, '..')
const PUBLIC_DIR = join(PROJECT_ROOT, 'public')

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
    'vendor-redoc',
    'vendor-vue',
    'vendor-pdf',
    'vendor-diff',
    'vendor-purify',
]

// ─── Helpers ───────────────────────────────────────────────────────────────────

function findFile(dir: string, prefix: string, suffix = '.js'): string | null {
    try {
        const files = readdirSync(dir)
        const match = files.find(f => f.startsWith(prefix) && f.endsWith(suffix))
        return match ? join(dir, match) : null
    } catch {
        return null
    }
}

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
    const indexHtmlPath = join(PUBLIC_DIR, 'index.html')

    // Skip entire suite if build output doesn't exist (e.g., CI test-only runs)
    let buildExists = false
    try {
        buildExists = statSync(PUBLIC_DIR).isDirectory() && statSync(indexHtmlPath).isFile()
    } catch {
        buildExists = false
    }

    beforeAll(() => {
        if (!buildExists) {
            console.log('  Skipping: public/ build output not found. Run ./build.sh first.')
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
            const indexPath = findFile(PUBLIC_DIR, 'index-', '.js')
            expect(indexPath, 'index-*.js not found in public/').not.toBeNull()

            const size = getFileSize(indexPath!)
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
            const files = readdirSync(PUBLIC_DIR)

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
            // 1. index-*.js (main entry, referenced in <script>)
            // 2. modulepreload links (eagerly loaded by browser)
            const scriptMatch = html.match(/src="([^"]*index-[^"]+\.js)"/)
            expect(scriptMatch, 'index script tag not found').not.toBeNull()

            const modulepreloadLinks = [...html.matchAll(/rel="modulepreload"[^>]*href="([^"]+)"/g)]
                .map(m => m[1])

            // Calculate total first-screen JS size
            let totalBytes = 0
            const details: string[] = []

            // Index chunk
            const indexFile = findFile(PUBLIC_DIR, 'index-', '.js')
            if (indexFile) {
                const size = getFileSize(indexFile)
                totalBytes += size
                details.push(`index: ${formatBytes(size)}`)
            }

            // Modulepreload chunks
            for (const link of modulepreloadLinks) {
                const fileName = link.replace(/^\//, '')
                const filePath = join(PUBLIC_DIR, fileName)
                try {
                    const size = getFileSize(filePath)
                    totalBytes += size
                    details.push(`${fileName}: ${formatBytes(size)}`)
                } catch {
                    details.push(`${fileName}: NOT FOUND`)
                }
            }

            // Threshold: index (2.1MB) + vendor-vue (~170KB) + vendor-purify (~28KB) + vendor-diff (~4KB) = ~2.3MB
            const firstScreenThreshold = 2_400_000

            console.log(`  First-screen JS payload:`)
            for (const d of details) console.log(`    ${d}`)
            console.log(`  Total: ${formatBytes(totalBytes)} (threshold: ${formatBytes(firstScreenThreshold)})`)

            expect(totalBytes).toBeLessThan(firstScreenThreshold)
        })
    })
})
