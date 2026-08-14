<template>
  <LoadingIndicator :label="label" size="md" class="async-component-loader" :class="{ minimal }">
    <slot />
  </LoadingIndicator>
</template>

<script setup lang="ts">
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'

defineProps<{
  label?: string
  minimal?: boolean
}>()
</script>

<style scoped>
.async-component-loader {
  height: 100%;
  min-height: 120px;
  padding: 32px;
}

.async-component-loader.minimal {
  min-height: 0;
  padding: 16px;
}
</style>
