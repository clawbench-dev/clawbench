import { describe, expect, it } from 'vitest'
import {
    IMAGE_EMBED_EXTENSIONS,
    isImageEmbedPath,
    stripEmbedDecoration,
    downgradeNonImageEmbedsIn,
} from '@/utils/nonImageEmbed.ts'

/**
 * The AI points `![alt](src)` at things a browser cannot decode as an image —
 * most often a rendered `.html` page it just wrote. That is not a rendering
 * bug (the markdown is well-formed), it is a wrong-media-type embed, and the
 * user sees the generic "failed to load" placeholder instead of the page.
 * These tests pin both halves of the repair: classification + downgrade.
 */

// ─── isImageEmbedPath ────────────────────────────────────────────────────────

describe('isImageEmbedPath', () => {
    it('accepts every decodable extension, case-insensitively', () => {
        for (const ext of IMAGE_EMBED_EXTENSIONS) {
            expect(isImageEmbedPath(`a${ext}`), ext).toBe(true)
            expect(isImageEmbedPath(`a${ext.toUpperCase()}`), ext).toBe(true)
            expect(isImageEmbedPath(`dir/sub/b${ext}`), ext).toBe(true)
        }
    })

    it('rejects a rendered page or document used as an image', () => {
        // The exact shape that produced the placeholder (chat message 65399).
        expect(isImageEmbedPath('/home/u/proj/docs/screenshots/_hero_layouts.html')).toBe(false)
        expect(isImageEmbedPath('report.pdf')).toBe(false)
        expect(isImageEmbedPath('notes.md')).toBe(false)
    })

    it('ignores query and fragment when reading the extension', () => {
        expect(isImageEmbedPath('a.png?t=123')).toBe(true)
        expect(isImageEmbedPath('a.html?t=123')).toBe(false)
        expect(isImageEmbedPath('a.html#top')).toBe(false)
    })

    it('treats an extension-less src as an image (no signal to act on)', () => {
        expect(isImageEmbedPath('/api/fs/raw/photos/cover')).toBe(true)
        expect(isImageEmbedPath('https://example.com/image')).toBe(true)
    })

    it('reads the real filename out of a served URL, not the endpoint', () => {
        // After rewriteImageUrls every src is /api/fs/… — the endpoint has no
        // extension, so the target param is the only place the name survives.
        expect(isImageEmbedPath('/api/fs/raw/?target=%2Ftmp%2Fa.html&t=1')).toBe(false)
        expect(isImageEmbedPath('/api/fs/raw/?target=%2Ftmp%2Fa.png&t=1')).toBe(true)
        expect(isImageEmbedPath('/api/fs/raw/a.png?t=1')).toBe(true)
        expect(isImageEmbedPath('/api/fs/raw/a.html?t=1')).toBe(false)
        // The thumb endpoint only ever serves decodable rasters.
        expect(isImageEmbedPath('/api/fs/thumb?target=a.html&w=1200')).toBe(true)
        // Share mode hides the name in ?path=.
        expect(isImageEmbedPath('/api/share/tok/local?path=%2Ftmp%2Fa.html&t=1')).toBe(false)
    })
})

// ─── stripEmbedDecoration ────────────────────────────────────────────────────

describe('stripEmbedDecoration', () => {
    it('strips a matching backtick / quote pair, literal or percent-encoded', () => {
        expect(stripEmbedDecoration('`.clawbench/a.png`')).toBe('.clawbench/a.png')
        // marked percent-encodes the backticks in the emitted src.
        expect(stripEmbedDecoration('%60.clawbench/a.png%60')).toBe('.clawbench/a.png')
        expect(stripEmbedDecoration('".clawbench/a.png"')).toBe('.clawbench/a.png')
        expect(stripEmbedDecoration("'a.png'")).toBe('a.png')
    })

    it('leaves an unmatched decoration character alone', () => {
        // A lone backtick is a legal (if unusual) filename character.
        expect(stripEmbedDecoration('`a.png')).toBe('`a.png')
        expect(stripEmbedDecoration('a.png`')).toBe('a.png`')
        expect(stripEmbedDecoration('`')).toBe('`')
    })
})

// ─── downgradeNonImageEmbedsIn ───────────────────────────────────────────────

describe('downgradeNonImageEmbedsIn', () => {
    function run(html: string): Document {
        const doc = new DOMParser().parseFromString(html, 'text/html')
        downgradeNonImageEmbedsIn(doc)
        return doc
    }

    it('turns an image embed pointing at an .html page into a link', () => {
        const doc = run('<p><img src="/proj/docs/_hero_layouts.html" alt="设备编排方案"></p>')
        expect(doc.querySelector('img')).toBeNull()
        const a = doc.querySelector('a')!
        expect(a.getAttribute('href')).toBe('/proj/docs/_hero_layouts.html')
        // The alt text is the label — it is what the user was meant to read.
        expect(a.textContent).toBe('设备编排方案')
    })

    it('falls back to the file name when the embed has no alt text', () => {
        const doc = run('<p><img src="/proj/docs/_hero_layouts.html" alt=""></p>')
        expect(doc.querySelector('a')!.textContent).toBe('_hero_layouts.html')
    })

    it('leaves a real image embed untouched', () => {
        const doc = run('<p><img src="/proj/a.png" alt="x"></p>')
        expect(doc.querySelector('img')!.getAttribute('src')).toBe('/proj/a.png')
        expect(doc.querySelector('a')).toBeNull()
    })

    it('fixes a backtick-wrapped image src instead of downgrading it', () => {
        // The other failure mode: decoration around a REAL image. The extension
        // check would read ".png%60" and wrongly classify it as foreign.
        const doc = run('<p><img src="%60.clawbench/a.png%60" alt="x"></p>')
        expect(doc.querySelector('img')!.getAttribute('src')).toBe('.clawbench/a.png')
        expect(doc.querySelector('a')).toBeNull()
    })

    it('replaces the image with text when the embed is already inside a link', () => {
        // An <a> cannot nest inside an <a>; the outer link must survive intact.
        const doc = run('<p><a href="/proj/x.html"><img src="/proj/x.html" alt="说明"></a></p>')
        expect(doc.querySelectorAll('a')).toHaveLength(1)
        expect(doc.querySelector('a')!.getAttribute('href')).toBe('/proj/x.html')
        expect(doc.querySelector('a')!.textContent).toBe('说明')
        expect(doc.querySelector('img')).toBeNull()
    })

    it('leaves data: URIs alone (inline bytes have no filename to judge)', () => {
        const doc = run('<p><img src="data:image/png;base64,iVBORw0KGgo=" alt="x"></p>')
        expect(doc.querySelector('img')).not.toBeNull()
    })

    it('downgrades a remote .html embed too (equally undecodable)', () => {
        const doc = run('<p><img src="https://example.com/page.html" alt="x"></p>')
        expect(doc.querySelector('a')!.getAttribute('href')).toBe('https://example.com/page.html')
    })

    it('uses the real file path as the href when the src is a serving URL', () => {
        // File-preview pipeline: fixImagePaths has already rewritten the src to
        // /api/fs/raw/?target=…. Handing that URL to the file-path annotator
        // makes it parse the endpoint ("glob pattern, not a file path") and the
        // link goes dead — the href must be the target path instead.
        const doc = run('<p><img src="/api/fs/raw/?target=%2Fproj%2Fdocs%2Fa.html&amp;t=1" alt="页面"></p>')
        const a = doc.querySelector('a')!
        expect(a.getAttribute('href')).toBe('/proj/docs/a.html')
        expect(a.textContent).toBe('页面')
    })

    it('drops the empty media-figure shell when the image was already lifted', () => {
        // Ordering in the file-preview pipeline is rewrite → wrap → this step, so
        // the <img> can already sit inside a figure with a header of buttons.
        const doc = run('<div class="image-block-wrapper"><div class="image-block-header">'
            + '<button class="image-block-view-btn">v</button></div>'
            + '<span class="lightbox-img-wrap"><img src="/proj/a.html" alt="页"></span></div>')
        expect(doc.querySelector('.image-block-wrapper')).toBeNull()
        expect(doc.querySelector('.image-block-view-btn')).toBeNull()
        expect(doc.querySelector('a')!.textContent).toBe('页')
    })

    it('does not treat the replacement label as markup', () => {
        const doc = run('<p><img src="/proj/a.html" alt="<img src=x onerror=alert(1)>"></p>')
        expect(doc.querySelectorAll('img')).toHaveLength(0)
        expect(doc.querySelector('a')!.textContent).toBe('<img src=x onerror=alert(1)>')
    })
})
