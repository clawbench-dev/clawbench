/**
 * Parse the "go to line" input into a clamped 1-based line number.
 *
 * Accepted forms (mirroring VS Code's go-to-line box, minus the column part —
 * this editor has no caret placement in read-only browsing):
 *   "42"      absolute line
 *   "+7"      seven lines below the current line
 *   "-7"      seven lines above the current line
 *   "50%"     that percentage through the document
 *
 * Returns null for anything unparseable, so the caller can keep the dialog open
 * (or ignore the submit) instead of jumping to line 1 on a typo. A *parseable*
 * value that falls outside the document is clamped, not rejected (see below).
 *
 * `currentLine` and `totalLines` must both be >= 1. Out-of-range results are
 * clamped rather than rejected: typing 9999 in a 100-line file should land on
 * the last line, which is what every editor does.
 */
export function parseGoToLine(
    input: string,
    currentLine: number,
    totalLines: number,
): number | null {
    const text = String(input ?? '').trim()
    if (!text) return null

    const total = Math.max(1, Math.floor(totalLines))
    const current = Math.min(total, Math.max(1, Math.floor(currentLine)))

    // Percentage: "50%" / "+10%" (relative percentage) / "-10%".
    const percentMatch = /^([+-])?(\d+(?:\.\d+)?)%$/.exec(text)
    if (percentMatch) {
        const [, sign, raw] = percentMatch
        let ratio = Number(raw) / 100
        if (sign === '-') ratio = -ratio
        const target = sign
            ? current + Math.round(total * ratio)
            : Math.round(total * ratio)
        return clamp(target, total)
    }

    // Absolute ("42") or relative ("+7" / "-7").
    const lineMatch = /^([+-])?(\d+)$/.exec(text)
    if (!lineMatch) return null
    const [, sign, raw] = lineMatch
    const value = Number(raw)
    if (!Number.isFinite(value)) return null
    const target = sign === '+' ? current + value : sign === '-' ? current - value : value
    return clamp(target, total)
}

function clamp(target: number, total: number): number {
    return Math.min(total, Math.max(1, Math.floor(target)))
}
