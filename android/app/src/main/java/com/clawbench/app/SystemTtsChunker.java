package com.clawbench.app;

import java.util.ArrayList;
import java.util.List;

/**
 * Splits text into pieces the platform TextToSpeech engine will accept.
 *
 * <p>{@code TextToSpeech.speak} silently truncates anything longer than
 * {@link android.speech.tts.TextToSpeech#getMaxSpeechInputLength()} (4000
 * UTF-16 units), so a long reply must be fed in several calls. Cutting at a
 * fixed offset would break mid-sentence and make the voice pause at odd
 * places, so breaks are preferred in this order: sentence end, clause end,
 * whitespace, then a hard cut.
 *
 * <p>Pure logic with no Android dependency so it can be unit-tested on the
 * JVM (the same split the engine performs must be verifiable without a
 * device — a bad split is audible but never throws).
 */
public final class SystemTtsChunker {

    /** Ends a sentence — a pause here sounds natural. */
    private static final String SENTENCE_BREAKS = "。！？!?；;…\n";

    /** Ends a clause — acceptable when no sentence end fits. */
    private static final String CLAUSE_BREAKS = "，,、：:";

    private SystemTtsChunker() {
    }

    /**
     * Split {@code text} into non-empty chunks of at most {@code maxLen}
     * UTF-16 units.
     *
     * @param text   the text to split; null or blank yields an empty list
     * @param maxLen maximum chunk length in UTF-16 units; must be > 0
     */
    public static List<String> split(String text, int maxLen) {
        List<String> chunks = new ArrayList<>();
        if (text == null || maxLen <= 0) {
            return chunks;
        }

        String remaining = text.trim();
        while (!remaining.isEmpty()) {
            if (remaining.length() <= maxLen) {
                addTrimmed(chunks, remaining);
                break;
            }
            int cut = findCut(remaining, maxLen);
            addTrimmed(chunks, remaining.substring(0, cut));
            remaining = remaining.substring(cut).trim();
        }
        return chunks;
    }

    /**
     * Index (exclusive) to cut {@code text} at, never beyond {@code maxLen}.
     *
     * <p>Only the second half of the window is searched for punctuation: a
     * break near the start would waste most of the engine's capacity and turn
     * one reply into many tiny utterances.
     */
    private static int findCut(String text, int maxLen) {
        int limit = Math.min(maxLen, text.length());
        int windowStart = Math.max(1, limit / 2);

        int cut = lastBreak(text, windowStart, limit, SENTENCE_BREAKS);
        if (cut > 0) {
            return cut;
        }
        cut = lastBreak(text, windowStart, limit, CLAUSE_BREAKS);
        if (cut > 0) {
            return cut;
        }
        cut = lastBreak(text, windowStart, limit, " \t");
        if (cut > 0) {
            return cut;
        }

        // Hard cut. Never end on a high surrogate: the low half would start the
        // next chunk and both halves become U+FFFD (emoji and rare CJK).
        if (limit > 0 && Character.isHighSurrogate(text.charAt(limit - 1))) {
            return limit - 1;
        }
        return limit;
    }

    /** Index just past the last break character in [from, to), or -1. */
    private static int lastBreak(String text, int from, int to, String breaks) {
        for (int i = to - 1; i >= from; i--) {
            if (breaks.indexOf(text.charAt(i)) >= 0) {
                return i + 1;
            }
        }
        return -1;
    }

    private static void addTrimmed(List<String> chunks, String value) {
        String trimmed = value.trim();
        if (!trimmed.isEmpty()) {
            chunks.add(trimmed);
        }
    }
}
