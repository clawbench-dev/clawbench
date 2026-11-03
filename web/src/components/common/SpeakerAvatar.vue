<template>
  <span class="speaker-avatar" :title="name" aria-hidden="true">
    <img v-if="src" class="speaker-avatar__img" :src="src" alt="" />
    <span v-else class="speaker-avatar__initial">{{ initial }}</span>
  </span>
</template>

<script setup lang="ts">
/**
 * The icon for one meeting participant, derived from the nickname.
 *
 * Two properties matter and both come from `utils/speakerAvatar.ts`: the same
 * name always yields the same icon (pure function of the name, no randomness),
 * and different names do not collide (the seed is the full nickname).
 *
 * The box is laid out at its final size from the first frame — the initial is
 * drawn until the DiceBear chunk resolves — so a transcript does not reflow
 * when 40 avatars arrive a moment later.
 *
 * Decorative by design: both surfaces print the name right next to the icon,
 * so announcing it again would just repeat every line to a screen reader.
 */
import { computed, ref, watch } from 'vue'
import { appLog } from '@/utils/appLog'
import { cachedSpeakerAvatar, speakerAvatarDataUri, speakerAvatarSeed } from '@/utils/speakerAvatar'

const props = defineProps<{ name: string }>()

const src = ref('')
const initial = computed(() => {
  const trimmed = props.name.trim()
  return trimmed ? Array.from(trimmed)[0].toUpperCase() : '?'
})

// A name change can land while the previous generation is still in flight, so
// each load carries a ticket and only the newest may write.
let ticket = 0

async function load(name: string): Promise<void> {
  const mine = ++ticket
  // Cached hits are synchronous: the second surface to render the same person
  // must not flash an initial.
  src.value = cachedSpeakerAvatar(name) ?? ''
  if (!speakerAvatarSeed(name)) return
  try {
    const uri = await speakerAvatarDataUri(name)
    if (mine === ticket) src.value = uri
  } catch (e) {
    // Offline or a stale build without the generator chunk: keep the initial.
    appLog.d('SpeakerAvatar', 'avatar generation failed', e)
  }
}

watch(() => props.name, load, { immediate: true })
</script>

<style scoped>
.speaker-avatar {
  /* One size for every surface (transcript, chat). Callers that need another
     size set `--sa-size` instead of restyling the box. */
  --sa-size: calc(20 * var(--dp));
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 auto;
  width: var(--sa-size);
  height: var(--sa-size);
  border-radius: 20%;
  overflow: hidden;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: calc(10 * var(--dp));
  font-weight: 600;
  line-height: 1;
  user-select: none;
}

.speaker-avatar__img {
  display: block;
  width: 100%;
  height: 100%;
}

.speaker-avatar__initial {
  display: block;
}
</style>
