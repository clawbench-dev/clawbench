<template>
  <div v-if="choices.length > 1" class="meeting-devices" data-role="meeting-mic-devices">
    <div class="meeting-devices__label">{{ t('meeting.create.micDevices') }}</div>
    <div class="meeting-devices__grid">
      <button
        v-for="d in choices"
        :key="d.id"
        class="meeting-device"
        :class="{ 'meeting-device--on': selectedId === d.id }"
        type="button"
        :disabled="disabled"
        :data-device="d.id"
        @click="emit('select', d.id)"
      >
        <span class="meeting-device__icon"><Mic :size="11" /></span>
        <span class="meeting-device__name" :title="d.name">{{ d.name }}</span>
        <Check v-if="selectedId === d.id" class="meeting-device__check" :size="11" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * Which input the publisher captures from.
 *
 * A preference list, not a set: a meeting publishes ONE microphone, so picking
 * another replaces the previous choice. Rendered on both surfaces that can
 * change it — the create form and the running meeting — because a host who
 * plugs in a headset mid-call should not have to end the meeting to use it.
 *
 * Hidden when there is nothing to choose between, which is the common case: one
 * input, or a device the browser will not name until it has been granted
 * microphone access.
 */
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Mic } from 'lucide-vue-next'
import { listAudioInputs, type AudioInputDevice } from '@/composables/useLiveShare'

withDefaults(defineProps<{ selectedId: string; disabled?: boolean }>(), { disabled: false })

const emit = defineEmits<{ select: [id: string] }>()

const { t } = useI18n()

const choices = ref<AudioInputDevice[]>([])

async function load(): Promise<void> {
  const list = (await listAudioInputs()).filter((d) => d.id !== 'default' && d.id !== 'communications')
  // "System default" only exists as a CHOICE once there is another device: on a
  // machine with one input it is the same thing twice.
  choices.value = list.length > 1 ? [{ id: '', name: t('meeting.create.micDefault') }, ...list] : list
}

onMounted(() => {
  void load()
  // Plugging a headset in is exactly when this list matters, and the browser
  // announces it.
  navigator.mediaDevices?.addEventListener?.('devicechange', load)
})

onBeforeUnmount(() => {
  navigator.mediaDevices?.removeEventListener?.('devicechange', load)
})
</script>

<style scoped>
.meeting-devices { display: flex; flex-direction: column; gap: var(--space-2); }
.meeting-devices__label {
  padding-left: calc(19 * var(--dp));
  font-size: var(--font-size-2xs); color: var(--text-muted);
}
.meeting-devices__grid {
  display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-3);
  padding-left: calc(19 * var(--dp));
}
.meeting-device {
  display: flex; align-items: center; gap: var(--space-2); min-width: 0;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-color); border-radius: var(--radius-md);
  background: var(--bg-secondary); color: var(--text-secondary);
  font-size: var(--font-size-xs); font-family: inherit; text-align: left;
  cursor: pointer;
}
.meeting-device:hover { border-color: var(--accent-color); }
.meeting-device:disabled { cursor: default; opacity: 0.6; }
.meeting-device--on {
  border-color: var(--accent-color); box-shadow: 0 0 0 1px var(--accent-color);
  color: var(--text-primary);
}
.meeting-device__icon {
  display: inline-flex; align-items: center; justify-content: center; flex-shrink: 0;
  width: calc(22 * var(--dp)); height: calc(22 * var(--dp));
  border-radius: var(--radius-sm); background: var(--bg-tertiary); color: var(--text-muted);
}
.meeting-device--on .meeting-device__icon { color: var(--accent-color); }
.meeting-device__name { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.meeting-device__check { margin-left: auto; flex-shrink: 0; color: var(--accent-color); }
</style>
