<template>
  <div class="capture-sources" :class="{ 'capture-sources--compact': compact }">
    <button
      v-for="src in sources"
      :key="src.id"
      class="capture-source"
      :class="{ 'capture-source--on': src.id === selectedId }"
      type="button"
      :disabled="disabled"
      :data-source="src.id"
      @click="emit('select', src.id)"
    >
      <!-- The still frame is the only thing that tells two windows of the same
           app apart, so a host that can supply one always gets it shown. A host
           that cannot (Wayland placeholders, no icon) keeps the tile's shape
           with an icon instead. -->
      <span class="capture-source__thumb">
        <img v-if="src.thumbnail" :src="src.thumbnail" :alt="src.name" />
        <Monitor v-else :size="20" />
      </span>
      <span class="capture-source__name" :title="src.name">{{ src.name }}</span>
      <span class="capture-source__note">
        {{ src.kind === 'window' ? t('meeting.create.window') : t('meeting.create.screen') }}
      </span>
    </button>
  </div>
</template>

<script setup lang="ts">
/**
 * The list of screens and windows a host can share.
 *
 * One component because the SAME choice is asked twice — when creating a
 * meeting and when swapping the source mid-meeting — and the two copies had
 * already drifted: the create page showed a still frame per source, the swap
 * list showed bare names, so the later pick was made blind.
 */
import { Monitor } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import type { CaptureSource } from '@/composables/useLiveShare'

withDefaults(
  defineProps<{
    sources: CaptureSource[]
    /** Empty means nothing is selected (yet). */
    selectedId: string
    /** Tiles are not clickable while a switch is in flight. */
    disabled?: boolean
    /** Narrow two-column layout for the meeting panel. */
    compact?: boolean
  }>(),
  { disabled: false, compact: false },
)

const emit = defineEmits<{ select: [id: string] }>()

const { t } = useI18n()
</script>

<style scoped>
.capture-sources {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(calc(150 * var(--dp)), 1fr));
  gap: var(--space-4);
  padding: 2px;
}

/* The meeting panel is ~280dp wide: two columns, and the list scrolls so a
   machine with a dozen windows does not push the rest of the form off. */
.capture-sources--compact {
  grid-template-columns: repeat(auto-fill, minmax(calc(120 * var(--dp)), 1fr));
  max-height: calc(240 * var(--dp));
  overflow-y: auto;
}

.capture-source {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-3);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  text-align: left;
  cursor: pointer;
  min-width: 0;
}

.capture-source:hover {
  border-color: var(--accent-color);
}

.capture-source:disabled {
  cursor: default;
  opacity: 0.6;
}

.capture-source--on {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 1px var(--accent-color);
}

.capture-source__thumb {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  aspect-ratio: 16 / 9;
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  color: var(--text-hint);
  overflow: hidden;
}

.capture-source__thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.capture-source__name {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.capture-source__note {
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
}
</style>
