<template>
  <div class="settings-index">
    <div
      v-for="cat in categories"
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
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import {
  Palette,
  MapPin,
  MessageSquare,
  Bot,
  FolderOpen,
  Terminal,
  Volume2,
  Sparkles,
  Brain,
  ArrowLeftRight,
  Bell,
  Shield,
  Smartphone,
  Info,
  ChevronRight,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useAppMode } from '@/composables/useAppMode'

defineEmits<{
  navigate: [categoryId: string]
}>()

const { t } = useI18n()
const { isAppMode } = useAppMode()

const categoryDefs = computed(() => [
  { id: 'appearance', icon: Palette },
  { id: 'project', icon: MapPin },
  { id: 'chat', icon: MessageSquare },
  { id: 'agents', icon: Bot },
  { id: 'files', icon: FolderOpen },
  { id: 'terminal', icon: Terminal },
  { id: 'tts', icon: Volume2 },
  { id: 'summarization', icon: Sparkles },
  { id: 'rag', icon: Brain },
  { id: 'portForward', icon: ArrowLeftRight },
  { id: 'push', icon: Bell },
  { id: 'security', icon: Shield },
  ...(isAppMode.value ? [{ id: 'android', icon: Smartphone }] : []),
  { id: 'about', icon: Info },
])

const categories = computed(() =>
  categoryDefs.value.map(cat => ({
    ...cat,
    label: t(`settings.categories.${cat.id}`),
  }))
)
</script>

<style scoped>
.settings-index {
  padding: 8px 0;
  background: var(--bg-secondary);
  min-height: 100%;
}

.settings-index__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 48px;
  padding: 0 16px;
  cursor: pointer;
  gap: 12px;
  background: var(--bg-primary);
  position: relative;
}

/* Grouped card: no border-radius */
.settings-index__row:first-child {
}

.settings-index__row:last-child {
}

.settings-index__row:only-child {
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
  gap: 12px;
  min-width: 0;
}

.settings-index__icon {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.settings-index__label {
  font-size: 15px;
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
