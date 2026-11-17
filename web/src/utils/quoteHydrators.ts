import { registerQuoteHydrator } from '@/utils/quoteHydrate'
import { meetingSourceDir, useMeetingHistory } from '@/composables/useMeetingHistory'
import { useLiveShare } from '@/composables/useLiveShare'
import { useLiveNotes } from '@/composables/useLiveNotes'
import { buildLiveMeetingQuote } from '@/utils/liveMeetingQuote'
import { buildMailQuote } from '@/utils/mailQuote'
import { useToast } from '@/composables/useToast'
import { apiGet } from '@/utils/api'
import { gt } from '@/composables/useLocale'
import { appLog } from '@/utils/appLog'
import type { QuoteData } from '@/composables/useChatContext'
import type { MailMessageDetail } from '@/utils/mailTypes'

const TAG = 'QuoteHydrators'

/**
 * Hydrators for the quote sources whose content cannot ride in a drag.
 *
 * Registered once, here, so BOTH drop targets (the chat column and a session
 * row) get the same behaviour from one place. They used to be two hand-written
 * `if` chains, and only one of them was ever mentioned in the spec doc — which
 * is exactly how a drag that worked onto the chat column could silently do
 * nothing on a session row.
 *
 * A source that needs no fetch (a commit, a task, an issue) registers nothing:
 * its payload already carries everything the card renders.
 */

/**
 * A meeting drag carries only its token, so the minutes have to be fetched
 * before there is a card to stage.
 *
 * Two shapes, in order:
 *  - a finished meeting → its artifacts (minutes, else revised, else transcript);
 *  - a meeting this app is STILL publishing → the live transcript held in
 *    memory, because a running meeting has no artifacts yet. Its record row is
 *    draggable like any other and used to answer "quote unavailable"; the
 *    transcript is the content, so quote that instead.
 *
 * Returns null when there is nothing readable either way: a quote with only a
 * title would waste a prompt slot and tell the model nothing. (A drag started
 * from the meeting STAGE never reaches the artifact path — the stage puts its
 * transcript in the payload at dragstart, and the first branch returns it.)
 */
registerQuoteHydrator('meeting', async (token, payload) => {
  if (payload.text) return payload
  const history = useMeetingHistory()
  const record = await history.get(token)
  if (record) {
    const quote = await history.buildQuote(record)
    if (quote) return quote
    const live = liveMeetingQuoteFor(token, meetingSourceDir(record))
    if (live) return live
    return null
  }
  // The record could not be read, but this app may still be publishing it.
  return liveMeetingQuoteFor(token, '')
})

/**
 * The running meeting's quote, when `token` is the one this app is publishing.
 *
 * Reads the publisher-side singletons (useLiveShare / useLiveNotes) rather than
 * any server state: a running meeting exists only in this process.
 */
function liveMeetingQuoteFor(token: string, sourceDir: string): QuoteData | null {
  const share = useLiveShare()
  if (share.token.value !== token) return null
  return buildLiveMeetingQuote({
    token,
    title: share.title.value || '',
    segments: useLiveNotes().segments.value,
    sourceDir,
  })
}

/**
 * A mail row carries its id only, so the body is fetched here.
 *
 * Same builder as the reader's "quote in chat" button, so dragging a row and
 * pressing that button produce an identical card.
 */
registerQuoteHydrator('mail', async (mailId) => {
  try {
    const data = await apiGet<{ message: MailMessageDetail }>(`/api/mail/messages/${mailId}`)
    if (!data?.message) return null
    return buildMailQuote(data.message, gt('mail.list.noSubject'))
  } catch (e) {
    appLog.w(TAG, 'mail quote drop failed', e)
    // The drop happened; say so rather than leaving the user with a drag that
    // visibly did nothing. (The old code path toasted from App.vue only.)
    useToast().show(gt('common.operationFailed'), { type: 'error', icon: 'warning' })
    return null
  }
})
