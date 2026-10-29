/**
 * Opt-out marker for DOM regions that the annotation pipeline must not touch.
 *
 * Every annotator (bare URL / file path / commit hash / localhost) walks text
 * nodes and elements inside a document and rewrites whatever looks like a
 * reference. That is right for prose, but wrong for regions whose text is
 * already annotated or is not a reference at all:
 *
 *  - a tool call's own file header already renders its path as a clickable chip
 *    with its own open button; re-annotating it nests a second chip and appends
 *    a second button;
 *  - an AskUserQuestion option label is the value the user is choosing — a chip
 *    inside it would swallow the click that selects the option;
 *  - the content body of a Write/Edit/Read preview is the file's own source,
 *    not a reference to it.
 *
 * A marker attribute (rather than an allow-list of selectors inside each
 * annotator) keeps the knowledge in the caller: the producer of the HTML knows
 * which regions it already handled, and each annotator only has to ask one
 * question.
 *
 * This module is deliberately a leaf (no imports): it is pulled in by five
 * annotators and by the tool-detail renderer, and a cycle between those would
 * break module evaluation order.
 */

/** Attribute set on a container whose subtree must not be annotated. */
export const ANNOTATION_SKIP_ATTR = 'data-no-annotate'

/**
 * True when `el` (or any ancestor) is marked as annotation-excluded.
 *
 * `closest` walks up from the element itself, so both a marked container and
 * anything inside it are rejected by one check.
 */
export function isAnnotationSkipped(el: Element | null | undefined): boolean {
  if (!el) return false
  return el.closest(`[${ANNOTATION_SKIP_ATTR}]`) !== null
}
