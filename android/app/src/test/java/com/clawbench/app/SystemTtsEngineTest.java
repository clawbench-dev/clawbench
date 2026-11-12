package com.clawbench.app;

import android.speech.tts.TextToSpeech;

import org.junit.Test;

import java.util.Locale;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

/**
 * Unit tests for the capability check behind the "use phone voice" switch.
 *
 * <p>Initialization succeeding only proves an engine exists. The switch is for
 * reading Chinese replies, so a phone with an English-only engine must be
 * rejected — otherwise every read would silently fall back to the server and
 * the user would think they had picked a voice they never hear.
 *
 * <p>Engines do not all report the same locale: some expose `zh_CN`, some only
 * `zh`, some only `zh_TW`. All three count as "can read Chinese", which is why
 * each case stubs all of them (an unstubbed Mockito int defaults to 0 =
 * LANG_AVAILABLE and would make every assertion pass).
 */
public class SystemTtsEngineTest {

    private static TextToSpeech engineWith(int zhCn, int zh, int zhTw) {
        TextToSpeech engine = mock(TextToSpeech.class);
        when(engine.isLanguageAvailable(Locale.SIMPLIFIED_CHINESE)).thenReturn(zhCn);
        when(engine.isLanguageAvailable(Locale.CHINESE)).thenReturn(zh);
        when(engine.isLanguageAvailable(Locale.TRADITIONAL_CHINESE)).thenReturn(zhTw);
        return engine;
    }

    @Test
    public void chineseVoiceInstalled_isUsable() {
        TextToSpeech engine = engineWith(
                TextToSpeech.LANG_COUNTRY_AVAILABLE,
                TextToSpeech.LANG_COUNTRY_AVAILABLE,
                TextToSpeech.LANG_NOT_SUPPORTED);

        assertTrue(SystemTtsEngine.hasChineseVoice(engine));
    }

    @Test
    public void engineExposingOnlyBareChinese_isUsable() {
        // A phone that advertises `zh` but not `zh_CN` can still read Chinese.
        TextToSpeech engine = engineWith(
                TextToSpeech.LANG_NOT_SUPPORTED,
                TextToSpeech.LANG_AVAILABLE,
                TextToSpeech.LANG_NOT_SUPPORTED);

        assertTrue(SystemTtsEngine.hasChineseVoice(engine));
    }

    @Test
    public void engineWithoutChineseData_isNotUsable() {
        TextToSpeech engine = engineWith(
                TextToSpeech.LANG_MISSING_DATA,
                TextToSpeech.LANG_MISSING_DATA,
                TextToSpeech.LANG_MISSING_DATA);

        assertFalse(SystemTtsEngine.hasChineseVoice(engine));
    }

    @Test
    public void engineThatDoesNotSupportChinese_isNotUsable() {
        TextToSpeech engine = engineWith(
                TextToSpeech.LANG_NOT_SUPPORTED,
                TextToSpeech.LANG_NOT_SUPPORTED,
                TextToSpeech.LANG_NOT_SUPPORTED);

        assertFalse(SystemTtsEngine.hasChineseVoice(engine));
    }

    @Test
    public void fallback_triesBareChineseWhenZhCnIsMissing() {
        TextToSpeech engine = mock(TextToSpeech.class);
        when(engine.setLanguage(Locale.CHINESE)).thenReturn(TextToSpeech.LANG_AVAILABLE);
        when(engine.setLanguage(Locale.TRADITIONAL_CHINESE)).thenReturn(TextToSpeech.LANG_NOT_SUPPORTED);

        assertTrue(SystemTtsEngine.applyLanguageFallback(engine, "你好"));
        verify(engine).setLanguage(Locale.CHINESE);
    }

    @Test
    public void fallback_givesUpWhenNoChineseVariantWorks() {
        TextToSpeech engine = mock(TextToSpeech.class);
        when(engine.setLanguage(Locale.CHINESE)).thenReturn(TextToSpeech.LANG_NOT_SUPPORTED);
        when(engine.setLanguage(Locale.TRADITIONAL_CHINESE)).thenReturn(TextToSpeech.LANG_MISSING_DATA);

        assertFalse(SystemTtsEngine.applyLanguageFallback(engine, "你好"));
    }
}
