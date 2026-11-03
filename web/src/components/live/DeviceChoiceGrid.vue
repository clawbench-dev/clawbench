<template>
  <div
    class="device-choice"
    data-role="device-choice"
    :data-kind="kind"
    :data-variant="variant"
  >
    <div class="meeting-subsection device-choice__label">{{ label }}</div>

    <!-- Frames, for the camera. A camera is a PICTURE source, so it is drawn
         with the same 16:9 cards as the screens next to it: one look for every
         "what goes on the air" choice. The off card keeps the dashed frame —
         「不共享摄像头」 is the absence of a picture, not one of the devices — and
         it is always on screen, even on a machine with no camera at all: the
         answer to "which camera" is still "none", and hiding the group would
         make the form look like the question was never asked. -->
    <div v-if="variant === 'tile'" class="device-choice__grid device-choice__grid--tile">
      <button
        class="device-choice__card device-choice__card--tile device-choice__card--off"
        :class="{ 'device-choice__card--on': selectedId === '' }"
        type="button"
        :disabled="disabled"
        data-device="__off__"
        @click="emit('select', '')"
      >
        <span class="device-choice__thumb">
          <component :is="offIcon" :size="20" />
          <span v-if="selectedId === ''" class="device-choice__thumb-check">
            <Check :size="10" />
          </span>
        </span>
        <span class="device-choice__name">{{ offLabel }}</span>
      </button>

      <button
        v-for="d in tileDevices"
        :key="d.id"
        class="device-choice__card device-choice__card--tile"
        :class="{ 'device-choice__card--on': selectedId === d.id }"
        type="button"
        :disabled="disabled"
        :data-device="d.id"
        @click="emit('select', d.id)"
      >
        <span class="device-choice__thumb">
          <component :is="onIcon" :size="20" />
          <span v-if="selectedId === d.id" class="device-choice__thumb-check">
            <Check :size="10" />
          </span>
        </span>
        <span class="device-choice__name" :title="d.name">{{ d.name }}</span>
      </button>

      <button
        v-if="showAsk"
        class="device-choice__card device-choice__card--tile device-choice__card--prime"
        type="button"
        :disabled="priming || disabled"
        data-action="device-list-ask"
        @click="prime"
      >
        <span class="device-choice__thumb device-choice__thumb--ask">
          <component :is="onIcon" :size="20" />
        </span>
        <span class="device-choice__name">
          {{ priming ? t('meeting.create.listingDevices') : t('meeting.create.listDevices') }}
        </span>
      </button>
    </div>

    <!-- Rows, for the microphone: sound is not a picture, so a 16:9 frame would
         promise a preview that can never be drawn. -->
    <div v-else class="device-choice__grid">
      <!-- "Nothing on this axis" is a card like any other, which is what
           replaces the old on/off checkbox: the host compares "muted" against
           the actual devices side by side instead of reading a tick first. -->
      <button
        class="device-choice__card device-choice__card--off"
        :class="{ 'device-choice__card--on': selectedId === '' }"
        type="button"
        :disabled="disabled"
        data-device="__off__"
        @click="emit('select', '')"
      >
        <span class="device-choice__icon">
          <component :is="offIcon" :size="13" />
        </span>
        <span class="device-choice__name">{{ offLabel }}</span>
        <Check v-if="selectedId === ''" class="device-choice__check" :size="11" />
      </button>

      <button
        v-for="d in devices"
        :key="d.id"
        class="device-choice__card"
        :class="{ 'device-choice__card--on': selectedId === d.id }"
        type="button"
        :disabled="disabled"
        :data-device="d.id"
        @click="emit('select', d.id)"
      >
        <span class="device-choice__icon">
          <component :is="onIcon" :size="13" />
        </span>
        <span class="device-choice__name" :title="d.name">{{ d.name }}</span>
        <Check v-if="selectedId === d.id" class="device-choice__check" :size="11" />
      </button>

      <!-- The browser names no device until the page has been granted access
           for that kind; before that `enumerateDevices` answers with a single
           nameless placeholder, so the list would look like "only one device".
           Asking is the only honest option. -->
      <button
        v-if="showAsk"
        class="device-choice__card device-choice__card--prime"
        type="button"
        :disabled="priming || disabled"
        data-action="device-list-ask"
        @click="prime"
      >
        <span class="device-choice__icon">
          <component :is="onIcon" :size="13" />
        </span>
        <span class="device-choice__name">
          {{ priming ? t('meeting.create.listingDevices') : t('meeting.create.listDevices') }}
        </span>
      </button>
    </div>

    <div v-if="showAsk" class="device-choice__hint">
      {{ kind === 'audio' ? t('meeting.create.listDevicesHint') : t('meeting.create.listCamerasHint') }}
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * One card per choice, for the two device axes of a meeting (microphone and
 * camera).
 *
 * The screen grid already works this way — "share nothing" is a frame next to
 * the actual screens — and the two device axes used to be checkboxes with a
 * separate list underneath, which is two controls for one decision. Here the
 * first card IS the off switch: `''` means muted / no camera, `'default'` means
 * the system default device, anything else is a `deviceId`.
 *
 * Two shapes, because the two axes answer different questions. A camera is a
 * picture, so it uses the same 16:9 frame as the screens (`tile`); a microphone
 * is not, so it stays a compact row (`row`). Both share the listing, priming and
 * "nothing on this axis" behavior.
 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
// The sub-heading style is shared with the sections around this grid.
import '@/assets/meeting-form.css'
import { Check, Mic, MicOff, Video, VideoOff } from 'lucide-vue-next'
import { listAudioInputs, listVideoInputs, type AudioInputDevice } from '@/composables/useLiveShare'
import { mediaPermission } from '@/utils/micCapture'
import { appLog } from '@/utils/appLog'

const props = withDefaults(
  defineProps<{
    /** Which device axis this grid is for. */
    kind: 'audio' | 'video'
    /** Small heading above the cards. */
    label: string
    /** Label of the first card — "静音" / "不共享摄像头". */
    offLabel: string
    /** `''` for the off card, `'default'` for the system default, else an id. */
    selectedId: string
    /** Frame cards (pictures) or compact rows (sound). */
    variant?: 'row' | 'tile'
    /**
     * Whether this machine has such hardware at all.
     *
     * The camera grid is always on screen (the off card is an answer even with
     * no camera), so it must not offer "ask the browser for the list" on a
     * machine that has nothing to list.
     */
    available?: boolean
    disabled?: boolean
  }>(),
  { variant: 'row', available: true, disabled: false },
)

const emit = defineEmits<{ select: [id: string] }>()

const { t } = useI18n()

const onIcon = computed(() => (props.kind === 'audio' ? Mic : Video))
const offIcon = computed(() => (props.kind === 'audio' ? MicOff : VideoOff))

const devices = ref<AudioInputDevice[]>([])
/** Access is withheld, so the device list cannot be read yet. */
const unnamed = ref(false)
const priming = ref(false)

/**
 * What the frame grid offers.
 *
 * `devices` always starts with the synthetic "system default" entry, which is
 * right when there IS hardware to fall back to. With no camera at all it would
 * offer a device that cannot exist — the honest answer is the off card alone
 * (plus the "ask for the list" card while access is still withheld).
 */
const tileDevices = computed(() => (devices.value.length > 1 ? devices.value : []))
/** Ask for the device list — only when there is hardware whose list is withheld. */
const showAsk = computed(() => unnamed.value && props.available)

async function load(): Promise<void> {
  const list = (props.kind === 'audio' ? await listAudioInputs() : await listVideoInputs()).filter(
    // An empty id means "the browser is not naming devices yet": those entries
    // are placeholders, and using them as keys would collapse the cards into one
    // per duplicate id. `default` / `communications` are Chromium's own
    // pseudo-devices and would offer the same hardware a second time.
    (d) => d.id !== '' && d.id !== 'default' && d.id !== 'communications',
  )
  // "System default" is always offered, even when there is one device: it is
  // the only value that keeps following the OS when a headset is plugged in,
  // and it is the default the meeting opens with — a card that is selected but
  // not on screen would leave the group looking unset.
  devices.value = [{ id: 'default', name: t('meeting.create.deviceDefault') }, ...list]
  unnamed.value = list.length === 0 && (await mediaPermission(props.kind === 'audio' ? 'microphone' : 'camera')) !== 'denied'
}

/**
 * Open the device once so the browser releases its list.
 *
 * The track is stopped immediately: this is about the permission, not about
 * capturing. Nothing is published, and the indicator goes away with the track.
 */
async function prime(): Promise<void> {
  if (priming.value) return
  priming.value = true
  try {
    const constraints = props.kind === 'audio' ? { audio: true } : { video: true }
    const stream = await navigator.mediaDevices.getUserMedia(constraints)
    stream.getTracks().forEach((track) => track.stop())
  } catch (e) {
    appLog.w('DeviceChoiceGrid', `listing ${props.kind} devices needs access`, e)
  } finally {
    priming.value = false
  }
  await load()
}

onMounted(() => {
  void load()
  // Plugging a headset (or a second camera) in is exactly when this list
  // matters, and the browser announces it.
  navigator.mediaDevices?.addEventListener?.('devicechange', load)
})

onBeforeUnmount(() => {
  navigator.mediaDevices?.removeEventListener?.('devicechange', load)
})
</script>

<style scoped>
.device-choice { display: flex; flex-direction: column; gap: var(--space-2); }
/* The label is a section sub-heading (meeting-subsection); only the spacing is
   local, so it sits 8px above the cards like every other sub-heading. */
.device-choice__label { margin-bottom: var(--space-2); }
.device-choice__grid {
  display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-3);
}
.device-choice__card {
  display: flex; align-items: center; gap: var(--space-2); min-width: 0;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-color); border-radius: var(--radius-md);
  background: var(--bg-secondary); color: var(--text-secondary);
  font-size: var(--font-size-xs); font-family: inherit; text-align: left;
  cursor: pointer;
}
.device-choice__card:hover { border-color: var(--accent-color); }
.device-choice__card:disabled { cursor: default; opacity: 0.6; }
.device-choice__card--on {
  border-color: var(--accent-color); box-shadow: 0 0 0 1px var(--accent-color);
  color: var(--text-primary);
}
/* The off card keeps a dashed outline, like "不共享屏幕" on the screen grid: it
   is the absence of a device, not one of them. Frame cards carry the dash on
   the thumb instead (the card itself stays a solid frame, exactly like the
   screen tiles), so this only applies to the row shape. */
.device-choice__card--off { border-style: dashed; }
.device-choice__card--off.device-choice__card--on { border-style: solid; }
.device-choice__card--tile.device-choice__card--off { border-style: solid; }
.device-choice__icon {
  display: inline-flex; align-items: center; justify-content: center; flex-shrink: 0;
  width: calc(22 * var(--dp)); height: calc(22 * var(--dp));
  border-radius: var(--radius-sm); background: var(--bg-tertiary); color: var(--text-muted);
}
.device-choice__card--on .device-choice__icon { color: var(--accent-color); }
.device-choice__name { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.device-choice__check { margin-left: auto; flex-shrink: 0; color: var(--accent-color); }
.device-choice__card--prime .device-choice__name { color: var(--accent-color); }
.device-choice__hint {
  font-size: var(--font-size-2xs); line-height: 1.6; color: var(--text-muted);
}

/* ── Frame variant ──────────────────────────────────────────────────────────
   The camera's cards are the SCREEN's cards: same 16:9 thumb, same dashed
   "nothing" frame, same check badge in the corner. The numbers below are the
   ones in CaptureSourceGrid (compact); the two must be changed together or the
   picture and camera rows stop lining up. */
.device-choice__grid--tile {
  grid-template-columns: repeat(auto-fill, minmax(calc(120 * var(--dp)), 1fr));
  gap: var(--space-4);
}
.device-choice__card--tile {
  flex-direction: column; align-items: stretch; gap: var(--space-2);
  padding: var(--space-3);
}
.device-choice__thumb {
  position: relative;
  display: flex; align-items: center; justify-content: center;
  width: 100%; aspect-ratio: 16 / 9;
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary); color: var(--text-hint);
  overflow: hidden;
}
/* An empty dashed frame reads as "no picture" without a word of explanation. */
.device-choice__card--off .device-choice__thumb {
  border: 1px dashed var(--border-color);
  background: transparent;
}
/* Same selected "nothing" language as the screen grid: a soft accent wash, not
   a solid outline on an empty box (that would nest a second frame inside the
   card's own ring). The dashed frame stays neutral. */
.device-choice__card--off.device-choice__card--on .device-choice__thumb {
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
  color: var(--accent-color);
}
.device-choice__card--on.device-choice__card--tile .device-choice__thumb { color: var(--accent-color); }
.device-choice__thumb--ask {
  border: 1px dashed var(--border-color);
  background: transparent;
  color: var(--text-muted);
}
.device-choice__card--prime:hover .device-choice__thumb--ask { color: var(--accent-color); }
.device-choice__thumb-check {
  position: absolute; top: calc(4 * var(--dp)); right: calc(4 * var(--dp));
  display: inline-flex; align-items: center; justify-content: center;
  width: calc(15 * var(--dp)); height: calc(15 * var(--dp));
  border-radius: var(--radius-full);
  background: var(--accent-color); color: var(--bg-secondary);
  box-shadow: 0 0 0 1.5px var(--bg-secondary);
}
.device-choice__card--tile .device-choice__name { color: var(--text-secondary); }
.device-choice__card--tile.device-choice__card--on .device-choice__name { color: var(--text-primary); }
.device-choice__card--prime.device-choice__card--tile .device-choice__name { color: var(--accent-color); }
</style>
