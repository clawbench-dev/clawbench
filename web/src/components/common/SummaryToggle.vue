<template>
  <!-- Button mode: inline toggle in chat meta bar -->
  <button v-if="mode === 'button'" class="chat-action-btn chat-action-btn--wide" @click.stop="$emit('toggle')">
    <Sparkles v-if="!showingSummary" :size="14" />
    <FileText v-else :size="14" />
    <span>{{ showingSummary ? labelOriginal : labelSummary }}</span>
  </button>
  <!-- Tab mode: page tabs in task exec detail -->
  <div v-else class="summary-toggle-bar">
    <button class="summary-toggle-tab" :class="{ active: showingSummary }" @click="!showingSummary && $emit('toggle')">
      <Sparkles :size="14" />
      <span>{{ labelSummary }}</span>
    </button>
    <button class="summary-toggle-tab" :class="{ active: !showingSummary }" @click="showingSummary && $emit('toggle')">
      <FileText :size="14" />
      <span>{{ labelOriginal }}</span>
    </button>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { Sparkles, FileText } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  /** Display mode: 'button' for chat meta bar, 'tab' for task exec detail */
  mode: { type: String, default: 'button' },
  /** Whether the summary view is currently shown */
  showingSummary: { type: Boolean, default: false },
  /** i18n key prefix for labels (e.g. 'chat.message' or 'task.exec') */
  i18nPrefix: { type: String, default: 'chat.message' },
})

defineEmits(['toggle'])

const { t } = useI18n()

// Tab mode uses short labels (tabSummary/tabOriginal), button mode uses action labels (summaryViewSummary/summaryViewOriginal)
const labelSummary = computed(() => t(`${props.i18nPrefix}.${props.mode === 'tab' ? 'tabSummary' : 'summaryViewSummary'}`))
const labelOriginal = computed(() => t(`${props.i18nPrefix}.${props.mode === 'tab' ? 'tabOriginal' : 'summaryViewOriginal'}`))
</script>

<style scoped>
/* ── Tab mode — matches the stats panel tab bar: connected rectangular tabs
   where the active one gets a tinted background and a bottom accent line. ── */
.summary-toggle-bar {
  display: flex;
  align-items: stretch;
  height: 34px;
  margin-bottom: 12px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color);
}

.summary-toggle-tab {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 0 16px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-md);
  font-weight: 500;
  cursor: pointer;
  user-select: none;
  -webkit-tap-highlight-color: transparent;
  position: relative;
}

@media (hover: hover) {
  .summary-toggle-tab:not(.active):hover {
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }
}

.summary-toggle-tab.active {
  color: var(--text-primary);
  background: color-mix(in srgb, var(--text-primary) 8%, transparent);
}

.summary-toggle-tab.active::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 2px;
  background: var(--accent-color);
}
</style>
