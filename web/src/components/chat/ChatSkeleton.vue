<template>
  <!-- Simulated conversation shown while a session switch is in flight: the
       previous messages were cleared and the new history has not arrived yet.
       It stands in the message area so the layout does not collapse to a lone
       spinner — the alternating rows preview the shape of what is coming.

       Purely decorative: `aria-hidden` keeps it out of the accessibility tree,
       and the live region on the panel announces the load. -->
  <div class="chat-skeleton" aria-hidden="true">
    <div
      v-for="(row, i) in rows"
      :key="i"
      class="chat-skeleton-row"
      :class="row.role"
    >
      <div v-if="row.role === 'assistant'" class="skeleton-block skeleton-circle chat-skeleton-avatar"></div>
      <div class="chat-skeleton-body">
        <!-- Assistant turns lead with a short "name" line; user turns are just
             a bubble, matching the real layout. -->
        <div v-if="row.role === 'assistant'" class="skeleton-block chat-skeleton-name"></div>
        <div
          v-for="(w, j) in row.lines"
          :key="j"
          class="skeleton-block chat-skeleton-line"
          :style="{ width: w }"
        ></div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * Static skeleton for the message area during a session switch. The rows are
 * fixed (not randomized) so the placeholder is stable across re-renders — a
 * shimmer already conveys "loading"; a shifting layout would read as content
 * arriving and then changing.
 *
 * Widths are percentages so the placeholder adapts to any panel width.
 */
const rows = [
  { role: 'assistant', lines: ['88%', '72%', '54%'] },
  { role: 'user', lines: ['62%'] },
  { role: 'assistant', lines: ['80%', '64%', '76%', '40%'] },
] as const
</script>

<style scoped>
.chat-skeleton {
  display: flex;
  flex-direction: column;
  gap: var(--space-8);
  padding: var(--space-2) 0;
}

.chat-skeleton-row {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  min-width: 0;
}

/* Assistant rows hug the left edge, user rows the right — mirroring the real
   .chat-message.user / .chat-message.assistant alignment. */
.chat-skeleton-row.user {
  justify-content: flex-end;
}

.chat-skeleton-avatar {
  width: 28px;
  height: 28px;
  margin-top: var(--space-1);
}

.chat-skeleton-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-width: 0;
  flex: 1;
}

.chat-skeleton-row.user .chat-skeleton-body {
  flex: 0 1 auto;
  align-items: flex-end;
  /* Cap the user bubble so it reads as a short message, not a full-width bar. */
  max-width: 60%;
}

.chat-skeleton-name {
  width: 96px;
  height: 12px;
}

.chat-skeleton-line {
  height: 14px;
}

.chat-skeleton-row.user .chat-skeleton-line {
  --skeleton-radius: var(--radius-lg);
  height: 20px;
}
</style>
