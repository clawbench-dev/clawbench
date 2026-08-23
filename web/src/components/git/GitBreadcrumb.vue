<template>
  <div class="git-breadcrumb">
    <!-- Root crumb: 提交列表 or 文件历史 -->
    <span
      class="git-crumb"
      :class="{ current: currentView === 'commits' }"
      @click="currentView !== 'commits' && $emit('navigate', 'commits')"
    >{{ mode === 'file' ? t('git.breadcrumb.fileHistory') : t('git.breadcrumb.commitList') }}</span>

    <!-- Manage crumb -->
    <template v-if="currentView === 'manage'">
      <span class="git-crumb-sep">›</span>
      <span class="git-crumb current">{{ t('git.manage.title') }}</span>
    </template>

    <!-- Commit crumb (shown when a commit is selected) -->
    <template v-if="selectedCommit">
      <span class="git-crumb-sep">›</span>
      <span
        class="git-crumb"
        :class="{ current: currentView === 'files' || (mode === 'file' && currentView === 'diff') }"
        @click="canNavigateCommit && $emit('navigate', commitTarget)"
      >{{ commitLabel }}</span>
    </template>

    <!-- File crumb (project mode only, in diff view) -->
    <template v-if="selectedFilePath && mode === 'project' && currentView === 'diff'">
      <span class="git-crumb-sep">›</span>
      <span class="git-crumb current">{{ fileName }}</span>
      <button class="git-file-open-btn" @click.stop="$emit('open-file', selectedFilePath)" :title="t('git.breadcrumb.openFile')" v-html="FILE_OPEN_ICON_SVG"></button>
    </template>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { baseName } from '@/utils/path.ts'
import { FILE_OPEN_ICON_SVG } from '@/composables/useFilePathAnnotation.ts'
const { t } = useI18n()

const props = defineProps({
  mode: { type: String, default: 'project' },
  currentView: { type: String, default: 'commits' },
  selectedCommit: Object,
  selectedFilePath: String,
})

defineEmits(['navigate', 'open-file'])

const commitLabel = computed(() => {
  if (!props.selectedCommit) return ''
  if (props.selectedCommit.isWT) return t('git.breadcrumb.workingTree')
  return props.selectedCommit.sha.slice(0, 7)
})

const fileName = computed(() => {
  if (!props.selectedFilePath) return ''
  return baseName(props.selectedFilePath)
})

// In project mode: can navigate back to files from diff
// In file mode: can navigate back to commits from diff
const canNavigateCommit = computed(() => {
  if (props.mode === 'project') return props.currentView === 'diff'
  return props.currentView === 'diff'
})

const commitTarget = computed(() => {
  return props.mode === 'project' ? 'files' : 'commits'
})
</script>

<style scoped>
.git-breadcrumb {
  display: flex;
  align-items: center;
  gap: 4px;
  overflow-x: auto;
  font-size: 13px;
  color: var(--text-muted, #999);
  scrollbar-width: none;
  flex: 1;
  min-width: 0;
}
.git-breadcrumb::-webkit-scrollbar {
  display: none;
}
.git-crumb {
  padding: 3px 6px;
  border-radius: 4px;
  cursor: pointer;
  white-space: nowrap;
  transition: background 0.15s;
}
.git-crumb.current {
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
  cursor: default;
}
@media (hover: hover) {
  .git-crumb:hover {
    background: var(--bg-secondary, #e0e0e0);
    color: var(--accent-color, #4a90d9);
  }
  .git-crumb.current:hover {
    background: none;
    color: var(--text-primary, #1a1a1a);
  }
}
.git-crumb-sep {
  color: var(--text-muted, #999);
  font-size: 11px;
}
.git-file-open-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  padding: 0;
  line-height: 1;
  opacity: 0.5;
  transition: opacity 0.15s, color 0.15s, background 0.15s;
  outline: none;
  flex-shrink: 0;
}
@media (hover: hover) {
  .git-file-open-btn:hover {
    opacity: 1;
    color: var(--accent-color, #4a90d9);
    background: var(--bg-secondary, #e0e0e0);
  }
}
.git-file-open-btn svg {
  display: block;
}
</style>
