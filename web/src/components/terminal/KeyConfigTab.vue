<template>
  <div class="kcf-content">
    <!-- Selected area -->
    <div class="kcf-selected">
      <div class="kcf-selected-header">
        <span class="kcf-section-title">{{ t('terminal.keyConfigSelected') }}</span>
        <span class="kcf-count">{{ localSelected.length }}</span>
        <div class="kcf-selected-actions">
          <button class="kcf-action-btn" @click="resetToDefault">{{ t('terminal.keyConfigReset') }}</button>
          <button class="kcf-action-btn kcf-action-btn-danger" @click="clearAll">{{ t('terminal.keyConfigClear') }}</button>
        </div>
      </div>
      <div v-if="localSelected.length > 0" class="kcf-selected-grid">
        <draggable v-model="localSelected" item-key="id" class="kcf-draggable" :animation="200" ghost-class="kcf-ghost" chosen-class="kcf-chosen" drag-class="kcf-drag" @end="onDragEnd">
          <template #item="{ element, index }">
            <button
              class="kcf-chip kcf-chip-selected"
            >
              <span class="kcf-chip-label">{{ element.label }}</span>
            </button>
          </template>
        </draggable>
      </div>
      <div v-else class="kcf-empty-hint">{{ t('terminal.keyConfigEmpty') }}</div>
    </div>

    <!-- Divider -->
    <div class="kcf-divider" />

    <!-- Available area -->
    <div class="kcf-available">
      <div class="kcf-section-title">{{ t('terminal.keyConfigAvailable') }}</div>
      <div v-for="group in groups" :key="group.key" class="kcf-group">
        <div class="kcf-group-title">{{ t(group.label) }}</div>
        <div class="kcf-group-grid">
          <button
            v-for="def in getGroupDefs(group.key)"
            :key="def.id"
            class="kcf-chip"
            :class="{ 'kcf-chip-active': isSelected(def.id) }"
            @click="toggleSelect(def.id)"
          >
            <span class="kcf-chip-label">{{ def.label }}</span>
            <span v-if="isSelected(def.id)" class="kcf-check">&#10003;</span>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import draggable from 'vuedraggable'
import { getDef, getAllDefs, getGroups, getDefaultIds, type KeyDef, type ConfigType } from '@/utils/terminalKeyDefs'

const props = defineProps<{
  type: ConfigType
  selectedIds: string[]
}>()

const { t } = useI18n()
const groups = getGroups(props.type)
const allDefs = getAllDefs(props.type)

const localSelected = ref<KeyDef[]>([])

function syncFromProps() {
  localSelected.value = props.selectedIds
    .map(id => getDef(props.type, id))
    .filter((d): d is KeyDef => d !== undefined)
}

watch(() => props.selectedIds, syncFromProps, { immediate: true })

function isSelected(id: string): boolean {
  return localSelected.value.some(d => d.id === id)
}

function toggleSelect(id: string) {
  if (isSelected(id)) {
    localSelected.value = localSelected.value.filter(d => d.id !== id)
  } else {
    const def = getDef(props.type, id)
    if (def) localSelected.value.push(def)
  }
}

function onDragEnd() {
  // localSelected is already updated by vuedraggable v-model
}

function resetToDefault() {
  const defaultIds = getDefaultIds(props.type)
  localSelected.value = defaultIds
    .map(id => getDef(props.type, id))
    .filter((d): d is KeyDef => d !== undefined)
}

function clearAll() {
  localSelected.value = []
}

function getGroupDefs(groupKey: string): KeyDef[] {
  return allDefs.filter(d => d.group === groupKey)
}

function getSelectedIds(): string[] {
  return localSelected.value.map(d => d.id)
}

defineExpose({ getSelectedIds })
</script>

<style scoped>
.kcf-content {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.kcf-selected {
  flex-shrink: 0;
  padding: 8px 12px;
}

.kcf-selected-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.kcf-selected-actions {
  margin-left: auto;
  display: flex;
  gap: 6px;
}

.kcf-action-btn {
  font-size: 12px;
  color: var(--accent, #4f8ef7);
  background: none;
  border: none;
  cursor: pointer;
  padding: 2px 6px;
  border-radius: var(--radius-sm, 6px);
  transition: background 0.15s;
  font-family: inherit;
}

.kcf-action-btn:active {
  background: var(--bg-tertiary, #eee);
}

.kcf-action-btn-danger {
  color: var(--danger, #e74c3c);
}

.kcf-section-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-muted, #999);
}

.kcf-count {
  font-size: 12px;
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #eee);
  border-radius: 10px;
  padding: 0 6px;
  min-width: 18px;
  text-align: center;
}

.kcf-selected-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.kcf-draggable {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.kcf-empty-hint {
  font-size: 13px;
  color: var(--text-muted, #999);
  text-align: center;
  padding: 16px 0;
}

.kcf-divider {
  height: 1px;
  background: var(--border-color, #e5e5e5);
  margin: 0 12px;
  flex-shrink: 0;
}

.kcf-available {
  flex: 1;
  overflow-y: auto;
  padding: 8px 12px;
  -webkit-overflow-scrolling: touch;
}

.kcf-group {
  margin-bottom: 12px;
}

.kcf-group-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted, #999);
  margin-bottom: 6px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.kcf-group-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.kcf-chip {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 36px;
  min-width: 36px;
  padding: 0 10px;
  border: 1px solid var(--border-color, #e0e0e0);
  border-radius: var(--radius-sm, 6px);
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  font-size: 13px;
  font-family: inherit;
  cursor: pointer;
  user-select: none;
  -webkit-tap-highlight-color: transparent;
  transition: background 0.15s, border-color 0.15s, opacity 0.15s;
}

.kcf-chip:active {
  opacity: 0.7;
}

.kcf-chip-active {
  border-color: var(--accent, #4f8ef7);
  background: var(--accent-bg, rgba(79, 142, 247, 0.1));
}

.kcf-chip-selected {
  border-color: var(--accent, #4f8ef7);
  background: var(--accent-bg, rgba(79, 142, 247, 0.1));
}

.kcf-chip-label {
  line-height: 1;
}

.kcf-check {
  position: absolute;
  top: 2px;
  right: 3px;
  font-size: 9px;
  color: var(--accent, #4f8ef7);
  line-height: 1;
}

/* Drag animation states */
.kcf-ghost {
  opacity: 0.3;
}

.kcf-chosen {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
  transform: scale(1.05);
  z-index: 1;
}

.kcf-drag {
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
  transform: scale(1.08);
  z-index: 10;
  opacity: 0.9;
}
</style>
