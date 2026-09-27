<template>
  <!-- The single "this media could not be loaded" element.
       Rendered by every media surface (image / video / audio viewer, preview
       card, lightbox) AND mounted imperatively by localMediaFallback.ts for
       v-html content (chat messages, share pages), so a failed local file
       reads the same everywhere. All styling lives in the global
       media-block.css (`.media-load-error*`) — a scoped block would not apply
       to the imperative mount, which renders outside any component tree. -->
  <div
    class="media-load-error"
    :class="{ 'media-load-error--fill': fill }"
    role="img"
    :title="title"
    :aria-label="title"
  >
    <span class="media-load-error-icon" aria-hidden="true">
      <component :is="icon" :size="18" />
    </span>
    <span class="media-load-error-text">{{ message }}</span>
    <span v-if="name" class="media-load-error-name">{{ name }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { ImageOff, VideoOff, AudioLines, FileWarning } from 'lucide-vue-next'
import { gt } from '@/composables/useLocale'
import i18n from '@/i18n'

/**
 * Localized through `gt()` rather than `useI18n()`.
 *
 * This component is mounted both inside the Vue app and, by
 * localMediaFallback.ts, with a bare `h()` + `render()` into a detached host
 * for v-html content. There is no app instance in the second case, and
 * `useI18n()` throws there ("Cannot read properties of null (reading
 * '__VUE_I18N_SYMBOL__')"). `gt()` resolves against the global instance, so one
 * implementation serves both — the same reason mediaBlockFactory.ts uses it.
 *
 * Reading `i18n.global.locale` keeps the label reactive: switching language
 * re-renders the message without a reload.
 */
const props = withDefaults(defineProps<{
  /** Media kind — selects the icon. */
  kind?: 'image' | 'video' | 'audio' | 'file'
  /** File name / path, shown under the message when known. */
  name?: string
  /** Override the default localized message. */
  message?: string
  /** Stretch to fill a pane that has no other content (viewer / preview card). */
  fill?: boolean
}>(), {
  kind: 'image',
  name: '',
  message: '',
  fill: false,
})

const ICONS = {
  image: ImageOff,
  video: VideoOff,
  audio: AudioLines,
  file: FileWarning,
} as const

const icon = computed(() => ICONS[props.kind] ?? ICONS.file)

const message = computed(() => {
  // Touch the locale so a language switch re-resolves the label.
  void i18n.global.locale.value
  return props.message || gt('media.loadFailed')
})

/** Names the file in the tooltip, which the truncated name line may hide. */
const title = computed(() => (props.name ? `${message.value}: ${props.name}` : message.value))
</script>
