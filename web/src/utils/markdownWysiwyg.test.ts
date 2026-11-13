import { describe, expect, it } from 'vitest'
import { wysiwygBlockReason } from './markdownWysiwyg'

describe('wysiwygBlockReason', () => {
    it('blocks a file that opens with YAML frontmatter', () => {
        expect(wysiwygBlockReason('---\nname: demo\ndescription: x\n---\n\n# Title\n')).toBe('frontmatter')
    })

    it('blocks frontmatter with CRLF line endings', () => {
        expect(wysiwygBlockReason('---\r\nname: demo\r\n---\r\nbody')).toBe('frontmatter')
    })

    it('ignores a leading BOM', () => {
        expect(wysiwygBlockReason('\uFEFF---\nname: demo\n---\n')).toBe('frontmatter')
    })

    it('allows a thematic break later in the document', () => {
        expect(wysiwygBlockReason('# Title\n\n---\n\nbody')).toBeNull()
    })

    it('allows a longer dash run (not frontmatter)', () => {
        expect(wysiwygBlockReason('----\nbody')).toBeNull()
    })

    it('allows ordinary markdown, empty and missing content', () => {
        expect(wysiwygBlockReason('# Title\n\ntext')).toBeNull()
        expect(wysiwygBlockReason('')).toBeNull()
        expect(wysiwygBlockReason(null)).toBeNull()
        expect(wysiwygBlockReason(undefined)).toBeNull()
    })
})
