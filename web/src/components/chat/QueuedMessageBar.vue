<template>
  <div v-if="messages.length > 0" class="queued-bar" :class="{ expanded }">
    <!-- The banner is a layout row holding two SIBLING buttons: the expand
         toggle, then the action. They cannot be nested (a <button> inside a
         <button> is invalid HTML), which is why the toggle is wrapped rather
         than the whole row being clickable. -->
    <div class="queued-bar-banner">
      <button
        class="queued-bar-header"
        type="button"
        :aria-expanded="expanded"
        @click="expanded = !expanded"
      >
        <LoadingIndicator class="queued-bar-spinner" size="sm" inline />
        <span class="queued-bar-status">
          <span class="queued-bar-title">{{ t('chat.pending.barTitle') }}</span>
          <!-- The count is only informative when there is more than one entry:
               for a single collapsed row the number repeats what the preview
               already shows ("排队中 1 看一下这个文件"). Expanded keeps it, since
               the badge then labels the list below. -->
          <span
            v-if="expanded || messages.length > 1"
            class="queued-bar-count count-badge"
          >{{ messages.length }}</span>
        </span>
        <span v-if="!expanded" class="queued-bar-preview">{{ nextPreview }}</span>
        <ChevronDown class="queued-bar-chevron" :size="14" />
      </button>

      <!-- Collapsed only: act on the NEXT message out without expanding first.
           Expanded rows carry their own action button, so this would duplicate
           it. Behaviour (insert vs interrupt) and the tooltip stay
           capability-dependent, exactly like the row button. -->
      <button
        v-if="!expanded && head"
        class="queued-bar-action queued-bar-header-action"
        :class="{ 'queued-bar-action-interrupt': !midTurnSupported }"
        type="button"
        :disabled="busy === head.queueId"
        :title="midTurnSupported ? t('chat.pending.insertHint') : t('chat.pending.interruptHint')"
        @click="$emit('action', head.queueId, midTurnSupported ? 'insert' : 'interrupt')"
      >
        <Zap v-if="midTurnSupported" :size="11" />
        <Square v-else :size="11" fill="currentColor" />
        {{ t('chat.pending.insert') }}
      </button>
    </div>

    <ul v-if="expanded" class="queued-bar-list">
      <li v-for="msg in messages" :key="msg.queueId" class="queued-bar-item">
        <div class="queued-bar-text">{{ msg.text || t('chat.pending.attachment') }}</div>
        <div v-if="msg.files.length > 0" class="queued-bar-files">
          <span v-for="(f, i) in msg.files" :key="i" class="queued-bar-file">{{ fileLabel(f) }}</span>
        </div>
        <div class="queued-bar-actions">
          <button
            class="queued-bar-action"
            :class="{ 'queued-bar-action-interrupt': !midTurnSupported }"
            type="button"
            :disabled="busy === msg.queueId"
            :title="midTurnSupported ? t('chat.pending.insertHint') : t('chat.pending.interruptHint')"
            @click="$emit('action', msg.queueId, midTurnSupported ? 'insert' : 'interrupt')"
          >
            <Zap v-if="midTurnSupported" :size="11" />
            <Square v-else :size="11" fill="currentColor" />
            <!-- The label is deliberately CONSTANT: the button is always
                 "插话" regardless of whether the backend can join the running
                 turn. The behaviour still differs (insert vs interrupt) and is
                 conveyed by the icon and the tooltip, which stay
                 capability-dependent. -->
            {{ t('chat.pending.insert') }}
          </button>
          <button
            class="queued-bar-remove"
            type="button"
            :disabled="busy === msg.queueId"
            :title="t('chat.pending.remove')"
            @click="$emit('remove', msg.queueId)"
          >×</button>
        </div>
      </li>
    </ul>

    <!-- Footer action, expanded only. Merging needs at least two entries, so it
         is absent (not disabled) for a single one — there is nothing it could
         do. Kept OUT of the per-row list: it acts on the WHOLE queue, so
         repeating it on every row would read as a per-row action. -->
    <div v-if="expanded && messages.length > 1" class="queued-bar-footer">
      <button
        class="queued-bar-action"
        type="button"
        :disabled="mergeBusy"
        :title="t('chat.pending.mergeHint')"
        @click="$emit('merge')"
      >
        <Merge :size="11" />
        {{ t('chat.pending.merge') }}
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, Zap, Square, Merge } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'

const props = defineProps({
  /** Queued messages for the active session (from useMessageQueue). */
  messages: { type: Array, required: true },
  /** Whether the current backend can join the running turn (insert vs interrupt). */
  midTurnSupported: { type: Boolean, default: false },
  /** queueId of the entry whose action request is in flight. */
  busy: { type: String, default: '' },
  /** Whether the merge request is in flight. */
  mergeBusy: { type: Boolean, default: false },
})

defineEmits(['remove', 'action', 'merge'])

const { t } = useI18n()
const expanded = ref(false)

// Collapse back when the queue empties. The root `v-if="messages.length > 0"`
// hides the CARD, but it does not unmount this component — so `expanded` would
// survive the gap and the next batch of queued messages would appear already
// expanded, even though the panel is supposed to start collapsed. Resetting on
// the empty transition (not on every change) keeps the user's expand/collapse
// choice while a queue is in progress.
watch(() => props.messages.length, (len) => {
  if (len === 0) expanded.value = false
})

// Collapsed header shows the NEXT message to be sent, so the queue is readable
// without expanding. Attachment-only entries fall back to the same label the
// expanded row uses. Single-line + ellipsis in CSS bounds the width.
const nextPreview = computed(() => {
  const first = props.messages[0]
  if (!first) return ''
  return first.text || t('chat.pending.attachment')
})

// The entry the collapsed banner's action button acts on: the NEXT one out.
// Same head the preview shows, so the button and the text always agree.
const head = computed(() => props.messages[0] || null)

function fileLabel(f) {
  if (!f) return ''
  if (f.kind === 'quote') return t('chat.pending.fileReference')
  if (f.kind === 'url') return f.path || f.url || ''
  const p = f.path || ''
  const parts = p.split('/')
  return parts[parts.length - 1] || p
}
</script>

<style scoped>
.queued-bar {
  flex-shrink: 0;
  /* Same horizontal inset + bottom rhythm as the plan card (.plan-panel) and
     the same corner radius as its collapsed chip (.plan-chip), so the two
     cards stack as one column when both are visible. */
  margin: 0 var(--space-5) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  background: var(--bg-secondary);
  overflow: hidden;
}

/* The collapsed banner is a row of two sibling buttons: the expand toggle
   (flex:1) and the action button, pinned right. */
.queued-bar-banner {
  display: flex;
  align-items: center;
}

.queued-bar-header {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  /* Matches .plan-chip's rhythm: a 4px/10px box with a 6px gap. The old
     2px/4px box with a 4px gap is what made the card feel cramped. */
  gap: var(--space-3);
  width: 100%;
  padding: var(--space-2) var(--space-5);
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-secondary);
  /* The header is a <button>, which the UA stylesheet centres. Pin the text
     left so the title/preview read as a left-aligned row. */
  text-align: left;
  /* Matches the execution-plan chip title (.plan-chip__text). The queue used
     --font-size-2xs (10px), which is the BADGE size — too small for a card's
     primary text. */
  font-size: var(--font-size-sm);
}

.queued-bar-spinner {
  --li-color: var(--accent-color, currentColor);
}

.queued-bar-status {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  text-align: left;
}

.queued-bar-title {
  white-space: nowrap;
}

/* Shape/geometry comes from the shared .count-badge (css/components.css); a
   scoped rule here would outrank it, so keep only colour + weight. The badge
   separates the count from the message preview, which otherwise read as one
   run-on sentence ("排队中 · 2 看一下这个文件"). */
.queued-bar-count {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-weight: var(--font-weight-semibold);
}

.queued-bar-preview {
  flex: 1;
  min-width: 0;
  /* Fainter than the title so it reads as secondary information, and clipped
     to one line so a long message can never grow the collapsed header. */
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  /* The header is a <button>, which the UA stylesheet centres. Without this the
     preview (a flex item, so it still takes the free space) renders its text
     centred inside that space — reported as "the summary is centred". */
  text-align: left;
}

.queued-bar-chevron {
  transition: transform var(--duration-base);
  flex-shrink: 0;
  /* Keeps the chevron at the right edge in BOTH states. The collapsed preview
     already fills the row with flex:1, but it is absent when expanded — without
     this the chevron would sit flush against the title instead. */
  margin-left: auto;
}

.queued-bar.expanded .queued-bar-chevron {
  transform: rotate(180deg);
}

.queued-bar-list {
  list-style: none;
  margin: 0;
  /* Inset matches the header's horizontal padding so rows line up with the
     title text, and the bottom inset gives the last row room to breathe. */
  padding: 0 var(--space-5) var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  /* Bounded so this list can never crowd out the message area when the plan
     card is ALSO expanded (its timeline is capped at 240px the same way). */
  max-height: min(40vh, 240px);
  overflow-y: auto;
}

.queued-bar-item {
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary, rgba(127, 127, 127, 0.08));
}

.queued-bar-text {
  font-size: var(--font-size-sm);
  color: var(--text-primary);
  /* Single-line (nowrap + ellipsis), so line-height sets the row height. Match
     the plan panel's entry text (snug) instead of the 1.6 base, so the taller
     font does not make each row disproportionately airy. */
  line-height: var(--line-height-snug);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.queued-bar-files {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1);
  margin-top: var(--space-1);
}

.queued-bar-file {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  background: rgba(127, 127, 127, 0.14);
  border-radius: var(--radius-full);
  padding: 0 var(--space-2);
  max-width: 40vw;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.queued-bar-actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-2);
}

/* Footer action row (merge). Sits below the bounded list, so it stays visible
   without scrolling even when the list is at its cap. Shares the list's
   horizontal inset so it lines up with the row text above it. */
.queued-bar-footer {
  display: flex;
  justify-content: flex-end;
  padding: 0 var(--space-5) var(--space-4);
}

.queued-bar-action {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  background: rgba(127, 127, 127, 0.16);
  border: none;
  border-radius: var(--radius-full);
  cursor: pointer;
  color: var(--text-secondary);
  padding: 1px 7px;
  font-size: var(--font-size-xs);
  line-height: var(--line-height-relaxed);
  transition: background var(--duration-base), color var(--duration-base);
}

.queued-bar-action:disabled,
.queued-bar-remove:disabled {
  opacity: var(--opacity-muted);
  cursor: default;
}

.queued-bar-action-interrupt {
  color: #e08a8a;
}

/* The collapsed banner's action button, rendered as a STANDALONE HALF-CAPSULE
   capping the card's right endpoint. Its shape is deliberately asymmetric:
     - LEFT  = 0 (square)      → a straight vertical cut, so the control reads
       as a SEGMENT attached to the banner (a piece bolted on) rather than a
       free-floating pill. This is what makes it look "assembled into" the bar.
     - RIGHT = card inner radius → follows the card's own corner so the outer
       end closes the card cleanly instead of overshooting it.
   It stretches to the banner's full height (it is an endpoint, not a chip), and
   must not shrink, or a long preview would squeeze the label.
   The right radius is `--radius-lg` minus the card's 1px border: matching the
   card's INNER corner is what makes the two radii read as one continuous edge.
   Note this is a component-local class (no global rule to outrank), so unlike
   the shared `.count-badge` the geometry belongs here. */
.queued-bar-header-action {
  flex-shrink: 0;
  align-self: stretch;
  /* Four values: TL TR BR BL. The left pair is a deliberate 0 — the square cut
     is the point, so it is written literally rather than tokenised (matching
     the design guide's rule that 0 is not a token: it expresses a decision). */
  border-radius: 0 calc(var(--radius-lg) - 1px) calc(var(--radius-lg) - 1px) 0;
  /* A distinct surface so the cap reads as its own control against the card
     (same --bg-tertiary-on---bg-secondary pairing the count badge uses). */
  background: var(--bg-tertiary);
  /* No right margin: the cap sits flush against the card's right edge. */
  padding: 0 var(--space-5) 0 var(--space-4);
}

@media (hover: hover) {
  .queued-bar-action:not(:disabled):hover {
    background: rgba(127, 127, 127, 0.28);
  }
}

.queued-bar-remove {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-secondary);
  padding: 0 var(--space-1);
  font-size: var(--font-size-md);
  line-height: 1;
  transition: color var(--duration-base);
  /* Pin the delete control to the right edge; the action button stays left. */
  margin-left: auto;
}

@media (hover: hover) {
  .queued-bar-remove:not(:disabled):hover {
    color: var(--text-primary);
  }
}
</style>
