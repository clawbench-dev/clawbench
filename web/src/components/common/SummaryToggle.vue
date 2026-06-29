<template>
  <!-- Button mode: inline toggle in chat meta bar -->
  <button v-if="mode === 'button'" class="chat-action-btn chat-action-btn--wide" @click.stop="$emit('toggle')">
    <Sparkles v-if="!showingSummary" :size="14" />
    <FileText v-else :size="14" />
    <span>{{ showingSummary ? labelOriginal : labelSummary }}</span>
  </button>
  <!-- Tab mode: segmented control in task exec detail -->
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
/* ── Tab mode ── */
.summary-toggle-bar {
  display: flex;
  gap: 4px;
  margin-bottom: 12px;
  background: var(--bg-secondary, #f1f5f9);
  border-radius: 8px;
  padding: 3px;
}

.summary-toggle-tab {
  flex: 1;
  border: none;
  background: transparent;
  color: var(--text-secondary, #6b7280);
  font-size: 13px;
  font-weight: 500;
  padding: 6px 12px;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s ease;
  text-align: center;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
}

.summary-toggle-tab.active {
  background: var(--bg-tertiary, #e2e8f0);
  color: var(--text-primary, #1a1a1a);
  font-weight: 500;
}

@media (hover: hover) {
  .summary-toggle-tab:not(.active):hover {
    color: var(--text-primary, #1a1a1a);
    background: var(--bg-tertiary, #e2e8f0);
  }
}
</style>
