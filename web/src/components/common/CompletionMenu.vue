<template>
  <PopupMenu
    :show="show"
    :target-element="targetElement"
    anchor="left"
    :max-width="380"
    :max-height="300"
    :menu-items-count="items.length"
    @update:show="$emit('update:show', $event)"
  >
    <button
      v-for="(item, idx) in items"
      :key="item.source + ':' + item.key"
      class="completion-item"
      :class="['completion-item--' + item.source, { 'completion-item--active': idx === activeIndex }]"
      :data-completion-idx="idx"
      @mousedown.prevent="$emit('select', item)"
      @click.stop
    >
      <!-- Group members render their real avatar (custom or built-in backend
           icon); every other source keeps its generic source icon. -->
      <AgentIcon
        v-if="item.isMember"
        :backend="item.memberBackend || ''"
        :name="item.label"
        :avatar="item.memberAvatar || ''"
        size="md"
        class="completion-member-avatar"
      />
      <!-- Skill rows: the owning AI backend's icon REPLACES the generic source
           icon, so a skill installed for one tool is recognizable at a glance.
           Falls back to the source icon when the skill has no native owner
           (user dir / git repo / shared dir). -->
      <AgentIcon
        v-else-if="item.skillBackends && item.skillBackends.length"
        :backend="item.skillBackends[0]"
        size="sm"
        class="completion-source-icon"
      />
      <component v-else :is="sourceMeta(item.source).icon" :size="14" class="completion-source-icon" :style="{ color: sourceMeta(item.source).color }" />
      <component v-if="item.icon" :is="item.icon" :path="item.key" :is-dir="item.isDir === true" :size="14" class="completion-item-icon" />
      <span class="completion-text">
        <span class="completion-name-row">
          <span class="completion-label" v-html="renderLabel(item)"></span>
          <!-- Skill rows name the AI backend(s) that own the skill, right after
               the name. The icon already sits in the left slot above, so this is
               name-only to avoid showing the same icon twice. Empty for
               non-native sources. -->
          <span v-for="b in item.skillBackends || []" :key="b" class="completion-skill-backend">
            {{ getBackendDisplayName(b) }}
          </span>
          <!-- Real-agent disclosure: a member whose nickname was auto-derived (a
               pool name / "<base> <n>") shows the underlying agent's real name.
               The visibility rule + styling live in the shared component. -->
          <RealAgentNameTag
            v-if="item.isMember"
            class="completion-real-name"
            :agent-name="item.memberAgentName"
            :display-name="item.label"
          />
        </span>
        <!-- Skill rows omit the trailing source tag: every skill comes from the
             same logical place (the discovered skill list), so a per-row "Skill"
             chip is pure noise and steals width from the name/description. -->
        <span v-if="item.description" class="completion-desc" :class="{ 'completion-desc--skill': item.source === 'skill' }">{{ item.source === 'skill' ? item.description : middleEllipsis(item.description, 34) }}</span>
      </span>
      <span
        v-if="item.source !== 'skill'"
        class="completion-source"
        :class="'completion-source--' + item.source"
        :style="{ color: sourceMeta(item.source).color }"
      >
        {{ t(sourceMeta(item.source).labelKey) }}
      </span>
    </button>
  </PopupMenu>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PopupMenu from '@/components/common/PopupMenu.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import RealAgentNameTag from '@/components/common/RealAgentNameTag.vue'
import { SOURCE_META } from '@/utils/completionSources.ts'
import { getBackendDisplayName } from '@/utils/backendNames.ts'
import { middleEllipsis } from '@/utils/completionMatch.ts'
import { escapeHtml } from '@/utils/html.ts'
import type { CompletionItem, CompletionSource } from '@/utils/completionMatch.ts'

defineProps<{
  items: CompletionItem[]
  activeIndex: number
  show: boolean
  targetElement?: HTMLElement | null
}>()

defineEmits<{
  select: [item: CompletionItem]
  'update:show': [value: boolean]
}>()

const { t } = useI18n()

function sourceMeta(source: CompletionSource) {
  return SOURCE_META[source]
}

/**
 * Render the label with the fuzzy-matched characters wrapped in <mark>.
 * Falls back to the escaped label when no positions were recorded.
 */
function renderLabel(item: CompletionItem): string {
  const label = item.label || ''
  if (!item.positions || item.positions.length === 0) return escapeHtml(label)
  const hit = new Set(item.positions)
  let out = ''
  for (let i = 0; i < label.length; i++) {
    const ch = escapeHtml(label[i])
    out += hit.has(i) ? '<mark>' + ch + '</mark>' : ch
  }
  return out
}
</script>

<style scoped>
.completion-item {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  padding: var(--space-4) var(--space-6);
  border: none;
  background: none;
  cursor: pointer;
  text-align: left;
  transition: background var(--duration-fast);
}

.completion-item--active {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}

@media (hover: hover) {
  .completion-item:hover {
    background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  }
}

.completion-source-icon {
  flex-shrink: 0;
}

/* Group-member row: the real avatar (AgentIcon) replaces the source icon. */
.completion-member-avatar {
  flex-shrink: 0;
}

.completion-item-icon {
  flex-shrink: 0;
}

.completion-text {
  display: flex;
  flex-direction: column;
  /* Breathing room between the name row and the description. Without it the
     two lines sit flush, which reads as one cramped block — noticeable now that
     skill descriptions wrap to two lines. */
  gap: var(--space-2);
  min-width: 0;
  flex: 1;
}

/* Nickname + optional real-agent chip on one line. The label takes the slack
   and ellipsises; the chip stays intact at the trailing edge. */
.completion-name-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.completion-label {
  min-width: 0;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* .completion-real-name (the real-agent disclosure chip) is styled by the
   shared RealAgentNameTag component. */

/* Skill rows: the owning AI backend(s), shown right after the name. Muted and
   shrink-proof (the label absorbs the slack); one chip per backend. */
.completion-skill-backend {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  white-space: nowrap;
}

.completion-label mark {
  background: rgba(255, 230, 0, 0.5);
  color: inherit;
  padding: 0 1px;
  font-weight: var(--font-weight-bold);
}

:root[data-theme-base="dark"] .completion-label mark {
  background: rgba(255, 230, 0, 0.35);
}

/* Directory hint: muted, middle-ellipsised in JS so both the leading and
   trailing path segments stay visible on long paths. */
.completion-desc {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  text-align: left;
}

/* Skill descriptions are prose, not paths: keep the PREFIX (a middle ellipsis
   would splice in an unrelated tail) and wrap onto two lines instead of one. */
.completion-desc--skill {
  white-space: normal;
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  overflow: hidden;
}

.completion-source {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
}
</style>
