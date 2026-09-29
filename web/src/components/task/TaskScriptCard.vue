<template>
  <div class="overview-card">
    <h3 class="card-title is-collapsible" @click="collapsed = !collapsed">
      <Terminal class="card-icon" :size="14" />
      <span class="card-title-text">{{ t('task.form.script') }}</span>
      <button class="card-toggle-btn" :title="collapsed ? t('task.overview.showScript') : t('task.overview.hideScript')">
        <ChevronDown :size="14" :class="{ 'is-collapsed': collapsed }" class="card-chevron" />
      </button>
    </h3>
    <!-- v-show keeps the CodeMirror instance alive across collapse/expand:
         re-mounting it on every toggle would rebuild the editor state and lose
         the scroll position. It is cheap enough (one read-only view) that the
         always-mounted cost is worth the stable toggle. -->
    <div v-show="!collapsed" class="script-body">
      <TaskScriptEditor :model-value="script" language="shell" :disabled="true" />
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ChevronDown, Terminal } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import TaskScriptEditor from '@/components/task/TaskScriptEditor.vue'
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
