<template>
  <div class="settings-index">
    <SettingsCard
      v-for="group in groups"
      :key="group.id"
      :title="t(`settings.groups.${group.id}`)"
    >
      <div
        v-for="cat in group.items"
        :key="cat.id"
        class="settings-index__row"
        :data-category="cat.id"
        @click="$emit('navigate', cat.id)"
      >
        <div class="settings-index__left">
          <component :is="cat.icon" class="settings-index__icon" :size="18" />
          <span class="settings-index__label">{{ cat.label }}</span>
        </div>
        <ChevronRight class="settings-index__arrow" :size="18" />
      </div>
    </SettingsCard>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import {
  Palette,
  LayoutGrid,
  Eye,
  BookOpen,
  Inbox,
  FolderTree,
  FolderGit2,
  MessageSquare,
  MessagesSquare,
  Bot,
  SquareTerminal,
  Volume2,
  Mic,
  Brain,
  ArrowLeftRight,
  Bell,
  Shield,
  Bug,
  Info,
  Gauge,
  Wand2,
  Plug,
  Boxes,
  ChevronRight,
  Github,
  UserRound,
  Mail,
  History,
  ListTodo,
  ChartColumn,
  Share2,
} from 'lucide-vue-next'
import { MEETING_ICON } from '@/utils/meetingIcon'
import { useI18n } from 'vue-i18n'
import SettingsCard from './SettingsCard.vue'

defineEmits<{
  navigate: [categoryId: string]
}>()

const { t } = useI18n()

/**
 * Category → group layout for the settings home page. Grouping is purely
 * presentational: the category IDs are unchanged, so deep links and the
 * navigation stack keep working (only the display labels are translated).
 *
 * Every category defined in `categoryItems` (settingsFieldMap.ts) must appear
 * here exactly once; `SettingsIndex.test.ts` enforces both directions, because a
 * category with no row is unreachable and a row with no category is a blank
 * page.
 */
const groupDefs = [
  {
    id: 'projectAppearance',
    items: [
      { id: 'project', icon: FolderGit2 },
      { id: 'appearance', icon: Palette },
      { id: 'dockPlugins', icon: LayoutGrid },
      { id: 'files', icon: FolderTree },
      { id: 'fileView', icon: Eye },
      // Both are per-project surfaces with their own defaults, so they sit with
      // the project and the file pages rather than with the AI features.
      { id: 'gitHistory', icon: History },
      { id: 'stats', icon: ChartColumn },
    ],
  },
  {
    id: 'aiChat',
    items: [
      // 身份 first: the nickname and self-description are the context every
      // agent sees, and they are not specific to chat or group chat.
      { id: 'identity', icon: UserRound },
      { id: 'chat', icon: MessageSquare },
      // 会话管理 right after 聊天: it is the same subject one level up (sessions
      // as objects, not the conversation), and separating them made users hunt
      // for the session-list options in two places.
      { id: 'sessions', icon: MessagesSquare },
      { id: 'agents', icon: Bot },
      { id: 'tasks', icon: ListTodo },
      { id: 'skills', icon: Wand2 },
      { id: 'mcp', icon: Plug },
      // 知识库 stays here, next to the other AI features. It shares the embedder
      // with RAG, but its own configuration is about what gets indexed, not
      // about the model.
      { id: 'kb', icon: BookOpen },
      // Meeting defaults sit next to the AI features they depend on. The icon
      // comes from utils/meetingIcon so this row matches the dock tab instead
      // of drawing a second, unrelated meeting glyph.
      { id: 'meeting', icon: MEETING_ICON },
    ],
  },
  {
    // Model capability is its own top-level group: these four pages configure
    // the models and engines themselves (provider credentials, the embedder,
    // speech synthesis and recognition), which is a different question from how
    // the chat behaves.
    id: 'modelCapability',
    items: [
      { id: 'providers', icon: Boxes },
      { id: 'rag', icon: Brain },
      { id: 'tts', icon: Volume2 },
      { id: 'stt', icon: Mic },
    ],
  },
  {
    id: 'connectivity',
    items: [
      { id: 'terminal', icon: SquareTerminal },
      { id: 'portForward', icon: ArrowLeftRight },
      // Mailbox accounts sit with the other outbound integrations: they are a
      // connection this machine holds, not a per-project setting.
      { id: 'mail', icon: Mail },
      // Sharing is an outbound integration too: it hands content to people who
      // are not logged in.
      { id: 'share', icon: Share2 },
      { id: 'forgeIntegration', icon: Github },
    ],
  },
  {
    id: 'notifySecurity',
    items: [
      { id: 'notification', icon: Bell },
      { id: 'notifications', icon: Inbox },
      // Side by side with 推送通知, not inside it: the usage meter is a
      // notification feature but its configuration (cycle / metric / budget)
      // is a separate subject, and nesting it under the push-channel card made
      // it read as one more push channel.
      { id: 'usageMeter', icon: Gauge },
      { id: 'security', icon: Shield },
    ],
  },
  {
    id: 'systemAbout',
    items: [
      { id: 'debug', icon: Bug },
      { id: 'about', icon: Info },
    ],
  },
]

const groups = computed(() =>
  groupDefs
    .map(group => ({
      ...group,
      items: group.items.map(cat => ({
        ...cat,
        label: t(`settings.categories.${cat.id}`),
      })),
    }))
    // Dropping the only item of a group would leave an empty titled card.
    .filter(group => group.items.length > 0)
)
</script>

<style scoped>
.settings-index {
  padding: var(--space-4);
  /* Same page surface as every other left-panel tab; the grouped cards below
     are the surface step on top of it. */
  background: var(--panel-page-bg);
  min-height: 100%;
}

/* The trailing gap after the last card is redundant page padding. */
.settings-index > :deep(.settings-card:last-child) {
  margin-bottom: 0;
}

.settings-index__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: calc(42 * var(--dp));
  padding: 0 var(--space-7);
  cursor: pointer;
  gap: var(--space-6);
  background: transparent;
  position: relative;
}

/* Row separator (not on last) */
.settings-index__row:not(:last-child)::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: calc(48 * var(--dp));
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

@media (hover: hover) {
  /* The row sits on a --bg-secondary card, so the old bg-secondary hover
     vanished into it. Mixed from the text colour: correct in every theme,
     unlike a fixed one-step background jump. */
  .settings-index__row:hover {
    background: color-mix(in srgb, var(--text-primary) 5%, var(--bg-secondary));
  }
}

.settings-index__row:active {
  background: color-mix(in srgb, var(--text-primary) 10%, var(--bg-secondary));
}

.settings-index__left {
  display: flex;
  align-items: center;
  gap: var(--space-6);
  min-width: 0;
}

.settings-index__icon {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.settings-index__label {
  font-size: var(--font-size-md);
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-index__arrow {
  flex-shrink: 0;
  color: var(--text-muted);
}
</style>
