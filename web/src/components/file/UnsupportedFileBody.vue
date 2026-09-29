<template>
  <!-- Centered "this file has no inline preview" placeholder: type icon, file
       name, reason, and the caller's action buttons.

       Shared by the full-screen file viewer and the quick-preview pane so the
       two surfaces cannot drift — a binary file must look the same whether it
       was opened from the file manager or from a path annotation.

       Fills its pane: it is dropped straight into the viewer's content area or
       the preview card's body slot, both of which are flex containers. -->
  <div class="unsupported-file">
    <FileIcon :path="path || name" :size="48" />
    <div class="unsupported-title">{{ name }}</div>
    <div class="unsupported-desc">
      {{ description }} {{ size ? '(' + formatFileSize(size) + ')' : '' }}
    </div>
    <div class="unsupported-actions">
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
import FileIcon from '@/components/common/FileIcon.vue'
import { formatFileSize } from '@/utils/fileType.ts'

defineProps<{
  /** File name, shown as the heading. */
  name: string
  /** Full path — drives the type icon (falls back to `name`). */
  path?: string
  /** Byte size; appended to the reason in parentheses when set. */
  size?: number | null
  /** Why the file cannot be previewed (already localized by the caller). */
  description: string
}>()
</script>

<style scoped>
/* Metrics copied verbatim from the file viewer's original placeholder so the
   two surfaces render identically. */
.unsupported-file {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  /* Grow to the pane when the host is a flex container (the preview card body
     and the viewer's content area both are); fall back to a content-sized box
     otherwise so nothing collapses. */
  flex: 1;
  min-height: 0;
  padding: 48px 24px;
  text-align: center;
  overflow: auto;
}

.unsupported-file > :deep(img.file-type-icon) {
  width: 48px;
  height: 48px;
  margin-bottom: var(--space-6);
}

.unsupported-title {
  font-size: var(--font-size-2xl);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
  margin-bottom: var(--space-4);
  word-break: break-all;
}

.unsupported-desc {
  font-size: var(--font-size-lg);
  color: var(--text-muted);
  margin-bottom: var(--space-8);
}

.unsupported-actions {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-5);
}
</style>
