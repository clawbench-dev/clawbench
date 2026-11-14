import { describe, expect, it } from 'vitest'
import { htmlRenderMode } from '@/components/file/milkdownHtmlBlock.ts'

/**
 * Only complete elements are rendered in the editor. Inline HTML reaches the
 * document as separate atoms (`<b>` and `</b>`), so rendering a lone tag would
 * paint nothing — those must keep showing their source.
 */
describe('htmlRenderMode', () => {
    it('treats a complete block element as a block', () => {
        expect(htmlRenderMode('<p align="center">\n  <img src="a.png">\n</p>')).toBe('block')
        expect(htmlRenderMode('<div class="note">hello</div>')).toBe('block')
        expect(htmlRenderMode('<div class="x" />')).toBe('block')
    })

    it('renders a lone void element in the text flow', () => {
        expect(htmlRenderMode('<img src="a.png" width="960">')).toBe('inline')
        expect(htmlRenderMode('<br>')).toBe('inline')
    })

    it('keeps unpaired inline tags as source text', () => {
        expect(htmlRenderMode('<b>')).toBeNull()
        expect(htmlRenderMode('</b>')).toBeNull()
        expect(htmlRenderMode('<span style="color:red">')).toBeNull()
        expect(htmlRenderMode('<!-- a comment -->')).toBeNull()
        expect(htmlRenderMode('')).toBeNull()
    })
})
