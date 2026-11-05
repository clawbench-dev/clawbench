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

    // Every quote row is gated on the wide layout: the drop target is the chat
    // column, which a narrow layout never shows at the same time as the drag
    // source. Without the gate the row still LOOKS draggable on a phone and
    // then drops nowhere.
    //
    // GitCommitList additionally skips its working-tree row (no commit to
    // reference); that per-row rule is asserted in its own test file.
    expect(src).toMatch(/:draggable="isWideScreen(?: && canDragCommit\(c\))?"/)
    expect(src).toMatch(/@dragstart="on\w*DragStart\(/)
    // The ghost is a real DOM node kept alive until dragend; without the cleanup
    // it leaks a node into the document on every drag.
    expect(src).toMatch(/@dragend="cleanupDragGhost\(\)"/)

    // The record row of a RUNNING meeting drags too, and the server has no
    // artifacts for it yet — App must fall back to the live transcript instead
    // of answering "quote unavailable".
    expect(readWebFile('src/App.vue')).toMatch(/buildLiveMeetingQuote\(/)
  })

  it.each(ROWS)('%s (%s) starts the drag through startQuoteDrag', (path) => {
    const src = readWebFile(path)

    // One shared entry point, so the payload shape cannot drift per row. The
    // payload is either built inline or hoisted to a local first (GitCommitList
    // hoists it so it can cancel the drag when the row has no commit).
    expect(src).toMatch(/startQuoteDrag\(e, (\w+DragPayload\(|payload\))/)
  })

  it('every other quote source is gated on the wide layout too', () => {
    for (const path of [
      'src/components/live/MeetingList.vue',
      'src/components/live/MeetingDetail.vue',
      'src/components/mail/MailListItem.vue',
    ]) {
      expect(readWebFile(path), `${path} must not look draggable on a phone`)
        .toMatch(/:draggable="isWideScreen/)
    }
  })

  it('the running meeting offers both gestures: click and drag', () => {
    // The record list only had drag, and the detail only had a button. A
    // meeting IN PROGRESS had neither, which is when its content is most
    // worth quoting.
    const src = readWebFile('src/components/live/MeetingStage.vue')

    expect(src, 'the stage body must be a quotable region').toMatch(/data-quote-meeting-token/)
    expect(src, 'click opens the composer').toMatch(/@click="quoteLiveMeeting"/)
    expect(src, 'the same control drags').toMatch(/@dragstart="onQuoteDragStart"/)
    expect(src, 'and is gated like every other drag source').toMatch(/:draggable="isWideScreen"/)
    expect(src).toMatch(/@dragend="cleanupDragGhost\(\)"/)
  })

  it('the drop handler resolves the payloads that carry identity only', () => {
    // A meeting row and a mail row cannot inline their content at dragstart
    // (it would need an await), so the drop path has to fetch it. Dropping one
    // without this branch stages an empty card — or nothing at all.
    const src = readWebFile('src/App.vue')

    expect(src).toMatch(/if \(quote\.meetingToken\)/)
    expect(src).toMatch(/if \(quote\.mailId\)/)
    // A LIVE meeting arrives with its transcript already in the payload, so the
    // drop must stage it instead of asking the server for a record that does
    // not exist yet.
    expect(src).toMatch(/if \(quote\.text\)/)
  })

  it('a chat message is dragged by its quote button, not the bubble', () => {
    // `draggable` on the message body would kill text selection, and selecting
    // text inside a message is how a snippet quote is made. The meta-bar button
    // is the drag handle instead.
    const src = readWebFile('src/components/chat/ChatMessageItem.vue')

    expect(src).toMatch(/:draggable="quoteDragEnabled"/)
    expect(src).toMatch(/@dragstart="onQuoteDragStart"/)
    // Same wide-screen rule as every other quote source.
    expect(src).toMatch(/isWideScreen\.value && !props\.readOnly/)
  })

  it('the commit and task rows also expose a quote BUTTON', () => {
    // Drag alone is undiscoverable: nothing on the row says it can be quoted.
    // The button opens the shared quote bar (so it can carry an annotation),
    // and must not trigger the row's own click (select / open).
    for (const path of [
      'src/components/git/GitCommitList.vue',
      'src/components/task/TaskListPage.vue',
    ]) {
      const src = readWebFile(path)
      expect(src, `${path} must offer a quote button`).toMatch(/@click\.stop="quote\w+\(/)
      expect(src, `${path} must open the shared composer`).toMatch(/openComposer\(/)
    }
  })
})
