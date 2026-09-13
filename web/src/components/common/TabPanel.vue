<template>
  <div
    v-if="everOpened"
    v-show="isActive"
    class="tab-panel"
    :class="{ 'tab-panel-active': isActive }"
  >
    <!-- Header -->
    <div v-if="!noHeader" class="bs-header" @click="handleHeaderClick">
      <slot name="header">
        <span class="bs-title">{{ title }}</span>
      </slot>
    </div>
    <!-- Body -->
    <div class="bs-body">
      <slot />
    </div>
    <!-- Footer slot -->
    <footer v-if="$slots.footer" class="bs-footer">
      <slot name="footer" />
    </footer>
  </div>
</template>

<script setup>
import { ref, watch, computed } from 'vue'

const props = defineProps({
  tabId: {
    type: String,
    required: true,
  },
  activeTab: {
    type: String,
    required: true,
  },
  title: {
    type: String,
    default: '',
  },
  noHeader: Boolean,
})

const emit = defineEmits(['header-click'])

const isActive = computed(() => props.activeTab === props.tabId)
const everOpened = ref(false)

watch(isActive, (val) => {
  if (val) {
    everOpened.value = true
  }
}, { immediate: true })

function handleHeaderClick() {
  emit('header-click')
}
</script>

<style>
.tab-panel {
  position: absolute;
  inset: 0;
  background: var(--bg-secondary, #fff);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  opacity: 0;
  transition: opacity var(--duration-base) ease;
  pointer-events: none;
  /* Create a stacking context so child z-indexes (e.g. .file-overlay's
     z-index:100) stay contained inside this panel instead of escaping to
     cover sibling panes — e.g. the split divider (z-index:2) next to the
     left column. z-index:auto on a positioned element does NOT create a
     stacking context, which is why the overlay escaped before. */
  isolation: isolate;
}

.tab-panel-active {
  opacity: 1;
  pointer-events: auto;
}

/* Tab panels sit directly below AppHeader which already has a border-bottom;
   no need for another border on the tab header — child components like
   .dir-nav provide their own dividers where needed.
   Also hide the drag handle — tab headers are not bottom sheets. */
.tab-panel > .bs-header {
  border-bottom: none;
  box-shadow: none;
}

.tab-panel > .bs-header > .bs-handle {
  display: none;
}
</style>
