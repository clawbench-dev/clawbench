<template>
  <div class="git-branch-list" :class="{ collapsed, 'no-header': hideHeader }">
    <div v-if="!hideHeader" class="section-header" @click="toggleCollapse">
      <div class="section-left">
        <span class="section-title">{{ t('git.manage.branches') }}</span>
        <span v-if="branches.length > 0" class="section-count">{{ branches.length }}</span>
        <span v-if="stashCount > 0" class="stash-badge">📦 {{ stashCount }}</span>
      </div>
      <ChevronDown v-if="!collapsed" :size="16" class="section-chevron" />
      <ChevronRight v-else :size="16" class="section-chevron" />
    </div>
    <div v-if="hideHeader || !collapsed" class="section-body">
      <div v-if="loading" class="section-loading">
        <div class="spinner" style="width:18px;height:18px;border-width:2px;" />
      </div>
      <div v-else-if="error" class="section-error">
        <span>{{ t('git.manage.loadError') }}</span>
        <button class="retry-btn" @click="$emit('retry')">{{ t('git.manage.retry') }}</button>
      </div>
      <div v-else-if="branches.length === 0" class="section-empty">{{ t('git.manage.noBranches') }}</div>
      <template v-else>
        <GitBranchRow
          v-for="b in sortedBranches"
          :key="b.name"
          :branch="b"
          :disabled="checkoutInProgress"
          @switch="$emit('switch-branch', $event)"
          @delete="$emit('delete-branch', $event)"
        />
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ChevronRight } from 'lucide-vue-next'
import GitBranchRow from './GitBranchRow.vue'

const { t } = useI18n()

type BranchItem = Record<string, unknown> & { name: string }

const props = withDefaults(defineProps<{
  branches: BranchItem[]
  stashCount?: number
  loading?: boolean
  error?: boolean
  checkoutInProgress?: boolean
  initialCollapsed?: boolean
  hideHeader?: boolean
}>(), {
  branches: () => [],
  stashCount: 0,
  loading: false,
  error: false,
  checkoutInProgress: false,
  initialCollapsed: false,
  hideHeader: false,
})

defineEmits(['switch-branch', 'delete-branch', 'retry'])

const STORAGE_KEY = 'git-branch-collapsed'
const collapsed = ref(false)

onMounted(() => {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored !== null) {
    collapsed.value = stored === 'true'
  } else {
    collapsed.value = props.initialCollapsed
  }
})

function toggleCollapse() {
  collapsed.value = !collapsed.value
  localStorage.setItem(STORAGE_KEY, String(collapsed.value))
}

const sortedBranches = computed(() => {
  return [...props.branches].sort((a, b) => {
    if (a.isDefault && !b.isDefault) return -1
    if (!a.isDefault && b.isDefault) return 1
    if (a.isCurrent && !b.isCurrent) return -1
    if (!a.isCurrent && b.isCurrent) return 1
    return a.name.localeCompare(b.name)
  })
})
</script>

<style scoped>
.git-branch-list {
  flex: 0 1 auto;
  min-height: 0;
  overflow: hidden;
  border-bottom: 1px solid var(--border-color, #dee2e6);
}

.git-branch-list.no-header {
  border-bottom: none;
  flex: 1;
  overflow-y: auto;
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  cursor: pointer;
  transition: background 0.15s;
}

@media (hover: hover) {
  .section-header:hover {
    background: var(--bg-secondary, #f8f9fa);
  }
}

.section-left {
  display: flex;
  align-items: center;
  gap: 6px;
}

.section-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
}

.section-count {
  font-size: 10px;
  font-weight: 700;
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-muted, #999);
  padding: 1px 6px;
  border-radius: 10px;
}

.stash-badge {
  font-size: 11px;
  color: var(--text-muted, #999);
}

.section-chevron {
  color: var(--text-muted, #999);
  flex-shrink: 0;
}

.section-body {
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
}

.section-loading {
  display: flex;
  justify-content: center;
  padding: 16px 0;
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
  padding: 8px 12px;
}

.spinner {
  border: 2px solid var(--border-color, #dee2e6);
  border-top-color: var(--accent-color, #4a90d9);
  border-radius: 50%;
  animation: spin 0.6s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
