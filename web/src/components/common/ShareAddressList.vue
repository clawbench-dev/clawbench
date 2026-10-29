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
      <input
        ref="inputRef"
        class="share-addr-input"
        type="text"
        :value="primaryUrl"
        readonly
        spellcheck="false"
        data-share-primary
        @focus="($event.target as HTMLInputElement).select()"
      />
      <CopyButton :text="primaryUrl" title-key="shareAddress.copy" class="share-addr-btn" />
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
import { ChevronDown } from 'lucide-vue-next'
import CopyButton from '@/components/common/CopyButton.vue'
import type { ShareAddress, ShareLink } from '@/composables/useShareLinks'

const props = defineProps<{ links: ShareLink[] }>()

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
}

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

.share-addr-btn {
  flex-shrink: 0;
}

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
