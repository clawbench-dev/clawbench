<template>
  <!--
    "Fit to screen" switch for the public share page.

    One icon, one click: it enlarges the page to suit the reader's monitor and
    clicking again returns it to the design size. The app has the same feature
    as a button in Settings → Appearance, but a share link is opened by someone
    who has never run the app — there is no settings surface to find it in, so
    the control has to live on the page itself.

    A single toggle rather than a size slider: the reader's question is "why is
    this tiny on my monitor", not "what percentage do I want", and the answer
    can be computed from the screen.
  -->
  <button
    class="share-btn share-fit-btn"
    :class="{ active: fit }"
    type="button"
    :title="fit ? t('share.fitOff') : t('share.fitOn')"
    :aria-label="fit ? t('share.fitOff') : t('share.fitOn')"
    :aria-pressed="fit"
    data-action="share-fit"
    @click="toggle"
  >
    <!-- `Scaling` reads as "make the page fill the screen" at 16px; a
         magnifier would be confused with the document's own search. -->
    <Scaling :size="16" />
  </button>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Scaling } from 'lucide-vue-next'
import { isShareFitEnabled, setShareFitEnabled } from './shareScale'

const { t } = useI18n()

/**
 * Local mirror of the stored switch.
 *
 * The applied scale lives on `<html>`, which Vue does not track; this ref is
 * what drives the pressed state, and it is seeded from the same storage the
 * entry point used before mount (so the button is correct on first paint).
 */
const fit = ref(isShareFitEnabled())

function toggle(): void {
  fit.value = !fit.value
  setShareFitEnabled(fit.value)
}
</script>
