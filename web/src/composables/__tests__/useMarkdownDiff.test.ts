import { describe, expect, it, vi } from 'vitest'
import { extractBlocks, computeMarkdownDiff, computeCharDiff, offscreenExtractBlocks, charDiffToLines, contentToDiffLines, contentToDiffLinesWithCtx, isDiffBlock, extractBlockElements, computeCodeDiffMarkers } from '@/composables/useMarkdownDiff'

// Mock globals for renderMarkdown
vi.mock('@/utils/globals', () => ({
  marked: {
    parse: (s: string) => {
      // Minimal markdown→HTML for testing
      return s
        .replace(/^### (.+)$/gm, '<h3>$1</h3>')
        .replace(/^## (.+)$/gm, '<h2>$1</h2>')
        .replace(/^# (.+)$/gm, '<h1>$1</h1>')
        .replace(/^> (.+)$/gm, '<blockquote><p>$1</p></blockquote>')
        .replace(/^- (.+)$/gm, '<li>$1</li>')
        .replace(/```mermaid\n([\s\S]*?)```/g, '<pre class="mermaid">$1</pre>')
        .replace(/```\n?([\s\S]*?)```/g, '<pre><code>$1</code></pre>')
        .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
        .replace(/\*(.+?)\*/g, '<em>$1</em>')
        .replace(/^(?!<[hupobl])/gm, (m) => m.trim() ? `<p>${m}</p>` : m)
    },
  },
  katex: { renderToString: vi.fn() },
  mermaid: { render: vi.fn() },
  DOMPurify: { sanitize: (html: string) => html },
}))

vi.mock('@/utils/html', () => ({
  escapeHtml: (s: string) => s,
}))

// Helper: create a DOM element from HTML string
function htmlToElement(html: string): Element {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  return doc.body
}

describe('extractBlocks', () => {
  it('extracts top-level block elements', () => {
    const el = htmlToElement('<h1>Title</h1><p>Paragraph 1</p><p>Paragraph 2</p>')
    const blocks = extractBlocks(el)
    expect(blocks).toHaveLength(3)
    expect(blocks[0].tag).toBe('H1')
    expect(blocks[1].tag).toBe('P')
    expect(blocks[2].tag).toBe('P')
  })

  it('recurses into LI elements', () => {
    const el = htmlToElement('<ul><li>Item 1</li><li>Item 2</li></ul>')
    const blocks = extractBlocks(el)
    // UL is not a block tag, so it recurses inside → finds LI elements
    expect(blocks).toHaveLength(2)
    expect(blocks[0].tag).toBe('LI')
    expect(blocks[0].textContent).toBe('Item 1')
    expect(blocks[1].tag).toBe('LI')
  })

  it('recurses into BLOCKQUOTE elements', () => {
    const el = htmlToElement('<blockquote><p>Quoted</p></blockquote>')
    const blocks = extractBlocks(el)
    // BLOCKQUOTE is a block, then recurse finds P inside
    expect(blocks).toHaveLength(2)
    expect(blocks[0].tag).toBe('BLOCKQUOTE')
    expect(blocks[1].tag).toBe('P')
  })

  it('detects div.table-wrap as a block', () => {
    const el = htmlToElement('<div class="table-wrap"><table><tr><td>cell</td></tr></table></div>')
    const blocks = extractBlocks(el)
    expect(blocks).toHaveLength(1)
    expect(blocks[0].tag).toBe('DIV')
  })

  it('detects div.mermaid as a block', () => {
    const el = htmlToElement('<div class="mermaid" data-mermaid="graph TD">svg content</div>')
    const blocks = extractBlocks(el)
    expect(blocks).toHaveLength(1)
    expect(blocks[0].tag).toBe('DIV')
    expect(blocks[0].mermaidSource).toBe('graph TD')
  })

  it('uses textContent for PRE blocks', () => {
    const el = htmlToElement('<pre><code class="hljs">const x = 1;</code></pre>')
    const blocks = extractBlocks(el)
    expect(blocks).toHaveLength(1)
    expect(blocks[0].tag).toBe('PRE')
    // innerHTML should be textContent (not the hljs markup)
    expect(blocks[0].innerHTML).toBe('const x = 1;')
  })

  it('does NOT treat generic DIVs as blocks', () => {
    const el = htmlToElement('<div><p>Inside div</p></div>')
    const blocks = extractBlocks(el)
    // Generic DIV is not a block, recurse to find P
    expect(blocks).toHaveLength(1)
    expect(blocks[0].tag).toBe('P')
  })

  it('skips non-block children and recurses', () => {
    const el = htmlToElement('<section><h1>Title</h1><span>inline</span><p>Text</p></section>')
    const blocks = extractBlocks(el)
    expect(blocks).toHaveLength(2)
    expect(blocks[0].tag).toBe('H1')
    expect(blocks[1].tag).toBe('P')
  })

  it('detects katex-display as a block', () => {
    const el = htmlToElement('<div class="katex-display">formula</div>')
    const blocks = extractBlocks(el)
    expect(blocks).toHaveLength(1)
    expect(blocks[0].tag).toBe('DIV')
  })
})

describe('computeMarkdownDiff', () => {
  it('detects added blocks', () => {
    const oldBlocks = [
      { tag: 'H1', textContent: 'Title', innerHTML: 'Title', selector: ':scope' },
    ]
    const newBlocks = [
      { tag: 'H1', textContent: 'Title', innerHTML: 'Title', selector: ':scope' },
      { tag: 'P', textContent: 'New paragraph', innerHTML: 'New paragraph', selector: ':scope' },
    ]
    const result = computeMarkdownDiff(oldBlocks, newBlocks)
    expect(result.hasChanges).toBe(true)
    expect(result.markers).toHaveLength(1)
    expect(result.markers[0].type).toBe('added')
    expect(result.markers[0].label).toBe('+')
  })

  it('detects deleted blocks', () => {
    const oldBlocks = [
      { tag: 'H1', textContent: 'Title', innerHTML: 'Title', selector: ':scope' },
      { tag: 'P', textContent: 'Paragraph', innerHTML: 'Paragraph', selector: ':scope' },
    ]
    const newBlocks = [
      { tag: 'H1', textContent: 'Title', innerHTML: 'Title', selector: ':scope' },
    ]
    const result = computeMarkdownDiff(oldBlocks, newBlocks)
    expect(result.hasChanges).toBe(true)
    expect(result.markers).toHaveLength(1)
    expect(result.markers[0].type).toBe('deleted')
  })

  it('merges multiple consecutive deleted blocks into one marker', () => {
    const oldBlocks = [
      { tag: 'H1', textContent: 'Title', innerHTML: 'Title', selector: ':scope' },
      { tag: 'P', textContent: 'Para 1', innerHTML: 'Para 1', selector: ':scope' },
      { tag: 'P', textContent: 'Para 2', innerHTML: 'Para 2', selector: ':scope' },
      { tag: 'P', textContent: 'Para 3', innerHTML: 'Para 3', selector: ':scope' },
    ]
    const newBlocks = [
      { tag: 'H1', textContent: 'Title', innerHTML: 'Title', selector: ':scope' },
    ]
    const result = computeMarkdownDiff(oldBlocks, newBlocks)
    expect(result.hasChanges).toBe(true)
    // Should produce ONE merged deleted marker, not three
    expect(result.markers).toHaveLength(1)
    expect(result.markers[0].type).toBe('deleted')
    // The merged marker should contain all three paragraphs in diffLines
    const marker = result.markers[0]
    expect(marker.diffLines).toBeTruthy()
    const delLines = marker.diffLines!.filter(l => l.type === 'del')
    expect(delLines.length).toBe(3)
    expect(delLines[0].content).toBe('Para 1')
    expect(delLines[1].content).toBe('Para 2')
    expect(delLines[2].content).toBe('Para 3')
  })

  it('merges extra deleted blocks in modified pair into one marker', () => {
    // 3 old blocks replaced by 1 new block → 1 modified + 2 merged deleted
    const oldBlocks = [
      { tag: 'P', textContent: 'Line 1', innerHTML: 'Line 1', selector: ':scope' },
      { tag: 'P', textContent: 'Line 2', innerHTML: 'Line 2', selector: ':scope' },
      { tag: 'P', textContent: 'Line 3', innerHTML: 'Line 3', selector: ':scope' },
    ]
    const newBlocks = [
      { tag: 'P', textContent: 'Line 1 changed', innerHTML: 'Line 1 changed', selector: ':scope' },
    ]
    const result = computeMarkdownDiff(oldBlocks, newBlocks)
    expect(result.hasChanges).toBe(true)
    const modified = result.markers.filter(m => m.type === 'modified')
    const deleted = result.markers.filter(m => m.type === 'deleted')
    expect(modified).toHaveLength(1)
    expect(deleted).toHaveLength(1)
    // The merged deleted marker should contain Line 2 and Line 3
    const delMarker = deleted[0]
    expect(delMarker.diffLines).toBeTruthy()
    const delLines = delMarker.diffLines!.filter(l => l.type === 'del')
    expect(delLines.length).toBe(2)
    expect(delLines[0].content).toBe('Line 2')
    expect(delLines[1].content).toBe('Line 3')
  })

  it('gives unique IDs to deleted markers from different positions', () => {
    const oldBlocks = [
      { tag: 'H1', textContent: 'Title', innerHTML: 'Title', selector: ':scope' },
      { tag: 'P', textContent: 'Para A', innerHTML: 'Para A', selector: ':scope' },
      { tag: 'P', textContent: 'Para B', innerHTML: 'Para B', selector: ':scope' },
    ]
    const newBlocks = [
      { tag: 'H1', textContent: 'Title', innerHTML: 'Title', selector: ':scope' },
    ]
    const result = computeMarkdownDiff(oldBlocks, newBlocks)
    // One merged marker, but its ID should be unique (contain old index)
    expect(result.markers).toHaveLength(1)
    expect(result.markers[0].id).toContain('old')
  })

  it('detects modified blocks', () => {
    const oldBlocks = [
      { tag: 'P', textContent: 'Hello world', innerHTML: 'Hello world', selector: ':scope' },
    ]
    const newBlocks = [
      { tag: 'P', textContent: 'Hello universe', innerHTML: 'Hello universe', selector: ':scope' },
    ]
    const result = computeMarkdownDiff(oldBlocks, newBlocks)
    expect(result.hasChanges).toBe(true)
    expect(result.markers).toHaveLength(1)
    expect(result.markers[0].type).toBe('modified')
    expect(result.markers[0].charDiff).toBeTruthy()
  })

  it('returns no changes for identical blocks', () => {
    const blocks = [
      { tag: 'H1', textContent: 'Title', innerHTML: 'Title', selector: ':scope' },
    ]
    const result = computeMarkdownDiff(blocks, blocks)
    expect(result.hasChanges).toBe(false)
    expect(result.markers).toHaveLength(0)
  })

  it('handles empty old blocks (all added)', () => {
    const newBlocks = [
      { tag: 'P', textContent: 'New', innerHTML: 'New', selector: ':scope' },
    ]
    const result = computeMarkdownDiff([], newBlocks)
    expect(result.hasChanges).toBe(true)
    expect(result.markers).toHaveLength(1)
    expect(result.markers[0].type).toBe('added')
  })

  it('handles empty new blocks (all deleted)', () => {
    const oldBlocks = [
      { tag: 'P', textContent: 'Old', innerHTML: 'Old', selector: ':scope' },
    ]
    const result = computeMarkdownDiff(oldBlocks, [])
    expect(result.hasChanges).toBe(true)
    expect(result.markers).toHaveLength(1)
    expect(result.markers[0].type).toBe('deleted')
  })

  it('skips marker when innerHTML changes but textContent is identical', () => {
    // Formatting-only changes (e.g. <strong> removed) produce no visible
    // content change, so no marker should appear.
    const oldBlocks = [
      { tag: 'P', textContent: 'bold text', innerHTML: '<strong>bold</strong> text', selector: ':scope' },
    ]
    const newBlocks = [
      { tag: 'P', textContent: 'bold text', innerHTML: 'bold text', selector: ':scope' },
    ]
    const result = computeMarkdownDiff(oldBlocks, newBlocks)
    expect(result.hasChanges).toBe(false)
    expect(result.markers).toHaveLength(0)
  })

  it('uses textContent for PRE block diff', () => {
    const oldBlocks = [
      { tag: 'PRE', textContent: 'const x = 1;', innerHTML: 'const x = 1;', selector: ':scope' },
    ]
    const newBlocks = [
      { tag: 'PRE', textContent: 'const x = 2;', innerHTML: 'const x = 2;', selector: ':scope' },
    ]
    const result = computeMarkdownDiff(oldBlocks, newBlocks)
    expect(result.hasChanges).toBe(true)
    expect(result.markers[0].type).toBe('modified')
  })
})

describe('computeCharDiff', () => {
  it('produces char-level diff', () => {
    const result = computeCharDiff('hello world', 'hello universe')
    expect(result.oldText).toBe('hello world')
    expect(result.newText).toBe('hello universe')
    expect(result.changes.length).toBeGreaterThan(0)
    // Verify the diff captures the change: old text and new text are different
    const fullOld = result.changes.filter(c => c.removed).map(c => c.value).join('')
    const fullNew = result.changes.filter(c => c.added).map(c => c.value).join('')
    expect(fullOld).toBeTruthy()
    expect(fullNew).toBeTruthy()
  })
})

describe('offscreenExtractBlocks', () => {
  it('simulates mermaid transformation', () => {
    const content = '# Title\n\n```mermaid\ngraph TD\n  A-->B\n```'
    const blocks = offscreenExtractBlocks(content)
    // Should find H1 and a DIV.mermaid (not PRE.mermaid)
    expect(blocks.length).toBeGreaterThanOrEqual(1)
    const mermaidBlock = blocks.find(b => b.tag === 'DIV' && b.mermaidSource !== undefined)
    expect(mermaidBlock).toBeTruthy()
    expect(mermaidBlock!.mermaidSource).toContain('graph TD')
  })
})

describe('charDiffToLines', () => {
  it('converts CharDiff to DiffLine[] with correct line numbers', () => {
    const charDiff = computeCharDiff('hello world', 'hello universe')
    const lines = charDiffToLines(charDiff)
    expect(lines.length).toBeGreaterThan(0)
    // Single-line change: should have ctx + del + add or similar
    const hasDel = lines.some(l => l.type === 'del')
    const hasAdd = lines.some(l => l.type === 'add')
    expect(hasDel || hasAdd).toBe(true)
  })

  it('handles multi-line content correctly', () => {
    const charDiff = computeCharDiff('line1\nline2\nline3', 'line1\nline2b\nline3')
    const lines = charDiffToLines(charDiff)
    // Should produce lines with proper oldLine/newLine numbering
    expect(lines.length).toBeGreaterThan(0)
    // Verify line numbers are sequential
    const ctxLines = lines.filter(l => l.type === 'ctx')
    for (const cl of ctxLines) {
      expect(cl.oldLine).not.toBeNull()
      expect(cl.newLine).not.toBeNull()
    }
  })

  it('handles pure deletion', () => {
    const charDiff = computeCharDiff('hello', '')
    const lines = charDiffToLines(charDiff)
    if (lines.length > 0) {
      expect(lines.every(l => l.type === 'del')).toBe(true)
      expect(lines[0].oldLine).toBe(1)
      expect(lines[0].newLine).toBeNull()
    }
  })

  it('handles pure addition', () => {
    const charDiff = computeCharDiff('', 'hello')
    const lines = charDiffToLines(charDiff)
    if (lines.length > 0) {
      expect(lines.every(l => l.type === 'add')).toBe(true)
      expect(lines[0].oldLine).toBeNull()
      expect(lines[0].newLine).toBe(1)
    }
  })
})

describe('contentToDiffLines', () => {
  it('converts old/new content to DiffLine[] at line level', () => {
    const lines = contentToDiffLines('a\nb\nc', 'a\nb2\nc')
    // diffArrays produces: ctx(a) + del(b) + add(b2) + ctx(c)
    expect(lines.length).toBe(4)
    // Line 1: context
    expect(lines[0].type).toBe('ctx')
    expect(lines[0].oldLine).toBe(1)
    expect(lines[0].newLine).toBe(1)
    // Line 2: deleted
    expect(lines[1].type).toBe('del')
    expect(lines[1].content).toBe('b')
    // Line 3: added
    expect(lines[2].type).toBe('add')
    expect(lines[2].content).toBe('b2')
    // Line 4: context
    expect(lines[3].type).toBe('ctx')
  })

  it('handles empty old content', () => {
    const lines = contentToDiffLines('', 'a\nb')
    expect(lines.every(l => l.type === 'add')).toBe(true)
  })

  it('handles empty new content', () => {
    const lines = contentToDiffLines('a\nb', '')
    expect(lines.every(l => l.type === 'del')).toBe(true)
  })
})

describe('contentToDiffLinesWithCtx', () => {
  const oldContent = Array.from({ length: 20 }, (_, i) => `line ${i + 1}`).join('\n')
  const newContent = oldContent.replace('line 10', 'line 10 MODIFIED')

  it('returns context lines around the changed line', () => {
    const lines = contentToDiffLinesWithCtx(oldContent, newContent, [10], 3)
    // Should have ~3 context before + del + add + ~3 context after = ~8 lines
    const nonEllipsis = lines.filter(l => l.content !== '⋯')
    expect(nonEllipsis.length).toBeLessThan(12)
    expect(nonEllipsis.length).toBeGreaterThan(2)
    // Should contain the modified line
    expect(lines.some(l => l.content === 'line 10 MODIFIED')).toBe(true)
    expect(lines.some(l => l.content === 'line 10')).toBe(true)
  })

  it('inserts ellipsis for distant context', () => {
    const lines = contentToDiffLinesWithCtx(oldContent, newContent, [10], 2)
    expect(lines.some(l => l.content === '⋯')).toBe(true)
  })

  it('does not insert ellipsis when all lines fit', () => {
    const shortOld = 'a\nb\nc'
    const shortNew = 'a\nb2\nc'
    const lines = contentToDiffLinesWithCtx(shortOld, shortNew, [2], 3)
    expect(lines.some(l => l.content === '⋯')).toBe(false)
  })

  it('returns empty for empty content', () => {
    const lines = contentToDiffLinesWithCtx('', '', [1], 3)
    expect(lines).toEqual([])
  })

  it('works with multiple center lines', () => {
    const old2 = 'a\nb\nc\nd\ne'
    const new2 = 'a\nb2\nc\nd2\ne'
    const lines = contentToDiffLinesWithCtx(old2, new2, [2, 4], 1)
    // Should show context around both lines 2 and 4
    expect(lines.some(l => l.content === 'b2')).toBe(true)
    expect(lines.some(l => l.content === 'd2')).toBe(true)
  })
})

describe('isDiffBlock', () => {
  it('recognizes standard block tags', () => {
    const el = htmlToElement('<h1>Title</h1>').firstElementChild!
    expect(isDiffBlock(el)).toBe(true)
  })

  it('recognizes P as a block tag', () => {
    const el = htmlToElement('<p>text</p>').firstElementChild!
    expect(isDiffBlock(el)).toBe(true)
  })

  it('recognizes div.table-wrap as a block', () => {
    const el = htmlToElement('<div class="table-wrap"><table></table></div>').firstElementChild!
    expect(isDiffBlock(el)).toBe(true)
  })

  it('recognizes div.mermaid as a block', () => {
    const el = htmlToElement('<div class="mermaid">svg</div>').firstElementChild!
    expect(isDiffBlock(el)).toBe(true)
  })

  it('rejects generic divs', () => {
    const el = htmlToElement('<div>content</div>').firstElementChild!
    expect(isDiffBlock(el)).toBe(false)
  })

  it('rejects inline elements like span', () => {
    const el = htmlToElement('<span>text</span>').firstElementChild!
    expect(isDiffBlock(el)).toBe(false)
  })
})

describe('extractBlockElements', () => {
  it('returns block elements with el references', () => {
    const el = htmlToElement('<h1>Title</h1><p>Para</p>')
    const blocks = extractBlockElements(el)
    expect(blocks).toHaveLength(2)
    expect(blocks[0].tag).toBe('H1')
    expect(blocks[0].el).toBeTruthy()
    expect(blocks[1].tag).toBe('P')
    expect(blocks[1].el).toBeTruthy()
  })

  it('generates CSS selectors for each block', () => {
    const el = htmlToElement('<h1>Title</h1><p>Para</p>')
    const blocks = extractBlockElements(el)
    expect(blocks[0].selector).toContain(':scope')
    expect(blocks[1].selector).toContain(':scope')
  })

  it('recurses into LI elements', () => {
    const el = htmlToElement('<ul><li>Item 1</li><li>Item 2</li></ul>')
    const blocks = extractBlockElements(el)
    expect(blocks).toHaveLength(2)
    expect(blocks[0].tag).toBe('LI')
  })

  it('includes katex-display elements', () => {
    const el = htmlToElement('<div class="katex-display">formula</div>')
    const blocks = extractBlockElements(el)
    expect(blocks).toHaveLength(1)
    expect(blocks[0].tag).toBe('DIV')
  })
})

describe('computeCodeDiffMarkers', () => {
  it('creates modified markers for changed lines', () => {
    const lineDiff: import('@/utils/diffUtils.ts').LineDiff = {
      deletedInOld: [],
      addedInNew: [],
      deletedChars: new Map([[1, [{ start: 6, end: 11 }]]]),
      addedChars: new Map([[1, [{ start: 6, end: 14 }]]]),
      modifiedPairs: [[1, 1]],
    }
    const markers = computeCodeDiffMarkers(lineDiff, 'hello world', 'hello universe')
    expect(markers.length).toBeGreaterThan(0)
    const mod = markers.find(m => m.type === 'modified')
    expect(mod).toBeTruthy()
    expect(mod!.lineNumbers).toContain(1)
    expect(mod!.charDiff).toBeTruthy()
  })

  it('creates modified markers even when only deletedChars exists (no addedChars)', () => {
    // This was the bug: pure deletion within a modified line would not
    // produce a marker because addedChars was empty, causing pairCount=0.
    const lineDiff: import('@/utils/diffUtils.ts').LineDiff = {
      deletedInOld: [],
      addedInNew: [],
      deletedChars: new Map([[1, [{ start: 5, end: 11 }]]]),
      addedChars: new Map(),  // empty — no char-level additions
      modifiedPairs: [[1, 1]],
    }
    const markers = computeCodeDiffMarkers(lineDiff, 'hello world', 'hello')
    const mod = markers.find(m => m.type === 'modified')
    expect(mod).toBeTruthy()
    expect(mod!.lineNumbers).toContain(1)
  })

  it('creates modified markers when only addedChars exists (no deletedChars)', () => {
    const lineDiff: import('@/utils/diffUtils.ts').LineDiff = {
      deletedInOld: [],
      addedInNew: [],
      deletedChars: new Map(),  // empty — no char-level deletions
      addedChars: new Map([[1, [{ start: 5, end: 12 }]]]),
      modifiedPairs: [[1, 1]],
    }
    const markers = computeCodeDiffMarkers(lineDiff, 'hello', 'hello world')
    const mod = markers.find(m => m.type === 'modified')
    expect(mod).toBeTruthy()
    expect(mod!.lineNumbers).toContain(1)
  })

  it('creates added markers for new lines', () => {
    const lineDiff: import('@/utils/diffUtils.ts').LineDiff = {
      deletedInOld: [],
      addedInNew: [3],
      deletedChars: new Map(),
      addedChars: new Map(),
      modifiedPairs: [],
    }
    const markers = computeCodeDiffMarkers(lineDiff, 'a\nb', 'a\nb\nc')
    const add = markers.find(m => m.type === 'added')
    expect(add).toBeTruthy()
    expect(add!.lineNumbers).toContain(3)
  })

  it('creates deleted markers for removed lines', () => {
    const lineDiff: import('@/utils/diffUtils.ts').LineDiff = {
      deletedInOld: [2],
      addedInNew: [],
      deletedChars: new Map(),
      addedChars: new Map(),
      modifiedPairs: [],
    }
    const markers = computeCodeDiffMarkers(lineDiff, 'a\nb', 'a')
    const del = markers.find(m => m.type === 'deleted')
    expect(del).toBeTruthy()
  })

  it('groups consecutive modified lines', () => {
    const lineDiff: import('@/utils/diffUtils.ts').LineDiff = {
      deletedInOld: [],
      addedInNew: [],
      deletedChars: new Map([[1, [{ start: 0, end: 1 }]], [2, [{ start: 0, end: 1 }]]]),
      addedChars: new Map([[1, [{ start: 0, end: 1 }]], [2, [{ start: 0, end: 1 }]]]),
      modifiedPairs: [[1, 1], [2, 2]],
    }
    const markers = computeCodeDiffMarkers(lineDiff, 'a\nb', 'x\ny')
    const mod = markers.find(m => m.type === 'modified')
    expect(mod).toBeTruthy()
    expect(mod!.lineNumbers).toEqual([1, 2])
  })

  it('returns empty array for no changes', () => {
    const lineDiff: import('@/utils/diffUtils.ts').LineDiff = {
      deletedInOld: [],
      addedInNew: [],
      deletedChars: new Map(),
      addedChars: new Map(),
      modifiedPairs: [],
    }
    const markers = computeCodeDiffMarkers(lineDiff, 'same', 'same')
    expect(markers).toHaveLength(0)
  })
})
