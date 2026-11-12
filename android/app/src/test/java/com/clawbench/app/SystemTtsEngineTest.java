package com.clawbench.app;

import android.speech.tts.TextToSpeech;

import org.junit.Test;

import java.util.Locale;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

/**
 * Unit tests for the capability check behind the "use phone voice" switch.
 *
 * <p>Initialization succeeding only proves an engine exists. The switch is for
 * reading Chinese replies, so a phone with an English-only engine must be
 * rejected — otherwise every read would silently fall back to the server and
 * the user would think they had picked a voice they never hear.
 */
public class SystemTtsEngineTest {

    @Test
    public void chineseVoiceInstalled_isUsable() {
        TextToSpeech engine = mock(TextToSpeech.class);
        when(engine.isLanguageAvailable(Locale.SIMPLIFIED_CHINESE))
                .thenReturn(TextToSpeech.LANG_COUNTRY_AVAILABLE);

        assertTrue(SystemTtsEngine.hasChineseVoice(engine));
    }

    @Test
    public void engineWithoutChineseData_isNotUsable() {
        TextToSpeech engine = mock(TextToSpeech.class);
        when(engine.isLanguageAvailable(Locale.SIMPLIFIED_CHINESE))
                .thenReturn(TextToSpeech.LANG_MISSING_DATA);

        assertFalse(SystemTtsEngine.hasChineseVoice(engine));
    }

    @Test
    public void engineThatDoesNotSupportChinese_isNotUsable() {
        TextToSpeech engine = mock(TextToSpeech.class);
        when(engine.isLanguageAvailable(Locale.SIMPLIFIED_CHINESE))
                .thenReturn(TextToSpeech.LANG_NOT_SUPPORTED);

        assertFalse(SystemTtsEngine.hasChineseVoice(engine));
    }
}
