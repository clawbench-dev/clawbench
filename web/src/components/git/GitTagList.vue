<template>
  <div class="git-tag-list">
    <div v-if="loading" class="section-loading">
      <LoadingIndicator size="sm" inline />
    </div>
    <div v-else-if="error" class="section-error">
      <span>{{ t('git.manage.loadError') }}</span>
      <button class="retry-btn" @click="$emit('retry')">{{ t('git.manage.retry') }}</button>
    </div>
    <div v-else-if="tags.length === 0" class="section-empty">{{ t('git.manage.noTags') }}</div>
    <template v-else>
      <div
        v-for="tag in sortedTags"
        :key="tag.name"
        class="tag-row"
        @click="handleRowClick(tag)"
      >
        <div class="tag-info">
          <div class="tag-main">
            <Tag :size="14" class="tag-icon" />
            <span class="tag-name">{{ tag.name }}</span>
          </div>
          <div v-if="tag.msg" class="tag-msg" :title="tag.msg">{{ tag.msg }}</div>
          <div class="tag-meta">
            <span v-if="tag.date" class="tag-date">{{ shortDate(tag.date) }}</span>
          </div>
        </div>
        <button
          class="tag-action-btn"
          :title="t('git.manage.deleteTag')"
          @click.stop="$emit('delete-tag', tag)"
        >
          <Trash2 :size="15" />
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Tag, Trash2 } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { hasActiveTextSelection } from '@/utils/textSelection'

const { t } = useI18n()

const props = defineProps<{
  tags: Array<Record<string, unknown> & { name: string; msg?: string; date?: string }>
  loading?: boolean
  error?: boolean
}>()

const emit = defineEmits(['retry', 'switch-tag', 'delete-tag'])

/**
 * A drag-select inside the row ends with a click on the row (mousedown and
 * mouseup share it as common ancestor); that must not be treated as a switch.
 */
function handleRowClick(tag: Record<string, unknown>) {
  if (hasActiveTextSelection()) return
  emit('switch-tag', tag)
}

// Most recent tags first
const sortedTags = computed(() =>
  [...props.tags].sort((a, b) => {
    const da = a.date ? new Date(a.date).getTime() : 0
    const db = b.date ? new Date(b.date).getTime() : 0
    return db - da
  }),
)

function shortDate(dateStr: string) {
  if (!dateStr) return ''
  // ISO date format: "2025-01-15 10:30:00 +0800" -> "2025-01-15"
  const parts = dateStr.split(' ')
  if (parts.length > 0) return parts[0]
  return dateStr
}
</script>

<style scoped>
.git-tag-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
}

.section-loading {
  display: flex;
  justify-content: center;
  padding: 24px 0;
}

.section-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-4) var(--space-6);
  font-size: var(--font-size-md);
  color: var(--color-red);
}

.retry-btn {
  font-size: var(--font-size-sm);
  padding:3px var(--space-5);
  border: 1px solid var(--accent-color, #4a90d9);
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--accent-color, #4a90d9);
  cursor: pointer;
}

.section-empty {
  font-size: var(--font-size-md);
  color: var(--text-muted, #999);
  padding:24px var(--space-6);
  text-align: center;
}

.tag-row {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  padding: var(--space-5) var(--space-6);
  border-bottom: 1px solid var(--border-color, #dee2e6);
  cursor: pointer;
  transition: background var(--duration-base);
}

.tag-info {
  flex: 1;
  min-width: 0;
}

.tag-action-btn {
  flex-shrink: 0;
  width: 30px;
  height: 30px;
  margin-top: -2px;
  border: none;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-sm);
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .tag-action-btn:hover {
    color: var(--color-red);
    background: color-mix(in srgb, var(--color-red) 10%, transparent);
  }
}

.tag-action-btn:active {
  background: color-mix(in srgb, var(--color-red) 15%, transparent);
}

@media (hover: hover) {
  .tag-row:hover {
    background: var(--bg-secondary, #f8f9fa);
  }
}

.tag-row:active {
  background: var(--bg-tertiary, #e9ecef);
}

.tag-main {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.tag-icon {
  color: var(--accent-color, #4a90d9);
  flex-shrink: 0;
}

.tag-name {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tag-msg {
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #666);
  margin-top: var(--space-1);
  margin-left: var(--space-8);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tag-meta {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  margin-top: var(--space-1);
  margin-left: var(--space-8);
}

.tag-date {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
}

</style>
