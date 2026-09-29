import { HighlightStyle } from '@codemirror/language'
import { tags } from '@lezer/highlight'

/**
 * Shared CodeMirror syntax-highlight mapping.
 *
 * Every CodeMirror surface in the app must resolve token colours through the
 * same `--code-syntax-*` variables, so a new theme only has to define those
 * variables once. Keeping the mapping here (rather than duplicating the
 * `HighlightStyle.define([...])` array per editor) is what stops the file
 * viewer and the task script editor from drifting apart.
 */
export const codeHighlightStyle = HighlightStyle.define([
    { tag: tags.comment, color: 'var(--code-syntax-comment)', fontStyle: 'italic' },
    { tag: [tags.keyword, tags.operator, tags.modifier], color: 'var(--code-syntax-keyword)' },
    { tag: [tags.string, tags.special(tags.string), tags.regexp, tags.monospace], color: 'var(--code-syntax-string)' },
    { tag: [tags.number, tags.bool, tags.null], color: 'var(--code-syntax-number)' },
    { tag: [tags.function(tags.variableName), tags.function(tags.propertyName), tags.function(tags.definition(tags.variableName))], color: 'var(--code-syntax-function)' },
    { tag: [tags.typeName, tags.className, tags.namespace], color: 'var(--code-syntax-type)' },
    { tag: [tags.variableName, tags.definition(tags.variableName)], color: 'var(--code-syntax-variable)' },
    { tag: [tags.propertyName], color: 'var(--code-syntax-property)' },
    { tag: [tags.tagName], color: 'var(--code-syntax-tag)' },
    { tag: [tags.attributeName], color: 'var(--code-syntax-attribute)' },
    { tag: [tags.meta, tags.contentSeparator], color: 'var(--code-syntax-meta)' },
    { tag: [tags.heading], color: 'var(--code-syntax-heading)', fontWeight: 'bold' },
    { tag: [tags.link, tags.url], color: 'var(--code-syntax-link)', textDecoration: 'underline' },
    { tag: [tags.emphasis], fontStyle: 'italic' },
    { tag: [tags.strong], fontWeight: 'bold' },
    { tag: [tags.quote], color: 'var(--code-syntax-comment)', fontStyle: 'italic' },
])
