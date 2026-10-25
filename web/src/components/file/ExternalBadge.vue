<template>
  <span class="external-badge" :title="title" :aria-label="title" role="img">
    <FolderOpen v-if="kind === 'dir'" :size="11" />
    <FileText v-else :size="11" />
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
 * The badge is ICON-ONLY. A text label — even the single short word it used to
 * carry — still cost a whole word's width inline in dense rows, and the row's
 * own filename is the thing that has to survive: when space runs short the
 * caller shrinks the NAME (see `.file-path-hint` / `.dir-preview-title`), never
 * this badge. The meaning moves to the tooltip (`title`), which also doubles as
 * the accessible name via `aria-label` — an icon alone would be invisible to
 * screen readers.
 *
 * The icon carries the file-vs-directory distinction (the surrounding context
 * already says which, but the badge stays correct standalone).
 *
 * Orange is the colour already used for project-external paths in chat
 * annotations and the code viewer (see annotation-buttons.css).
 */
// `kind` is read in the template (icon choice), so no script-side binding.
defineProps({
  kind: { type: String, default: 'file' },
})

const { t } = useI18n()
const title = computed(() => t('file.nav.externalTip'))
</script>

<style scoped>
.external-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-1);
  border-radius: var(--radius-full);
  border: 1px solid color-mix(in srgb, var(--color-orange, #d9730d) 45%, transparent);
  background: color-mix(in srgb, var(--color-orange, #d9730d) 12%, transparent);
  color: var(--color-orange, #d9730d);
  /* Never squeezed or clipped: the sibling name yields first. */
  flex-shrink: 0;
  user-select: none;
}
</style>
