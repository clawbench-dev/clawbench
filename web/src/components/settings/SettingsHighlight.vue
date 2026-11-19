<template>
  <!-- Search-hit text with the typed words marked. A separate component (not a
       v-html template) so the markup is built from data and cannot inject the
       query into the DOM. -->
  <span class="settings-highlight"><template v-for="(part, i) in parts" :key="i"><mark v-if="part.hit" class="settings-highlight__mark">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template></span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { highlightParts } from './settingsSearch'

const props = defineProps<{
  /** The text to display (a label or a description, already translated). */
  text: string
  /** The raw query as typed; empty renders the text unchanged. */
  query: string
}>()

const parts = computed(() => highlightParts(props.text, props.query))
</script>

<style scoped>
/* No display rule on the root: the same component renders a row label (a flex
   item that ellipsises) and a clamped description (a -webkit-box), and either
   would be broken by forcing `inline`. Typography is inherited from the class
   the caller passes in. */

/* Same accent tint as the row that the search lands on, so "what I typed" and
   "where I ended up" read as the same highlight. Kept as a background tint
   rather than a colour change: 3 of 36 themes use an accent too close to the
   body text to stay readable. */
.settings-highlight__mark {
  background: color-mix(in srgb, var(--accent-color) 24%, transparent);
  color: inherit;
  border-radius: calc(2 * var(--dp));
  padding: 0 calc(1 * var(--dp));
}
</style>
