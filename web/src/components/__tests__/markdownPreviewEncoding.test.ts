import { describe, expect, it } from 'vitest'
import { readFileSync } from 'fs'
import { resolve } from 'path'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import MarkdownPreview from '@/components/file/MarkdownPreview.vue'
import { isThumbExtension, buildThumbUrl } from '@/utils/chatRenderUtils.ts'

const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh: {}, en: {} } })

/**
 * Tests for MarkdownPreview.vue error display and image path encoding.
 * Since fixLocalImagePaths is an internal function inside <script setup>,
 * we test it indirectly through the component's rendered output.
 * For pure logic testing, we extract the function logic and test it directly.
 */

// ── Test the fixLocalImagePaths logic (extracted for testability) ──

/**
 * Replicate the fixLocalImagePaths logic from MarkdownPreview.vue
 * to test the encoding behavior independently.
 */
function fixLocalImagePaths(html: string, currentDir: string, imageTimestamp: number, thumbWidth = 800): string {
    return html.replace(/<img\s+([^>]*src=[^>]*)>/gi, (match, attrs) => {
        const srcMatch = attrs.match(/src="([^"]*)"/)
        if (!srcMatch) return match
        const src = srcMatch[1]
        if (/^(https?:|\/\/|^\/)/i.test(src)) return match
        // Decode percent-encoded src first (marked may encode Chinese chars),
        // then re-encode each segment properly for the URL
        let resolved = currentDir ? currentDir + '/' + src : src
        try {
            resolved = decodeURIComponent(resolved)
        } catch { /* malformed encoding, use as-is */ }
        const parts = resolved.split(/[/\\]/)
        const normalized = []
        for (const part of parts) {
            if (part === '.' || part === '') continue
            if (part === '..') { normalized.pop(); continue }
            normalized.push(encodeURIComponent(part))
        }
        const rel = normalized.join('/')
        const fullSrc = `/api/fs/raw/${rel}?t=${imageTimestamp}`
        // Raster formats the thumb endpoint can decode → inline thumbnail + full for lightbox.
        const thumbSrc = isThumbExtension(src) ? buildThumbUrl(rel, thumbWidth) : null
        const replacement = thumbSrc
            ? `src="${thumbSrc}" data-full-src="${fullSrc}"`
            : `src="${fullSrc}"`
        return match.replace(`src="${src}"`, replacement)
    })
}

describe('fixLocalImagePaths — Chinese path encoding', () => {
    const timestamp = 1234567890

    it('encodes Chinese characters in image path segments', () => {
        const html = '<img src="中文/图片.png">'
        const result = fixLocalImagePaths(html, 'docs', timestamp)
        expect(result).toContain('/api/fs/raw/docs/%E4%B8%AD%E6%96%87/%E5%9B%BE%E7%89%87.png')
        expect(result).toContain(`t=${timestamp}`)
    })

    it('encodes Chinese filename but keeps extension readable', () => {
        const html = '<img src="截图.jpg">'
        const result = fixLocalImagePaths(html, '', timestamp)
        expect(result).toContain('/api/fs/raw/%E6%88%AA%E5%9B%BE.jpg')
    })

    it('handles mixed ASCII and Chinese path segments', () => {
        const html = '<img src="assets/图片/logo.png">'
        const result = fixLocalImagePaths(html, '', timestamp)
        expect(result).toContain('/api/fs/raw/assets/%E5%9B%BE%E7%89%87/logo.png')
    })

    it('does not modify absolute URLs (http://)', () => {
        const html = '<img src="http://example.com/image.png">'
        const result = fixLocalImagePaths(html, 'docs', timestamp)
        expect(result).toBe(html)
    })

    it('does not modify absolute URLs (https://)', () => {
        const html = '<img src="https://example.com/image.png">'
        const result = fixLocalImagePaths(html, 'docs', timestamp)
        expect(result).toBe(html)
    })

    it('does not modify protocol-relative URLs', () => {
        const html = '<img src="//cdn.example.com/image.png">'
        const result = fixLocalImagePaths(html, 'docs', timestamp)
        expect(result).toBe(html)
    })

    it('does not modify root-relative URLs', () => {
        const html = '<img src="/static/image.png">'
        const result = fixLocalImagePaths(html, 'docs', timestamp)
        expect(result).toBe(html)
    })

    it('handles relative path with ../ segments', () => {
        const html = '<img src="../images/图片.png">'
        const result = fixLocalImagePaths(html, 'docs/sub', timestamp)
        // docs/sub + ../images/图片.png → docs/images/图片.png
        expect(result).toContain('/api/fs/raw/docs/images/%E5%9B%BE%E7%89%87.png')
    })

    it('handles relative path with ./ segments', () => {
        const html = '<img src="./图片.png">'
        const result = fixLocalImagePaths(html, 'docs', timestamp)
        expect(result).toContain('/api/fs/raw/docs/%E5%9B%BE%E7%89%87.png')
    })

    it('encodes special characters in path segments', () => {
        const html = '<img src="path with spaces/image.png">'
        const result = fixLocalImagePaths(html, '', timestamp)
        expect(result).toContain('/api/fs/raw/path%20with%20spaces/image.png')
    })

    it('preserves ASCII paths without modification', () => {
        const html = '<img src="assets/logo.png">'
        const result = fixLocalImagePaths(html, 'docs', timestamp)
        expect(result).toContain('/api/fs/raw/docs/assets/logo.png')
    })

    it('handles multiple images in one HTML string', () => {
        const html = '<img src="中文/a.png"><img src="english/b.png">'
        const result = fixLocalImagePaths(html, 'docs', timestamp)
        expect(result).toContain('/api/fs/raw/docs/%E4%B8%AD%E6%96%87/a.png')
        expect(result).toContain('/api/fs/raw/docs/english/b.png')
    })

    it('does not double-encode when src is already percent-encoded (marked output)', () => {
        // marked may output <img src="%E4%B8%AD%E6%96%87/%E5%9B%BE%E7%89%87.png">
        // We must decode first, then re-encode to avoid %25 double-encoding
        const html = '<img src="%E4%B8%AD%E6%96%87/%E5%9B%BE%E7%89%87.png">'
        const result = fixLocalImagePaths(html, 'docs', timestamp)
        expect(result).toContain('/api/fs/raw/docs/%E4%B8%AD%E6%96%87/%E5%9B%BE%E7%89%87.png')
        // Must NOT contain double-encoded %25
        expect(result).not.toContain('%25')
    })

    it('handles already-percent-encoded src with mixed segments', () => {
        const html = '<img src="assets/%E5%B7%A5%E5%85%B7/logo.png">'
        const result = fixLocalImagePaths(html, '', timestamp)
        expect(result).toContain('/api/fs/raw/assets/%E5%B7%A5%E5%85%B7/logo.png')
        expect(result).not.toContain('%25')
    })
})

describe('fixLocalImagePaths — thumbnail compression for raster images', () => {
    const timestamp = 1234567890

    it('uses /api/fs/thumb for .png and keeps full src as data-full-src', () => {
        const result = fixLocalImagePaths('<img src="assets/logo.png">', 'docs', timestamp)
        // Inline src is the compressed thumbnail (stable URL, no ?t= so ETag revalidation works)
        expect(result).toContain('src="/api/fs/thumb?target=docs/assets/logo.png&w=800"')
        // Original full-size kept for the lightbox, with the cache-buster timestamp
        expect(result).toContain(`data-full-src="/api/fs/raw/docs/assets/logo.png?t=${timestamp}"`)
    })

    it('uses /api/fs/thumb for .jpg', () => {
        const result = fixLocalImagePaths('<img src="photo.jpg">', '', timestamp)
        expect(result).toContain('src="/api/fs/thumb?target=photo.jpg&w=800"')
        expect(result).toContain(`data-full-src="/api/fs/raw/photo.jpg?t=${timestamp}"`)
    })

    it('does not add a ?t= cache-buster to the thumbnail src (stays revalidatable)', () => {
        const result = fixLocalImagePaths('<img src="photo.png">', 'docs', timestamp)
        const thumbSrc = result.match(/src="\/api\/fs\/thumb[^"]*"/)?.[0] || ''
        expect(thumbSrc).toContain('w=800')
        // `target=` contains the substring `t=`; assert on the cache-buster form.
        expect(thumbSrc).not.toContain('&t=')
        expect(thumbSrc).not.toContain('?t=')
    })

    it('keeps full-size /api/fs/raw/ src for SVG (no thumb support)', () => {
        const result = fixLocalImagePaths('<img src="logo.svg">', 'docs', timestamp)
        expect(result).toContain(`src="/api/fs/raw/docs/logo.svg?t=${timestamp}"`)
        expect(result).not.toContain('/api/fs/thumb')
        expect(result).not.toContain('data-full-src')
    })

    it('keeps full-size /api/fs/raw/ src for GIF (preserve animation)', () => {
        const result = fixLocalImagePaths('<img src="anim.gif">', 'docs', timestamp)
        expect(result).toContain(`src="/api/fs/raw/docs/anim.gif?t=${timestamp}"`)
        expect(result).not.toContain('/api/fs/thumb')
    })

    it('keeps full-size /api/fs/raw/ src for webp (no thumb support)', () => {
        const result = fixLocalImagePaths('<img src="photo.webp">', 'docs', timestamp)
        expect(result).toContain(`src="/api/fs/raw/docs/photo.webp?t=${timestamp}"`)
        expect(result).not.toContain('/api/fs/thumb')
    })

    it('handles uppercase .PNG extension for thumbnail', () => {
        const result = fixLocalImagePaths('<img src="logo.PNG">', '', timestamp)
        expect(result).toContain('src="/api/fs/thumb?target=logo.PNG&w=800"')
        expect(result).toContain('data-full-src')
    })

    it('uses mobile thumbnail width when device is not PC', () => {
        const result = fixLocalImagePaths('<img src="assets/logo.png">', 'docs', timestamp, 480)
        expect(result).toContain('src="/api/fs/thumb?target=docs/assets/logo.png&w=480"')
        expect(result).toContain(`data-full-src="/api/fs/raw/docs/assets/logo.png?t=${timestamp}"`)
    })
})

// ── FileViewer error bubble template test ──

describe('FileViewer error display', () => {
    it('uses error-bubble class instead of error-message banner', () => {
        const componentPath = resolve(__dirname, '../file/FileViewer.vue')
        const source = readFileSync(componentPath, 'utf-8')

        // Should use the new bubble style
        expect(source).toContain('error-bubble')
        expect(source).not.toContain('class="error-message"')
    })

    it('error-bubble has compact pill/bubble styling', () => {
        const componentPath = resolve(__dirname, '../file/FileViewer.vue')
        const source = readFileSync(componentPath, 'utf-8')

        // Should have pill radius for the bubble shape
        expect(source).toMatch(/border-radius:\s*20px/)
        // Should have small padding (not 16px banner); values now come from
        // the spacing scale, so assert the tokens (6px / 12px)
        expect(source).toMatch(/padding:\s*var\(--space-3\)\s+var\(--space-6\)/)
    })

    it('error-bubble includes warning icon SVG', () => {
        const componentPath = resolve(__dirname, '../file/FileViewer.vue')
        const source = readFileSync(componentPath, 'utf-8')

        // The error-bubble div should contain an SVG icon
        expect(source).toMatch(/error-bubble[^>]*>[\s\S]*?<svg/)
    })
})

// ── MarkdownPreview component-level test (covers fixLocalImagePaths in actual component) ──

describe('MarkdownPreview — Chinese image path encoding (component level)', () => {
    it('encodes Chinese image path segments via fixLocalImagePaths in rendered output', async () => {
        // Mount the actual MarkdownPreview component with markdown containing a Chinese image path
        const wrapper = mount(MarkdownPreview, {
            props: {
                file: {
                    path: 'docs/README.md',
                    content: '![图片](中文/截图.png)',
                },
                viewMode: 'rendered',
            },
            global: {
                plugins: [i18n],
            },
        })

        // Wait for async rendering (doRender is triggered by content watcher)
        await nextTick()
        await nextTick()
        await nextTick()

        // The rendered markdown should contain the encoded image URL
        const html = wrapper.html()
        // Chinese chars should be percent-encoded in the /api/fs/raw/ URL
        expect(html).toContain('/api/fs/raw/')
        expect(html).toContain('%E4%B8%AD%E6%96%87')
    })

    it('encodes mixed ASCII and Chinese image path in rendered output', async () => {
        const wrapper = mount(MarkdownPreview, {
            props: {
                file: {
                    path: 'docs/README.md',
                    content: '![logo](assets/图片/logo.png)',
                },
                viewMode: 'rendered',
            },
            global: {
                plugins: [i18n],
            },
        })

        await nextTick()
        await nextTick()
        await nextTick()

        const html = wrapper.html()
        expect(html).toContain('/api/fs/raw/')
        // "图片" should be encoded, "assets" and "logo.png" kept as-is
        expect(html).toContain('assets/%E5%9B%BE%E7%89%87/logo.png')
    })

    it('does not encode external image URLs', async () => {
        const wrapper = mount(MarkdownPreview, {
            props: {
                file: {
                    path: 'docs/README.md',
                    content: '![external](https://example.com/图片.png)',
                },
                viewMode: 'rendered',
            },
            global: {
                plugins: [i18n],
            },
        })

        await nextTick()
        await nextTick()
        await nextTick()

        const html = wrapper.html()
        // External URLs should not be rewritten to /api/fs/raw/
        expect(html).not.toContain('/api/fs/raw/')
        expect(html).toContain('https://example.com')
    })
})

// ── SVG media: proportional fill-width sizing re-stamped on media load ──
//
// This is the full-document host (MarkdownPreview.vue) rather than the
// slice host (MarkdownPreviewBody.vue). Both mount the same pipeline, but each
// owns its own `@load.capture` handler; the SVG-file ratio is only resolvable
// after decode, so this wiring is the sole route that sizes SVG *files*.

describe('MarkdownPreview — svg file sizing on media load', () => {
    /** jsdom never loads images, so the decoded intrinsic size must be injected. */
    function injectNaturalSize(img: Element, w: number, h: number): void {
        Object.defineProperty(img, 'naturalWidth', { value: w, configurable: true })
        Object.defineProperty(img, 'naturalHeight', { value: h, configurable: true })
    }

    async function mountWithSvgFile() {
        const wrapper = mount(MarkdownPreview, {
            props: {
                file: { path: 'docs/README.md', content: '![chart](chart.svg)' },
                viewMode: 'rendered',
            },
            global: { plugins: [i18n] },
        })
        await nextTick()
        await nextTick()
        await nextTick()
        return wrapper
    }

    it('stamps the figure with its ratio once the SVG image has loaded', async () => {
        const wrapper = await mountWithSvgFile()
        const figure = wrapper.find('.image-block-wrapper')
        expect(figure.exists()).toBe(true)
        // Not yet decoded → no ratio, so the figure keeps its previous sizing.
        expect(figure.classes()).not.toContain('svg-fit')

        const img = wrapper.find('img.lightbox-img')
        injectNaturalSize(img.element, 400, 100)
        await img.trigger('load')

        expect(figure.classes()).toContain('svg-fit')
        expect((figure.element as HTMLElement).style.getPropertyValue('--svg-ar')).toBe('4')
    })

    it('leaves a raster image figure unsized on load', async () => {
        const wrapper = mount(MarkdownPreview, {
            props: {
                file: { path: 'docs/README.md', content: '![photo](photo.png)' },
                viewMode: 'rendered',
            },
            global: { plugins: [i18n] },
        })
        await nextTick()
        await nextTick()
        await nextTick()

        const img = wrapper.find('img.lightbox-img')
        injectNaturalSize(img.element, 400, 100)
        await img.trigger('load')

        expect(wrapper.find('.image-block-wrapper').classes()).not.toContain('svg-fit')
    })
})
