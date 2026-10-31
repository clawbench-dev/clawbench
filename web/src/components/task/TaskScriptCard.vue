<template>
  <div class="overview-card">
    <h3 class="card-title is-collapsible" @click="collapsed = !collapsed">
      <Terminal class="card-icon" :size="14" />
      <span class="card-title-text">{{ t('task.form.script') }}</span>
      <button class="card-toggle-btn" :title="collapsed ? t('task.overview.showScript') : t('task.overview.hideScript')">
        <ChevronDown :size="14" :class="{ 'is-collapsed': collapsed }" class="card-chevron" />
      </button>
    </h3>
    <!-- The editor is async on purpose: CodeMirror + Lezer are ~1MB, and a
         static import here put them in the FIRST-SCREEN bundle (this card lives
         in the always-loaded task page). The chunk now loads when a task detail
         actually renders. v-show still keeps the instance alive across
         collapse/expand once it exists. -->
    <div v-show="!collapsed" class="script-body">
      <TaskScriptEditor :model-value="script" language="shell" :disabled="true" />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, defineAsyncComponent } from 'vue'
import { ChevronDown, Terminal } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

// Plain defineAsyncComponent (no loader/error chrome): this is a read-only
// preview inside an already-collapsed card, so a failed chunk load should just
// leave the section empty instead of rendering an error card.
const TaskScriptEditor = defineAsyncComponent(
  async () => (await import('@/components/task/TaskScriptEditor.vue')).default,
)

import '@/assets/task-overview-card.css'

const { t } = useI18n()

const props = defineProps({
  task: { type: Object, required: true },
})

const script = computed(() => props.task?.script || '')

// Default collapsed, per the task detail design: the script is reference
// material, not the thing the user came to read.
const collapsed = ref(true)
</script>

<style scoped>
/* Card chrome (.overview-card / .card-title / .card-toggle-btn / .card-chevron)
   is global — see assets/task-overview-card.css. Only this card's own body
   spacing lives here. */
.script-body {
  padding-top: var(--space-3);
  min-width: 0;
}
</style>
