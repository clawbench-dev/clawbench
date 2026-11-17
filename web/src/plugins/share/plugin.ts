import { Share2 } from 'lucide-vue-next'
import type { DockPlugin } from '@/composables/dockPluginTypes'

/**
 * 分享: every public link this project has handed out (files, conversations,
 * meetings), in one place. It sits next to 会议 because both are "things this
 * machine offers to other people", and it stays before 端口映射 for the same
 * reason — the outgoing-access pair stays together.
 *
 * Share2 (a node with two arrows leaving it), not a link glyph: this tab is the
 * OUTGOING side — what has been handed to other people — and the meeting icon
 * next to it already carries the "screen" half of that idea.
 */
export default {
  id: 'share',
  titleKey: 'nav.share',
  descriptionKey: 'settings.dockPlugins.desc.share',
  group: 'project',
  closable: true,
  feature: 'share.enabled',
  settingsCategory: 'share',
  icon: Share2,
  settingsPage: {
    group: 'dockModules',
    icon: Share2,
    kind: 'flat',
  },
  panel: {
    load: () => import('@/components/share/ShareCenterPanel.vue'),
  },
} satisfies DockPlugin
