import { Github } from 'lucide-vue-next'
import i18n from '@/i18n'
import { appLog } from '@/utils/appLog'
import {
  clearPendingForgeTarget,
  forgeTargetFromDetail,
  setPendingForgeTarget,
  type ForgeTarget,
} from '@/composables/useForgeNavigation'
import { forgeDockIconKind, forgeTabTitleKey, useForgeBinding } from '@/composables/useForgeBinding'
import { forgeBrandIcon } from '@/components/forge/forgeBrandIcon'
import { useForgeUnread } from '@/composables/useForgeUnread'
import type { DockPlugin } from '@/composables/dockPluginTypes'

const TAG = 'ForgeDeepLink'

const { forgeUnreadCount } = useForgeUnread()
const { platform } = useForgeBinding()

/**
 * Payload of the `clawbench-open-forge` event (forge system notification tap).
 *
 * `target` and `forgeTarget` are the SAME value under two names, because the
 * event has two producers with different shapes:
 *   - the renderer's own paths (in-page notification onClick, completion card)
 *     dispatch `{ projectPath, target }`;
 *   - the native shell (Electron) forwards its whole `NotificationNav` verbatim
 *     through preload as the event detail, and that object names the field
 *     `forgeTarget`.
 */
interface OpenForgeDetail {
  projectPath?: string
  target?: ForgeTarget
  forgeTarget?: ForgeTarget
  fromNotificationCenter?: boolean
}

/**
 * Forge is the one plugin whose icon and title depend on runtime state: the tab
 * names and draws the integration the project is actually bound to.
 *
 * Its manifest `titleKey` is the platform-NEUTRAL label ("issues & change
 * requests"), used only as the last fallback when no repository is bound. The
 * dock does not use it: `title()` resolves `forgeTabTitleKey(platform)`, whose
 * every platform key now reads 代码托管集成 — the feature has ONE name, and the
 * brand names (GitHub / GitLab / Gitea) moved into the description text. The
 * glyph still follows the platform, so the entry keeps showing which service
 * the project is bound to.
 */
export default {
  id: 'forge',
  titleKey: 'nav.forge',
  // Same key the dock label uses. `nav.forge` stays the platform-neutral
  // fallback (and the manifest guard requires a static key), but every
  // surface that names the feature asks for the key through the runtime, so
  // the settings page and the panel header say the same service the dock does.
  dynamicTitleKey: () => forgeTabTitleKey(platform.value),
  descriptionKey: 'settings.dockPlugins.desc.forge',
  group: 'project',
  closable: true,
  feature: 'forge.enabled',
  settingsCategory: 'forgeIntegration',
  icon: Github,
  settingsPage: {
    group: 'dockModules',
    icon: Github,
    kind: 'flat',
  },
  dynamicIcon: () => forgeBrandIcon(forgeDockIconKind(platform.value)),
  title: () => i18n.global.t(forgeTabTitleKey(platform.value)),
  badge: () => ({ count: forgeUnreadCount.value }),
  // No `available()`: the feature axis (switched off / on) belongs to the
  // server's plugin registry (`GET /api/features`), not to a second copy of the
  // config check here. `available` is reserved for HOST capability.
  deeplinks: [
    {
      event: 'clawbench-open-forge',
      /**
       * Handle clawbench-open-forge — a forge (GitHub/GitLab) system
       * notification was clicked, or the in-app completion card was tapped.
       *
       * The forge panel is project-scoped: it shows the repository bound to the
       * ACTIVE project. A change in a repository bound by another project must
       * therefore switch projects first, or the user lands on a panel that has
       * no such row (and the notification looks like a lie).
       *
       * The item to open is published as a module-level pending target rather
       * than passed down as a prop: the panel is mounted lazily and, when the
       * forge tab is already active, `switchTab` is a no-op — so the ref itself
       * is the only signal that reaches it. See useForgeNavigation.
       *
       * The ORDER here is load-bearing and is preserved verbatim from the shell
       * handler it replaced: refuse → publish → same-project jump / cross-
       * project switch (clearing the target when the switch did not land).
       */
      handler: (raw, ctx) => {
        const detail = raw as OpenForgeDetail | undefined
        const projectPath = detail?.projectPath
        // Accept either name — see forgeTargetFromDetail.
        const target = forgeTargetFromDetail(detail)

        // Refuse the whole navigation while the integration is unavailable,
        // BEFORE any state is written. `switchTab` would otherwise reject the
        // tab on its own, but by then this handler has already published a
        // pending target and registered a back-origin — so the next time the
        // panel mounts (after the user re-enables it) it would jump to a stale
        // item, and Back would point at a jump that never happened. A
        // notification queued before the switch was turned off is the realistic
        // way to get here.
        if (ctx.tabBlocked('forge')) {
          clearPendingForgeTarget()
          return
        }

        // Publish the target before switching tabs so the panel sees it whichever
        // path it takes to become visible (mount, activation, or ref change).
        //
        // The target is stamped with the project it belongs to. Without a
        // concrete destination the panel's own project gate has nothing to
        // compare against and a still-mounted old-project panel would consume a
        // target meant for another project. `projectPath` here is the
        // attribution the server sent; empty means "whatever project is active",
        // which is exactly the same-project case.
        if (target) setPendingForgeTarget({ ...target, projectPath })

        if (!projectPath || projectPath === ctx.currentProject()) {
          // Same project (or the event carried no attribution): nothing to switch.
          ctx.recordNotificationOrigin(detail)
          ctx.switchTab('forge')
          return
        }
        // hotSwitchProject resolves even when the target project is unusable —
        // it toasts and returns early rather than rejecting — so the tab is
        // opened after the attempt settles either way. That is also the right
        // fallback: the user lands on the forge panel they can actually see,
        // instead of nothing happening.
        ctx
          .hotSwitchProject(projectPath)
          .then(() => {
            // The switch was refused (project gone / not under the configured
            // roots), so the panel that is actually on screen is the CURRENT
            // project's — which cannot hold this item. Drop the target rather
            // than leave it armed: it would otherwise fire on some later
            // activation whose project happened to match.
            if (ctx.currentProject() !== projectPath) clearPendingForgeTarget()
          })
          .catch(() => {
            appLog.w(TAG, 'clawbench-open-forge: project switch threw, opening current project')
            clearPendingForgeTarget()
          })
          .finally(() => ctx.switchTab('forge'))
      },
    },
  ],
  panel: {
    load: () => import('@/components/forge/ForgePanelContent.vue'),
    props: (host) => ({ projectPath: host.projectPath }),
    // "Unbound — pick a project" means: leave the forge panel and show the
    // chat (the right-hand pane), which is where project switching lives.
    on: (host) => ({ 'request-project': () => host.openTab('chat') }),
  },
} satisfies DockPlugin
