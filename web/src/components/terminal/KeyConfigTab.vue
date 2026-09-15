<template>
  <div class="kcf-content">
    <!-- Selected area -->
    <div class="kcf-selected">
      <div class="kcf-selected-header">
        <span class="kcf-section-title">{{ t('terminal.keyConfigSelected') }}</span>
        <span class="kcf-count count-badge count-badge--md">{{ localSelected.length }}</span>
        <div class="kcf-selected-actions">
          <button class="kcf-action-btn" @click="resetToDefault">{{ t('terminal.keyConfigReset') }}</button>
          <button class="kcf-action-btn kcf-action-btn-danger" @click="clearAll">{{ t('terminal.keyConfigClear') }}</button>
        </div>
      </div>
      <div v-if="localSelected.length > 0" class="kcf-selected-grid">
        <VueDraggable v-model="localSelected" class="kcf-draggable" :animation="200" ghost-class="kcf-ghost" chosen-class="kcf-chosen" drag-class="kcf-drag" @end="onDragEnd">
            <button
              v-for="element in localSelected"
              :key="element.id"
              class="kcf-chip kcf-chip-selected"
            >
              <span class="kcf-chip-label">{{ element.label }}</span>
            </button>
        </VueDraggable>
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
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
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
  // localSelected is already updated by VueDraggable v-model
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
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.kcf-selected {
  flex-shrink: 0;
  padding: var(--space-4) var(--space-6);
}

.kcf-selected-header {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
}

.kcf-selected-actions {
  margin-left: auto;
  display: flex;
  gap: var(--space-3);
}

.kcf-action-btn {
  font-size: var(--font-size-sm);
  color: var(--accent-color);
  background: none;
  border: none;
  cursor: pointer;
  padding: var(--space-1) var(--space-3);
  border-radius: 0;
  transition: background var(--duration-base);
  font-family: inherit;
}

.kcf-action-btn:active {
  background: var(--bg-tertiary, #eee);
}

.kcf-action-btn-danger {
  color: var(--color-red);
}

.kcf-section-title {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-muted, #999);
}

.kcf-count {
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #eee);
}

.kcf-selected-grid {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}

.kcf-draggable {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}

.kcf-empty-hint {
  font-size: var(--font-size-md);
  color: var(--text-muted, #999);
  text-align: center;
  padding: var(--space-7) 0;
}

.kcf-divider {
  height: 1px;
  background: var(--border-color, #e5e5e5);
  margin:0 var(--space-6);
  flex-shrink: 0;
}

.kcf-available {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-4) var(--space-6);
  -webkit-overflow-scrolling: touch;
}

.kcf-group {
  margin-bottom: var(--space-6);
}

.kcf-group-title {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-muted, #999);
  margin-bottom: var(--space-3);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.kcf-group-grid {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}

.kcf-chip {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 36px;
  min-width: 36px;
  padding:0 var(--space-5);
  border: 1px solid var(--border-color, #e0e0e0);
  border-radius: 0;
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  font-size: var(--font-size-md);
  font-family: inherit;
  cursor: pointer;
  user-select: none;
  -webkit-tap-highlight-color: transparent;
  transition: background var(--duration-base), border-color var(--duration-base), opacity var(--duration-base);
}

.kcf-chip:active {
  opacity: var(--opacity-soft);
}

.kcf-chip-active {
  border-color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
}

.kcf-chip-selected {
  border-color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
}

.kcf-chip-label {
  line-height: 1;
}

/* Drag animation states */
.kcf-ghost {
  opacity: var(--opacity-disabled);
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
  opacity: var(--opacity-hover);
}
</style>
