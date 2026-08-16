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
        @click="$emit('switch-tag', tag)"
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

const { t } = useI18n()

const props = defineProps<{
  tags: Array<Record<string, unknown> & { name: string; msg?: string; date?: string }>
  loading?: boolean
  error?: boolean
}>()

defineEmits(['retry', 'switch-tag', 'delete-tag'])

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
  padding: 8px 12px;
  font-size: 13px;
  color: var(--danger-color, #dc3545);
}

.retry-btn {
  font-size: 12px;
  padding: 3px 10px;
  border: 1px solid var(--accent-color, #4a90d9);
  border-radius: 4px;
  background: transparent;
  color: var(--accent-color, #4a90d9);
  cursor: pointer;
}

.section-empty {
  font-size: 13px;
  color: var(--text-muted, #999);
  padding: 24px 12px;
  text-align: center;
}

.tag-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-color, #dee2e6);
  cursor: pointer;
  transition: background 0.15s;
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
  border-radius: 6px;
  transition: background 0.15s, color 0.15s;
}

@media (hover: hover) {
  .tag-action-btn:hover {
    color: var(--danger-color, #dc3545);
    background: var(--danger-bg, rgba(220, 53, 69, 0.1));
  }
}

.tag-action-btn:active {
  background: var(--danger-bg, rgba(220, 53, 69, 0.15));
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
  gap: 6px;
}

.tag-icon {
  color: var(--accent-color, #4a90d9);
  flex-shrink: 0;
}

.tag-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tag-msg {
  font-size: 12px;
  color: var(--text-secondary, #666);
  margin-top: 2px;
  margin-left: 20px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tag-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 2px;
  margin-left: 20px;
}

.tag-date {
  font-size: 11px;
  color: var(--text-muted, #999);
}

</style>
