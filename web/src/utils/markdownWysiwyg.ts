/**
 * Safety gate for the visual (WYSIWYG) markdown editor.
 *
 * The visual editor parses the file into its own document model and serializes
 * it back on save, so anything the model has no place for is lost. Most CommonMark
 * and GFM constructs survive, but a YAML frontmatter block does not: the opening
 * `---` is read as a thematic break and the key/value lines become headings and
 * paragraphs, so saving would silently destroy the file's metadata.
 *
 * Files that hit a blocker stay editable in the source editor (CodeMirror),
 * which is byte-exact. This is deliberately a small allow/deny list rather than
 * a round-trip check: a round-trip would have to run the editor to answer, and
 * the user would already be inside it by then.
 */
export type WysiwygBlockReason = 'frontmatter'

/** Returns why the visual editor must not open this text, or null when safe. */
export function wysiwygBlockReason(content: string | null | undefined): WysiwygBlockReason | null {
    const src = (content || '').replace(/^\uFEFF/, '')
    // YAML frontmatter: a `---` line as the very first line. A later `---` is a
    // thematic break and round-trips fine.
    if (/^---[ \t]*\r?\n/.test(src)) return 'frontmatter'
    return null
}
