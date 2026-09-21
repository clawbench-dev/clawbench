<template>
  <span class="external-badge" :title="title">
    <FolderOpen v-if="kind === 'dir'" :size="11" />
    <FileText v-else :size="11" />
    <span class="external-badge-label">{{ label }}</span>
  </span>
</template>

<script setup>
import { computed } from 'vue'
import { FolderOpen, FileText } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

/**
 * Marks content that lives OUTSIDE the project root.
 *
 * The file manager can browse any directory the server exposes, and the viewer
 * can open any file a chat annotation names — so "is this inside my project?"
 * stops being implicit. This badge is the single place that answers it, shared
 * by the browse list, the directory preview, the file preview and the directory
 * breadcrumb bar so the four surfaces cannot drift into four different looks.
 *
 * Orange is the colour already used for project-external paths in chat
 * annotations and the code viewer (see annotation-buttons.css).
 */
const props = defineProps({
  kind: { type: String, default: 'file' },
})

const { t } = useI18n()
const label = computed(() => (props.kind === 'dir' ? t('file.nav.externalDir') : t('file.nav.externalFile')))
const title = computed(() => label.value)
</script>

<style scoped>
.external-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: 1px var(--space-3);
  border-radius: var(--radius-full);
  border: 1px solid color-mix(in srgb, var(--color-orange, #d9730d) 45%, transparent);
  background: color-mix(in srgb, var(--color-orange, #d9730d) 12%, transparent);
  color: var(--color-orange, #d9730d);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-medium);
  line-height: 1.5;
  white-space: nowrap;
  flex-shrink: 0;
  user-select: none;
}

.external-badge-label {
  /* The icon alone reads as a decoration; the label is what actually says
     "outside the project", so it must survive truncation of the host row. */
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
