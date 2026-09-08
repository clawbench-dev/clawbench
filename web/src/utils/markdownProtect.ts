// Shared markdown "protection" — extracts code spans/blocks and LaTeX math
// from raw markdown before marked.parse, replacing them with NUL/SOH-delimited
// placeholders. Both the render pipeline (useMarkdownRenderer) and the TOC
// heading-id pipeline (utils/toc.ts) run this SAME function on the SAME
// document, so the slug inputs both channels produce are identical — keeping
// heading anchor ids in sync (fixes TOC jump mismatches for math headings).

/** Display / inline LaTeX math block that was pre-extracted. */
export interface MathEntry {
    math: string
    displayMode: boolean
}

/** Placeholder for a pre-extracted math block: \x00MATHI<n>\x00 or \x00MATHD<n>\x00 */
// eslint-disable-next-line no-control-regex -- NUL bytes are intentional placeholder delimiters
export const MATH_PH_RE = /\x00MATH([DI])(\d+)\x00/g

/** Placeholder for a pre-extracted inline/fenced code segment: \x01CODE<n>\x01 */
// eslint-disable-next-line no-control-regex -- SOH bytes are intentional placeholder delimiters
export const CODE_PH_RE = /\x01CODE(\d+)\x01/g

export interface ProtectResult {
    /**
     * Markdown with math replaced by `\x00MATH[DI]<n>\x00` placeholders and code
     * restored to its original literal text. This is EXACTLY the text marked
     * receives in the render pipeline, so heading slug inputs derived from it
     * (toc.ts) match the heading ids marked generates.
     *
     * Row count equals the source row count: fenced blocks collapse to a single
     * placeholder row during protection but are restored (multi-line) at the
     * end, and display-math placeholders are padded with newlines to span the
     * same rows the formula occupied — so line numbers never shift for either
     * code or math.
     */
    protected: string
    /** Pre-extracted math blocks, in placeholder order. */
    mathEntries: MathEntry[]
}

/**
 * Protect LaTeX math (and code, so `$` inside code isn't mistaken for math)
 * before marked.parse.
 *
 * Pipeline mirrors the original `extractCodeAndMath` in useMarkdownRenderer:
 *  1. Fenced code blocks  ```…``` / ~~~…~~~  → single `\x01CODE<n>\x01` row
 *  2. Inline code spans   `` `…` ``          → single `\x01CODE<n>\x01`
 *  3. Display math `$$…$$` / `\[…\]` and inline math `$…$` / `\(…\)`
 *     → `\x00MATH[DI]<n>\x00`, appended to `mathEntries` for later re-render
 *  4. Restore code placeholders back to their original text.
 *
 * Math placeholders stay hidden from marked (so `_`/`*` inside formulas never
 * become emphasis), while code is restored because marked should render code
 * verbatim. Heading anchors computed from the result therefore carry the math
 * placeholder tokens (e.g. `能量-mathi0`) — matching the render channel.
 */
export function protectMarkdown(markdown: string): ProtectResult {
    const mathEntries: MathEntry[] = []
    const codeBlocks: string[] = []
    let codeIdx = 0

    let result = markdown

    // 1a. Fenced code blocks: collapse whole block to one placeholder row.
    result = result.replace(/(?:^|\n)(~~~+|```+)[^\n]*\n[\s\S]*?\n\1[ \t]*(?=\n|$)/g, (block) => {
        const ph = `\x01CODE${codeIdx++}\x01`
        codeBlocks.push(block)
        return ph
    })

    // 1b. Inline code spans: one or more backticks, content between matching runs.
    result = result.replace(/(`+)([^`]+?)\1/g, (match) => {
        const ph = `\x01CODE${codeIdx++}\x01`
        codeBlocks.push(match)
        return ph
    })

    // 2. Math blocks.
    let mathIdx = 0
    const ph = (displayMode: boolean) => {
        const prefix = displayMode ? 'MATHD' : 'MATHI'
        return `\x00${prefix}${mathIdx++}\x00`
    }

    // Display math can span multiple source lines. Replacing the whole block
    // with a single-line placeholder would drop those lines and shift every
    // line number that follows (toc.ts, data-source-line, …). Pad the
    // placeholder with (blockLines − 1) newlines so the protected text keeps
    // the same row count as the source — mirroring what step 3 does for code.
    const displayPh = (whole: string, math: string) => {
        mathEntries.push({ math: math.trim(), displayMode: true })
        const blockLines = whole.split('\n').length
        return ph(true) + '\n'.repeat(blockLines - 1)
    }

    // 2a. Display math: $$...$$
    result = result.replace(/\$\$([\s\S]+?)\$\$/g, (whole, math) => displayPh(whole, math))

    // 2b. Display math: \[...\]
    result = result.replace(/\\\[([\s\S]+?)\\\]/g, (whole, math) => displayPh(whole, math))

    // 2c. Inline math: $...$ (excludes currency / escaped \$ / $$
    //     — same rules as the legacy INLINE_MATH_RE).
    result = result.replace(/(^|[^$\d\\])\$(?!\$)([^$\n]+?)\$(?!\d)/g, (_whole, pre, math) => {
        mathEntries.push({ math: math.trim(), displayMode: false })
        return pre + ph(false)
    })

    // 2d. Inline math: \(...\)
    result = result.replace(/\\\(([^\\\n]+?)\\\)/g, (_whole, math) => {
        mathEntries.push({ math: math.trim(), displayMode: false })
        return ph(false)
    })

    // 3. Restore code segments — marked renders real code verbatim; only math
    //    stays placeholder-hidden.
    result = result.replace(CODE_PH_RE, (_m, ci) => {
        const i = parseInt(ci, 10)
        return codeBlocks[i] ?? _m
    })

    return { protected: result, mathEntries }
}
