import {
  Clock,
  GitCommit,
  GitPullRequest,
  Link as LinkIcon,
  ListChecks,
  MessageSquareText,
  MousePointer2,
  PlayCircle,
  SquareTerminal,
  CircleDot,
  FileCode,
} from 'lucide-vue-next'
import type { QuoteDisplayType } from '@/utils/quoteItem'

/**
 * Icon for each quote display type, shared by the attachment card and the
 * detail drawer so the two can never disagree about what a quote is.
 *
 * A module of its own (rather than a map inside QuoteCard) because the drawer
 * needs the same mapping, and duplicating it is how the card and the drawer
 * drift apart. Kept out of `quoteItem.ts` so that pure utility stays free of a
 * lucide dependency — it is imported by modules that tests mock narrowly.
 *
 * Icon choice, by what the user recognises:
 *   - task/exec   → a clock (cron) and a run list (one execution)
 *   - diff/pipeline → a commit dot and a CI run
 *   - pr/issue    → a merge request and an open item
 *   - terminal/chat/file/selection → terminal, message, code, pointer
 */
export const QUOTE_TYPE_ICON: Record<QuoteDisplayType, unknown> = {
  task: Clock,
  exec: ListChecks,
  diff: GitCommit,
  pipeline: PlayCircle,
  pr: GitPullRequest,
  issue: CircleDot,
  link: LinkIcon,
  terminal: SquareTerminal,
  chat: MessageSquareText,
  file: FileCode,
  selection: MousePointer2,
}

/**
 * i18n key for each type's label. Kept next to the icon so a new type cannot be
 * given one without the other.
 *
 * These are the labels the user asked for: a git icon WITH a "PR" tag, a
 * terminal icon WITH a "terminal" tag, and so on.
 */
export const QUOTE_TYPE_LABEL_KEY: Record<QuoteDisplayType, string> = {
  task: 'quoteBar.typeTask',
  exec: 'quoteBar.typeExec',
  diff: 'quoteBar.typeDiff',
  pipeline: 'quoteBar.typePipeline',
  pr: 'quoteBar.typePr',
  issue: 'quoteBar.typeIssue',
  link: 'quoteBar.typeLink',
  terminal: 'quoteBar.typeTerminal',
  chat: 'quoteBar.messageQuote',
  file: 'quoteBar.typeFile',
  selection: 'quoteBar.selectionQuote',
}
