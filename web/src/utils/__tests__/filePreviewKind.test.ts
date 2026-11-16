import { describe, expect, it } from 'vitest'
import { FILE_TYPES } from '../fileType'
import { FILETYPE_FLAG_KINDS, filePreviewKind } from '../filePreviewKind'

const KNOWN_FLAGS = new Set(Object.keys(FILETYPE_FLAG_KINDS))

/**
 * Drift guard for "every file type the app knows has a quick-preview body".
 *
 * The quick preview used to fall back to raw text for any type it did not
 * branch on, so adding a renderer to the full-screen viewer (CSV tables, office
 * documents, diagrams) silently left the card showing a code slice. These tests
 * fail when the two lists diverge:
 *
 *  - a new `isX` renderer flag on FileType with no preview kind, and
 *  - a registered extension whose declared flag no longer matches its kind.
 *
 * The companion mount test lives in CodeLinkPreview.test.ts ("quick-preview
 * type coverage"), which proves each kind actually renders a body.
 */
describe('filePreviewKind — quick-preview type coverage', () => {
    it('has a preview kind for every FileType renderer flag', () => {
        for (const fileType of FILE_TYPES) {
            for (const [key, value] of Object.entries(fileType)) {
                if (value !== true || !key.startsWith('is')) continue
                expect(
                    KNOWN_FLAGS.has(key),
                    `${fileType.exts[0]} declares ${key}, but filePreviewKind.ts has no kind for it`,
                ).toBe(true)
            }
        }
    })

    it('classifies every registered extension, matching its declared flag', () => {
        for (const fileType of FILE_TYPES) {
            const declared = Object.keys(FILETYPE_FLAG_KINDS).filter(
                flag => (fileType as unknown as Record<string, unknown>)[flag] === true,
            )
            expect(
                declared.length,
                `${fileType.exts[0]} declares more than one renderer flag: ${declared.join(', ')}`,
            ).toBeLessThanOrEqual(1)

            for (const ext of fileType.exts) {
                const name = `sample${ext}`
                const kind = filePreviewKind(name)
                if (declared.length === 1) {
                    expect(kind, `${name} declares ${declared[0]}`).toBe(
                        FILETYPE_FLAG_KINDS[declared[0] as keyof typeof FILETYPE_FLAG_KINDS],
                    )
                } else {
                    expect(kind, `${name} has no renderer flag and must fall back to text`).toBe('text')
                }
            }
        }
    })

    it('lets the backend subtype win over the extension', () => {
        // An OpenAPI spec is a `.yaml`/`.json` file; the extension alone can
        // never identify it.
        expect(filePreviewKind('api.yaml', 'openapi')).toBe('openapi')
        expect(filePreviewKind('api.json', 'openapi')).toBe('openapi')
        expect(filePreviewKind('scene.excalidraw', 'excalidraw')).toBe('excalidraw')
        expect(filePreviewKind('flow.drawio', 'drawio')).toBe('drawio')
        // A subtype for a kind the extension already decides is harmless.
        expect(filePreviewKind('readme.md', 'openapi')).toBe('openapi')
    })
})
