<script setup lang="ts">
import { ref, computed, watchEffect } from 'vue'
import { getFileIconUrl, getFolderIconUrl } from '@/utils/materialIcons'

const props = withDefaults(defineProps<{
  /** File path or file name */
  path: string
  /**
   * Icon size: a number of CSS px, or any CSS length (a `var(--token)` string).
   * The length form exists so the empty-state glyph can be driven by
   * `--empty-icon-size` instead of a second copy of the number.
   */
  size?: number | string
  /** Whether this is a directory */
  isDir?: boolean
  /** Whether the directory is open (expanded) */
  isDirOpen?: boolean
}>(), {
  size: 16,
  isDir: false,
  isDirOpen: false,
})

const iconSrc = ref('')

watchEffect(async () => {
  const currentPath = props.path
  const currentIsDir = props.isDir
  const currentIsDirOpen = props.isDirOpen
  const url = currentIsDir
    ? await getFolderIconUrl(currentPath, currentIsDirOpen)
    : await getFileIconUrl(currentPath)
  // Only update if props haven't changed while we were awaiting
  if (props.path === currentPath && props.isDir === currentIsDir) {
    iconSrc.value = url
  }
})

const sizeStyle = computed(() => {
  const value = typeof props.size === 'number' ? `${props.size}px` : props.size
  return { width: value, height: value }
})

const alt = computed(() => props.isDir ? `folder: ${props.path}` : `file: ${props.path}`)
</script>

<template>
  <img
    v-if="iconSrc"
    :src="iconSrc"
    :alt="alt"
    class="file-type-icon"
    :style="sizeStyle"
    draggable="false"
  />
</template>

<style scoped>
.file-type-icon {
  display: inline-block;
  object-fit: contain;
  vertical-align: middle;
  flex-shrink: 0;
}
</style>
