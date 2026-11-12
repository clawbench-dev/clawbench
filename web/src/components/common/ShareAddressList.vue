<template>
  <!--
    One primary link + an expandable list of every address the server is
    reachable on.

    Default state shows ONLY the primary link, because that is the common case
    and a wall of addresses would be noise. The list is one click away for the
    case that matters: the colleague cannot open the primary link because they
    are on a different subnet.

    Virtual adapters (Docker bridges, VPN tunnels) are shown rather than hidden,
    but labelled — a user who cannot explain why "172.17.0.1" fails is worse off
    than one who reads "Docker" next to it and picks the Wi-Fi address.
  -->
  <div class="share-addr">
    <div class="share-addr-primary">
      <span v-if="leadingIcon" class="share-addr-icon"><Globe :size="11" /></span>
      <input
        ref="inputRef"
        class="share-addr-input"
        :class="{ 'is-with-icon': leadingIcon }"
        type="text"
        :value="primaryUrl"
        readonly
        spellcheck="false"
        data-share-primary
        @focus="($event.target as HTMLInputElement).select()"
      />
      <CopyButton
        :text="primaryUrl"
        :label="copyLabel"
        title-key="shareAddress.copy"
        class="share-addr-btn"
        :class="{ 'share-addr-btn--labeled': copyLabel }"
      />
      <slot name="trailing" />
    </div>

    <button
      v-if="links.length > 1"
      class="share-addr-toggle"
      type="button"
      :aria-expanded="expanded"
      data-share-toggle
      @click="expanded = !expanded"
    >
      <ChevronDown :size="13" class="share-addr-chevron" :class="{ 'is-open': expanded }" />
      {{ expanded ? t('shareAddress.hideAll') : t('shareAddress.showAll', { count: links.length }) }}
    </button>

    <ul v-if="expanded" class="share-addr-list">
      <li v-for="l in links" :key="l.url" class="share-addr-row">
        <span class="share-addr-ip">{{ l.address.ip }}</span>
        <span v-if="kindLabel(l.address)" class="share-addr-kind" :class="`share-addr-kind--${l.address.kind}`">
          {{ kindLabel(l.address) }}
        </span>
        <span v-if="l.address.interface" class="share-addr-iface">{{ l.address.interface }}</span>
        <CopyButton :text="l.url" title-key="shareAddress.copy" class="share-addr-row-btn" />
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, Globe } from 'lucide-vue-next'
import CopyButton from '@/components/common/CopyButton.vue'
import type { ShareAddress, ShareLink } from '@/composables/useShareLinks'

const props = defineProps<{
  links: ShareLink[]
  /**
   * Draw a globe inside the field. For hosts that show the address on its own
   * card (the meeting record) rather than under a labelled row.
   */
  leadingIcon?: boolean
  /**
   * Visible label on the copy button. Off by default: the button is usually
   * pinned to a container edge, and a label grows it into whatever sits
   * beside it.
   */
  copyLabel?: string
}>()

const { t } = useI18n()
const expanded = ref(false)
const inputRef = ref<HTMLInputElement | null>(null)

const primaryUrl = computed(() => props.links[0]?.url ?? '')

/**
 * Human label for an address kind. Returns '' for the browser's own origin
 * (`current`) and for plain LAN, because "局域网" next to every entry is noise;
 * the meaningful labels are the ones that warn (virtual) or inform (public).
 */
function kindLabel(a: ShareAddress): string {
  switch (a.kind) {
    case 'virtual':
      return t('shareAddress.kindVirtual')
    case 'public':
      return t('shareAddress.kindPublic')
    case 'current':
      return t('shareAddress.kindCurrent')
    default:
      return ''
  }
}

/** Focus the primary input (used after a link is generated). */
function focusPrimary(): void {
  inputRef.value?.select()
}

defineExpose({ focusPrimary })
</script>

<style scoped>
.share-addr {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
  min-width: 0;
}

.share-addr-primary {
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  min-width: 0;
  position: relative;
}

.share-addr-icon {
  position: absolute;
  left: var(--space-4, 8px);
  top: 50%;
  transform: translateY(-50%);
  display: flex;
  color: var(--text-hint, #9aa4af);
  pointer-events: none;
}

.share-addr-input.is-with-icon { padding-left: calc(22 * var(--dp, 1px)); }

.share-addr-input {
  flex: 1;
  min-width: 0;
  padding: var(--space-3, 6px) var(--space-5, 10px);
  border-radius: var(--radius-sm, 6px);
  border: 1px solid var(--border-color, #d0d7de);
  background: var(--bg-secondary, #fff);
  color: var(--text-primary, #1f2328);
  font-size: var(--font-size-sm, 12px);
  font-family: var(--font-mono, monospace);
}

/* Square icon button, the same footprint as the host's trailing button (the
   share dialogs' "regenerate"). A bare 14px glyph here would sit next to a
   28dp box and the pair would read as two unrelated things. */
.share-addr-btn {
  flex-shrink: 0;
  width: calc(28 * var(--dp, 1px));
  height: calc(28 * var(--dp, 1px));
  border-radius: var(--radius-sm, 6px);
  color: var(--text-secondary, #495057);
}

@media (hover: hover) {
  .share-addr-btn:not(.share-addr-btn--labeled):hover {
    background: var(--bg-tertiary, #e9ecef);
    color: var(--accent-color, #4a90d9);
  }
}

/* Label variant: a bordered field-height button, because a bare glyph with a
   word next to it has no edge to sit on. Width must be released or the label
   would be clipped to the square footprint above. */
.share-addr-btn--labeled {
  width: auto;
  gap: var(--space-2, 4px);
  height: calc(28 * var(--dp, 1px));
  padding: 0 var(--space-5, 10px);
  border: 1px solid var(--border-color, #d0d7de);
  border-radius: var(--radius-sm, 6px);
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-secondary, #495057);
  font-size: var(--font-size-xs, 11px);
}
.share-addr-btn--labeled:hover { color: var(--accent-color, #4a90d9); }

.share-addr-toggle {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1, 2px);
  align-self: flex-start;
  padding: var(--space-1, 2px) 0;
  border: none;
  background: transparent;
  color: var(--text-muted, #6c757d);
  font-size: var(--font-size-xs, 11px);
  cursor: pointer;
}

.share-addr-toggle:hover {
  color: var(--text-secondary, #495057);
}

.share-addr-chevron {
  transition: transform var(--duration-fast, 0.15s) ease;
}

.share-addr-chevron.is-open {
  transform: rotate(180deg);
}

.share-addr-list {
  list-style: none;
  margin: 0;
  padding: var(--space-1, 2px) 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-1, 2px);
  max-height: 160px;
  overflow-y: auto;
}

.share-addr-row {
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: var(--space-1, 2px) var(--space-2, 4px);
  border-radius: var(--radius-xs, 3px);
  font-size: var(--font-size-xs, 11px);
  min-width: 0;
}

.share-addr-row:hover {
  background: var(--bg-hover, rgba(0, 0, 0, 0.04));
}

.share-addr-ip {
  font-family: var(--font-mono, monospace);
  color: var(--text-primary, #1f2328);
  flex-shrink: 0;
}

.share-addr-kind {
  flex-shrink: 0;
  padding: 0 var(--space-2, 4px);
  border-radius: var(--radius-xs, 3px);
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-secondary, #495057);
  font-size: var(--font-size-2xs, 10px);
  line-height: 1.6;
}

/* Virtual adapters are the ones that usually fail for a colleague; tinting them
   is the whole point of showing them instead of hiding them. */
.share-addr-kind--virtual {
  background: var(--bg-tertiary, #e9ecef);
  color: var(--color-yellow, #a16207);
}

.share-addr-iface {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted, #6c757d);
}

.share-addr-row-btn {
  flex-shrink: 0;
  margin-left: auto;
}
</style>
