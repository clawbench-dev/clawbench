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
  FolderGit2,
  MessageSquare,
  MessagesSquare,
  Bot,
  Volume2,
  Mic,
  Brain,
  ScanText,
  Bell,
  Shield,
  Bug,
  Info,
  Gauge,
  Wand2,
  Plug,
  Boxes,
  ChevronRight,
  UserRound,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import SettingsCard from './SettingsCard.vue'
import { DOCK_SETTINGS_PAGES } from '@/composables/dockPluginRuntime'

defineEmits<{
  navigate: [categoryId: string]
}>()

const { t } = useI18n()

/**
 * Rows of the 功能模块 group that do NOT come from a dock plugin.
 *
 * The manager is the one page that configures every plugin, and message push /
 * usage meter are features with a settings page but no dock entry. Everything
 * between them is derived from the plugin registry (see `DOCK_SETTINGS_PAGES`),
 * in DOCK order.
 */
const DOCK_MODULE_ITEMS = [
  { id: 'dockPlugins', icon: LayoutGrid },
  ...DOCK_SETTINGS_PAGES.filter((page) => page.group === 'dockModules').map((page) => ({
    id: page.categoryId,
    icon: page.icon,
  })),
  { id: 'notification', icon: Bell },
  { id: 'usageMeter', icon: Gauge },
]

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
      // 会话索引 right after 会话管理: it is the index those sessions are
      // searched through, so it belongs with the sessions rather than with the
      // model services.
      { id: 'rag', icon: Brain },
      { id: 'agents', icon: Bot },
      { id: 'skills', icon: Wand2 },
      { id: 'mcp', icon: Plug },
    ],
  },
  {
    // 功能模块: everything that configures a left-dock feature lives in one
    // group. The manager comes first (it is where those switches live), then
    // one page per plugin in DOCK order — DERIVED from the plugin manifests, so
    // a new plugin's settings page appears here without touching this file —
    // then the two pages that configure a feature without owning a dock entry
    // (message push, usage meter).
    //
    // These pages stay listed even when their plugin is hidden or switched
    // off: the user needs them to configure the feature back on.
    id: 'dockModules',
    items: DOCK_MODULE_ITEMS,
  },
  {
    // Model capability is its own top-level group: these pages configure the
    // models and engines themselves (provider credentials, the shared
    // embedding service, speech synthesis and recognition), which is a
    // different question from how the chat behaves.
    id: 'modelCapability',
    items: [
      { id: 'providers', icon: Boxes },
      // The embedding service sits next to the providers because it IS one:
      // one endpoint, one model, one key. Both 会话索引 and 知识库 call it, so it
      // owns its own page instead of living inside either consumer.
      { id: 'embedding', icon: ScanText },
      { id: 'tts', icon: Volume2 },
      { id: 'stt', icon: Mic },
    ],
  },
  {
    id: 'system',
    items: [
      { id: 'debug', icon: Bug },
      { id: 'security', icon: Shield },
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
