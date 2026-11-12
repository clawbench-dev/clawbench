package com.clawbench.app;

import org.junit.Test;

import java.util.List;
import java.util.Locale;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for the system-TTS text splitter.
 *
 * <p>TextToSpeech truncates anything past its input limit without telling
 * anyone, so the split is what keeps a long reply from being read halfway. It
 * is pure logic, which is why it lives in its own class: a bad split is
 * audible but never throws, so it can only be caught here.
 */
public class SystemTtsChunkerTest {

    @Test
    public void shortText_staysInOneChunk() {
        List<String> chunks = SystemTtsChunker.split("今天天气不错。", 100);
        assertEquals(1, chunks.size());
        assertEquals("今天天气不错。", chunks.get(0));
    }

    @Test
    public void everyChunk_respectsTheLimit() {
        String text = "这是一段很长的中文文本，用来验证分段逻辑不会超过引擎允许的长度。"
                + "它包含很多句子。每个句子都应该被完整地读出来，而不是在中间被截断。"
                + "最后再来一句收尾的话。";
        List<String> chunks = SystemTtsChunker.split(text, 20);
        assertTrue(chunks.size() > 1);
        for (String chunk : chunks) {
            assertTrue("chunk longer than limit: " + chunk, chunk.length() <= 20);
        }
    }

    @Test
    public void prefersSentenceEndOverClauseEnd() {
        // 12 chars; a clause break sits at 5 and a sentence break at 9. The
        // splitter must pick the sentence end even though both fit.
        List<String> chunks = SystemTtsChunker.split("前面五个字，后面结束。尾巴", 12);
        assertEquals("前面五个字，后面结束。", chunks.get(0));
    }

    @Test
    public void fallsBackToClauseEnd_whenNoSentenceEndFits() {
        List<String> chunks = SystemTtsChunker.split("前面五个字，后面还有很长很长", 8);
        assertEquals("前面五个字，", chunks.get(0));
    }

    @Test
    public void hardCuts_whenThereIsNoPunctuationAtAll() {
        List<String> chunks = SystemTtsChunker.split("abcdefghijklmnopqrstuvwxyz", 10);
        assertEquals(3, chunks.size());
        assertEquals("abcdefghij", chunks.get(0));
        assertEquals("klmnopqrst", chunks.get(1));
        assertEquals("uvwxyz", chunks.get(2));
    }

    @Test
    public void neverProducesEmptyChunks() {
        List<String> chunks = SystemTtsChunker.split("。 。 。 。 。 。 。 。 。 。 。 。", 3);
        for (String chunk : chunks) {
            assertFalse(chunk.isEmpty());
        }
    }

    @Test
    public void nullAndBlankYieldNoChunks() {
        assertTrue(SystemTtsChunker.split(null, 10).isEmpty());
        assertTrue(SystemTtsChunker.split("   \n\t ", 10).isEmpty());
        assertTrue(SystemTtsChunker.split("abc", 0).isEmpty());
    }

    @Test
    public void hardCut_neverSplitsASurrogatePair() {
        // Five emoji = ten UTF-16 units. A naive cut at 5 would separate the
        // high half from its low half, and both become U+FFFD.
        String emoji = "\uD83D\uDE00\uD83D\uDE00\uD83D\uDE00\uD83D\uDE00\uD83D\uDE00";
        List<String> chunks = SystemTtsChunker.split(emoji, 5);
        for (String chunk : chunks) {
            assertFalse(Character.isHighSurrogate(chunk.charAt(chunk.length() - 1)));
            assertFalse(Character.isLowSurrogate(chunk.charAt(0)));
        }
        assertEquals(emoji, String.join("", chunks));
    }

    @Test
    public void chineseText_usesTheChineseVoice() {
        assertEquals(Locale.SIMPLIFIED_CHINESE, SystemTtsEngine.pickLocale("你好，世界"));
        assertEquals(Locale.SIMPLIFIED_CHINESE, SystemTtsEngine.pickLocale("hello 你好"));
    }

    @Test
    public void latinText_usesTheEnglishVoice() {
        assertEquals(Locale.US, SystemTtsEngine.pickLocale("hello world"));
    }
}
