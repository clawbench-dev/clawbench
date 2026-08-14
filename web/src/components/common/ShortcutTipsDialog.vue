<template>
  <ModalDialog :open="open" :max-width="720" @close="$emit('close')">
    <template #header>
      <Keyboard :size="16" class="modal-header-icon" />
      <span class="modal-title">{{ title }}</span>
    </template>
    <div v-if="groups.length === 0" class="st-dialog-empty">{{ t('appHeader.shortcutTipsDialog.empty') }}</div>
    <div v-else class="st-dialog-body">
      <section v-for="g in groups" :key="g.context" class="st-group">
        <h4 class="st-group-title">
          {{ t('appHeader.shortcutTipGroup.' + g.context) }}
          <span class="st-group-count">{{ g.tips.length }}</span>
        </h4>
        <table class="st-table">
          <thead>
            <tr>
              <th class="st-col-key">{{ t('appHeader.shortcutTipsDialog.colKey') }}</th>
              <th class="st-col-context">{{ t('appHeader.shortcutTipsDialog.colContext') }}</th>
              <th class="st-col-action">{{ t('appHeader.shortcutTipsDialog.colAction') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="tip in g.tips" :key="tip.contextKey">
              <td class="st-cell-key">
                <kbd v-for="k in tip.keys || []" :key="k" class="st-kbd">{{ k }}</kbd>
                <span v-if="!tip.keys || tip.keys.length === 0" class="st-nokey">—</span>
              </td>
              <td class="st-cell-context">{{ t(tip.contextKey) }}</td>
              <td class="st-cell-action">{{ t(tip.actionKey) }}</td>
            </tr>
          </tbody>
        </table>
      </section>
    </div>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Keyboard } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { getAllShortcutTips, SHORTCUT_CONTEXT_ORDER, type ShortcutTipDef } from '@/config/shortcutTips'

defineProps<{ open: boolean }>()
defineEmits(['close'])
const { t } = useI18n()

const all = computed<ShortcutTipDef[]>(() => getAllShortcutTips())
const groups = computed(() =>
  SHORTCUT_CONTEXT_ORDER
    .map((ctx) => ({ context: ctx, tips: all.value.filter((x) => x.context === ctx) }))
    .filter((g) => g.tips.length > 0),
)
const title = computed(() => t('appHeader.shortcutTipsDialog.title', { count: all.value.length }))
</script>

<style scoped>
.st-dialog-empty {
  padding: 32px;
  text-align: center;
  color: var(--text-muted);
}
.st-dialog-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 12px 16px 16px;
}
.st-group-title {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0 0 6px;
  font-size: 13px;
  color: var(--text-primary);
}
.st-group-count {
  color: var(--text-muted);
  font-weight: 500;
}
.st-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.st-table th {
  text-align: left;
  padding: 4px 8px;
  color: var(--text-muted);
  font-weight: 600;
  border-bottom: 1px solid var(--border-color);
  white-space: nowrap;
}
.st-table td {
  padding: 5px 8px;
  border-bottom: 1px solid color-mix(in srgb, var(--border-color) 50%, transparent);
  vertical-align: top;
}
.st-cell-key { white-space: nowrap; }
.st-cell-context { color: var(--text-secondary); white-space: nowrap; }
.st-cell-action { color: var(--text-primary); }
.st-kbd {
  display: inline-block;
  margin: 0 2px;
  padding: 1px 5px;
  border: 1px solid var(--border-color);
  border-bottom-width: 2px;
  border-radius: 4px;
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: 11px;
  font-weight: 600;
  font-family: var(--font-mono, monospace);
  white-space: nowrap;
}
.st-nokey {
  color: var(--text-muted);
}
</style>
