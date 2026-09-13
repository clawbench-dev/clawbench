/**
 * Presentation metadata for completion-menu sources.
 *
 * Shared by the slash-command and @-file menus so every row can render a
 * consistent source icon + coloured label. Kept out of the component so it can
 * be unit-tested without mounting.
 */

import {
  Bot,
  Clock,
  FolderOpen,
  History,
  Share2,
  Upload,
  Wrench,
  type LucideIcon,
} from 'lucide-vue-next'
import type { CompletionSource } from '@/utils/completionMatch.ts'

export interface SourceMeta {
  icon: LucideIcon
  /** Accent colour used for the label text (light theme). */
  color: string
  /** i18n key under `chat.completion.source`. */
  labelKey: string
}

export const SOURCE_META: Record<CompletionSource, SourceMeta> = {
  'recent-open': { icon: Clock, color: '#0ea5e9', labelKey: 'chat.completion.source.recentOpen' },
  'current-dir': { icon: FolderOpen, color: '#f59e0b', labelKey: 'chat.completion.source.currentDir' },
  'recent-ref': { icon: History, color: '#8b5cf6', labelKey: 'chat.completion.source.recentRef' },
  'recent-upload': { icon: Upload, color: '#10b981', labelKey: 'chat.completion.source.recentUpload' },
  'recent-share': { icon: Share2, color: '#ec4899', labelKey: 'chat.completion.source.recentShare' },
  clawbench: { icon: Wrench, color: '#8b5cf6', labelKey: 'chat.completion.source.clawbench' },
  agent: { icon: Bot, color: '#0ea5e9', labelKey: 'chat.completion.source.agent' },
}
