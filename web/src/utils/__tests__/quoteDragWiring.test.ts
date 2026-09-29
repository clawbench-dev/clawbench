import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The quote-drag drop path lives in App.vue, which has no unit test (it is the
 * whole app). Every part of it is easy to lose in a refactor without any
 * behavioural test noticing, so the wiring is asserted at the source level.
 */
describe('quote drag drop wiring', () => {
  const APP = 'src/App.vue'

  it('reads the quote payload on drop', () => {
    const src = readWebFile(APP)

    expect(src).toMatch(/import \{[^}]*readQuoteDragData[^}]*\} from '\.\/utils\/quoteDrag'/)
    expect(src).toMatch(/const quote = readQuoteDragData\(e\.dataTransfer\)/)
  })

  // Dropping must STAGE the card (no annotation) and land the user in the chat,
  // which is the whole point of the feature.
  it('stages the card without an annotation and switches to the chat', () => {
    const src = readWebFile(APP)

    expect(src).toMatch(/addStagedQuote\(quote\)/)
    expect(src).toMatch(/switchTab\('chat'\)/)
  })

  // Without this the browser never fires `drop` on the chat column, so the
  // feature silently does nothing.
  it('allows the drop in dragover', () => {
    const src = readWebFile(APP)
    const body = functionBody(src, 'onChatColDragOver')

    // Scoped to THIS function: a loose `[\s\S]*?` would happily match the
    // preventDefault inside onChatColDrop and pass even with the quote case
    // removed here.
    const allowLine = body.split('\n').find(l => l.includes('preventDefault()'))
    expect(allowLine, 'dragover must call preventDefault for a quote drag').toBeTruthy()
    expect(allowLine).toContain('quote')
  })

  it('shows the drop highlight for a quote drag', () => {
    const src = readWebFile(APP)
    const body = functionBody(src, 'onChatColDragEnter')

    expect(body).toContain('hasQuoteDragData(e.dataTransfer)')
    // The guard that decides whether to highlight must actually consult it.
    expect(body).toMatch(/if \(!internal && !quote && !osFiles\) return/)
  })
})

/**
 * Extract a top-level `function name(...) { ... }` body by brace matching.
 *
 * Source-level assertions need this: a lazy `[\s\S]*?` between two landmarks
 * silently matches ACROSS function boundaries, so a mutation inside the function
 * under test can still satisfy the pattern via an unrelated later function.
 */
function functionBody(src: string, name: string): string {
  const start = src.indexOf(`function ${name}(`)
  if (start < 0) throw new Error(`function ${name} not found`)
  const open = src.indexOf('{', start)
  let depth = 0
  for (let i = open; i < src.length; i++) {
    if (src[i] === '{') depth++
    else if (src[i] === '}') {
      depth--
      if (depth === 0) return src.slice(open + 1, i)
    }
  }
  throw new Error(`unbalanced braces in ${name}`)
}

/**
 * All four draggable item types must actually declare the drag handlers —
 * otherwise the row looks draggable but drops nothing.
 */
describe('draggable item rows', () => {
  const ROWS: [string, string][] = [
    ['src/components/git/GitCommitList.vue', 'commit'],
    ['src/components/task/TaskListPage.vue', 'task'],
    ['src/components/forge/ForgePanelContent.vue', 'forge issue/PR + pipeline'],
    ['src/components/forge/ForgeOverviewList.vue', 'forge activity'],
  ]

  it.each(ROWS)('%s (%s) declares draggable + dragstart + dragend', (path) => {
    const src = readWebFile(path)

    // GitCommitList is conditionally draggable (`:draggable="canDragCommit(c)"`):
    // its working-tree row has no commit to reference. Everything else is
    // unconditionally draggable. Both shapes are accepted here; the specific
    // GitCommitList rule is asserted behaviourally in its own test file.
    expect(src).toMatch(/:?draggable="(true|canDragCommit\(c\))"/)
    expect(src).toMatch(/@dragstart="on\w*DragStart\(/)
    // The ghost is a real DOM node kept alive until dragend; without the cleanup
    // it leaks a node into the document on every drag.
    expect(src).toMatch(/@dragend="cleanupDragGhost\(\)"/)
  })

  it.each(ROWS)('%s (%s) starts the drag through startQuoteDrag', (path) => {
    const src = readWebFile(path)

    // One shared entry point, so the payload shape cannot drift per row. The
    // payload is either built inline or hoisted to a local first (GitCommitList
    // hoists it so it can cancel the drag when the row has no commit).
    expect(src).toMatch(/startQuoteDrag\(e, (\w+DragPayload\(|payload\))/)
  })
})
