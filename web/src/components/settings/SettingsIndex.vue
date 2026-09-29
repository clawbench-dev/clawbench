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
  FolderTree,
  MessageSquare,
  Bot,
  SquareTerminal,
  Volume2,
  Mic,
  Brain,
  ArrowLeftRight,
  Globe,
  Bell,
  Shield,
  Bug,
  Info,
  Sparkles,
  ChevronRight,
  Github,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import SettingsCard from './SettingsCard.vue'

defineEmits<{
  navigate: [categoryId: string]
}>()

const { t } = useI18n()

/**
 * Category → group layout for the settings home page. Grouping is purely
 * presentational: the category IDs (and their i18n labels) are unchanged, so
 * deep links and the navigation stack keep working.
 *
 * Keep this list in sync with `categoryItems` in settingsFieldMap.ts — every
 * category defined there must appear here exactly once.
 */
const groupDefs = [
  {
    id: 'appearanceFiles',
    items: [
      { id: 'appearance', icon: Palette },
      { id: 'projectFiles', icon: FolderTree },
    ],
  },
  {
    id: 'aiChat',
    items: [
      { id: 'chat', icon: MessageSquare },
      { id: 'agents', icon: Bot },
      { id: 'aiSummary', icon: Sparkles },
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
      { id: 'frp', icon: Globe },
      { id: 'forgeIntegration', icon: Github },
    ],
  },
  {
    id: 'notifySecurity',
    items: [
      { id: 'notification', icon: Bell },
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
  groupDefs.map(group => ({
    ...group,
    items: group.items.map(cat => ({
      ...cat,
      label: t(`settings.categories.${cat.id}`),
    })),
  }))
)
</script>

<style scoped>
.settings-index {
  padding: var(--space-4);
  background: var(--bg-secondary);
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
  height: 48px;
  padding:0 var(--space-7);
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
  left: 48px;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

@media (hover: hover) {
  .settings-index__row:hover {
    background: var(--bg-secondary);
  }
}

.settings-index__row:active {
  background: var(--bg-tertiary);
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
  font-size: var(--font-size-lg);
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
