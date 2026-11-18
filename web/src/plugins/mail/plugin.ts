import { Mail } from 'lucide-vue-next'
import { appLog } from '@/utils/appLog'
import { useMailUnread, startMailUnreadPolling, stopMailUnreadPolling } from '@/composables/useMailUnread'
import type { DockPlugin } from '@/composables/dockPluginTypes'
import { parkPendingMailOpen } from '@/composables/useMailOpen'

const TAG = 'MailDeepLink'

const { total } = useMailUnread()

/**
 * Mail: a full mail client (accounts, folders, reading, composing). Data is
 * global rather than per-project — a mailbox belongs to the person, not to a
 * repository — so the tab looks the same in every project.
 *
 * The unread poller is the only background resource in the dock: it exists
 * solely to feed this badge, so the host starts it only while the entry is
 * visible and the server feature is on.
 */
export default {
  id: 'mail',
  titleKey: 'nav.mail',
  descriptionKey: 'settings.dockPlugins.desc.mail',
  group: 'personal',
  closable: true,
  feature: 'mail.enabled',
  settingsCategory: 'mail',
  icon: Mail,
  settingsPage: {
    group: 'dockModules',
    icon: Mail,
    kind: 'group',
  },
  badge: () => ({ count: total.value }),
  // No `available()`: the feature axis belongs to the server registry (see
  // forge/plugin.ts). The background poller is gated by the same availability
  // object in useDockLifecycle.
  deeplinks: [
    {
      event: 'clawbench-open-mail',
      /**
       * A quote jumped to the email it was taken from.
       *
       * The mail PANEL listens to the same event and opens the reader (it owns
       * the list/reader state). This handler does the one thing the panel cannot:
       * switch to the 邮件 tab. Mail is global rather than project-scoped, so
       * unlike the meeting jump there is no project to rebuild first.
       */
      handler: (raw, ctx) => {
        const detail = raw as { messageId?: number } | undefined
        if (!detail?.messageId) {
          appLog.w(TAG, 'clawbench-open-mail: no messageId in detail, ignoring')
          return
        }
        if (ctx.tabBlocked('mail')) return
        // A notification-center row must arm the log as Back's target; a quote
        // jump carries no `fromNotificationCenter` flag, so this is a no-op for
        // that path (see recordNotificationOrigin in App.vue).
        ctx.recordNotificationOrigin(detail)
        // The panel is lazily mounted, so on a cold jump there is no listener
        // yet and the id would be dropped — park it for the mount that is about
        // to happen (a mounted panel handles the event itself, see useMailOpen).
        parkPendingMailOpen(detail.messageId)
        ctx.switchTab('mail')
      },
    },
    {
      event: 'clawbench-compose-mail',
      /**
       * Another panel asked for a new message with the body already filled in
       * (分享中心's 「用邮件发送」).
       *
       * The parked request itself is consumed by the mail panel — it owns the
       * account list and the composer. This handler only switches to the tab,
       * and refuses when the feature is off, exactly like the open-mail jump, so
       * a link never strands the user on a panel with no entry pointing at it.
       */
      handler: (_raw, ctx) => {
        if (ctx.tabBlocked('mail')) return
        ctx.switchTab('mail')
      },
    },
  ],
  panel: {
    load: () => import('@/components/mail/MailPanel.vue'),
    on: (host) => ({ 'open-settings': () => host.openSettingsCategory('mail') }),
  },
  startBackground: () => {
    startMailUnreadPolling()
    return stopMailUnreadPolling
  },
} satisfies DockPlugin
