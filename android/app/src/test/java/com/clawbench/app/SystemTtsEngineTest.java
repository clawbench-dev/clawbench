package com.clawbench.app;

import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.content.pm.ResolveInfo;
import android.content.pm.ServiceInfo;
import android.speech.tts.TextToSpeech;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;

import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Locale;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyInt;
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
@RunWith(RobolectricTestRunner.class)
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

    // ── Locale selection ───────────────────────────────────────────────────
    //
    // The voice is chosen from the TEXT, not from a setting: a Chinese reply
    // read by an English voice is unintelligible, and vice versa. Each CJK block
    // is a separate range in Unicode, so all three must be recognized.

    @Test
    public void chineseText_picksSimplifiedChinese() {
        assertEquals(Locale.SIMPLIFIED_CHINESE, SystemTtsEngine.pickLocale("你好，世界"));
        assertEquals(Locale.SIMPLIFIED_CHINESE, SystemTtsEngine.pickLocale("结论：测试通过"));
    }

    @Test
    public void latinText_picksUsEnglish() {
        assertEquals(Locale.US, SystemTtsEngine.pickLocale("All tests passed"));
        assertEquals(Locale.US, SystemTtsEngine.pickLocale(""));
        assertEquals(Locale.US, SystemTtsEngine.pickLocale("café 42"));
    }

    @Test
    public void mixedText_prefersChinese() {
        // The reply is read in the language that has CJK in it; reading the
        // Chinese part with an English voice is the worse failure.
        assertEquals(Locale.SIMPLIFIED_CHINESE, SystemTtsEngine.pickLocale("build 通过"));
    }

    @Test
    public void containsCjk_coversEveryCjkBlock() {
        // Unified Ideographs, Extension A, and the compatibility block are three
        // separate ranges; a reply in a rare character must not be mistaken for
        // Latin and routed to the English voice.
        assertTrue(SystemTtsEngine.containsCjk("中"));   // U+4E2D, unified
        assertTrue(SystemTtsEngine.containsCjk("\u3400")); // extension A
        assertTrue(SystemTtsEngine.containsCjk("\uF900")); // compatibility
        assertFalse(SystemTtsEngine.containsCjk("abc123"));
        assertFalse(SystemTtsEngine.containsCjk(""));
        // Kana are their own blocks (U+3040–30FF), not Han. A reply written
        // entirely in kana is still routed to the English voice, which is the
        // documented limitation — this pins the boundary rather than claiming
        // kana support.
        assertFalse(SystemTtsEngine.containsCjk("あいうえお"));
    }

    // ── Engine description (client log) ────────────────────────────────────

    @Test
    public void describeEngines_reportsEveryLocaleProbe() {
        TextToSpeech engine = engineWith(
                TextToSpeech.LANG_COUNTRY_AVAILABLE,
                TextToSpeech.LANG_NOT_SUPPORTED,
                TextToSpeech.LANG_MISSING_DATA);
        when(engine.isLanguageAvailable(Locale.US)).thenReturn(TextToSpeech.LANG_AVAILABLE);
        when(engine.getEngines()).thenReturn(Collections.emptyList());

        String described = SystemTtsEngine.describeEngines(engine);
        assertTrue(described, described.contains("zhCN="));
        assertTrue(described, described.contains("zh="));
        assertTrue(described, described.contains("zhTW="));
        assertTrue(described, described.contains("en="));
        assertTrue(described, described.contains("engines="));
    }

    @Test
    public void describeEngines_neverThrowsWhenTheEngineIsBroken() {
        // This runs inside the init success path; a throw here would abort init
        // and leave the UI stuck on "initializing".
        TextToSpeech engine = mock(TextToSpeech.class);
        when(engine.isLanguageAvailable(any(Locale.class))).thenThrow(new IllegalStateException("binder died"));

        String described = SystemTtsEngine.describeEngines(engine);
        assertNotNull(described);
        assertTrue(described, described.startsWith("describe failed"));
    }

    // ── Engine package discovery ───────────────────────────────────────────

    private static ResolveInfo serviceOf(String packageName) {
        ResolveInfo info = new ResolveInfo();
        info.serviceInfo = new ServiceInfo();
        info.serviceInfo.packageName = packageName;
        return info;
    }

    @Test
    public void queryEnginePackages_listsEachPackageOnce() {
        // Some ROMs register several TTS services under one package; trying the
        // same package twice just doubles the init timeout.
        Context context = mock(Context.class);
        PackageManager pm = mock(PackageManager.class);
        when(context.getPackageManager()).thenReturn(pm);
        when(pm.queryIntentServices(any(Intent.class), anyInt())).thenReturn(Arrays.asList(
                serviceOf("com.google.android.tts"),
                serviceOf("com.google.android.tts"),
                serviceOf("com.samsung.SMT")));

        List<String> packages = SystemTtsEngine.queryEnginePackages(context);
        assertEquals(Arrays.asList("com.google.android.tts", "com.samsung.SMT"), packages);
    }

    @Test
    public void queryEnginePackages_skipsMalformedEntries() {
        // A ResolveInfo with no serviceInfo cannot be asked for by package name;
        // keeping it would make the retry loop dial a null package.
        Context context = mock(Context.class);
        PackageManager pm = mock(PackageManager.class);
        when(context.getPackageManager()).thenReturn(pm);
        ResolveInfo noService = new ResolveInfo();
        ResolveInfo noPackage = new ResolveInfo();
        noPackage.serviceInfo = new ServiceInfo();
        when(pm.queryIntentServices(any(Intent.class), anyInt())).thenReturn(Arrays.asList(
                noService, noPackage, null, serviceOf("com.example.tts")));

        assertEquals(Collections.singletonList("com.example.tts"),
                SystemTtsEngine.queryEnginePackages(context));
    }

    @Test
    public void queryEnginePackages_isEmptyWhenTheSystemListsNothing() {
        // Empty is a meaningful answer (no engine at all, or the manifest lost
        // its TTS_SERVICE query) — it must not throw.
        Context context = mock(Context.class);
        PackageManager pm = mock(PackageManager.class);
        when(context.getPackageManager()).thenReturn(pm);
        when(pm.queryIntentServices(any(Intent.class), anyInt())).thenReturn(null);

        assertTrue(SystemTtsEngine.queryEnginePackages(context).isEmpty());
    }

    @Test
    public void queryEnginePackages_survivesAPackageManagerFailure() {
        Context context = mock(Context.class);
        PackageManager pm = mock(PackageManager.class);
        when(context.getPackageManager()).thenReturn(pm);
        when(pm.queryIntentServices(any(Intent.class), anyInt()))
                .thenThrow(new SecurityException("no TTS_SERVICE query"));

        assertTrue(SystemTtsEngine.queryEnginePackages(context).isEmpty());
    }

    // ── Language fallback for Latin text ──────────────────────────────────

    @Test
    public void fallback_usesEnglishForLatinText() {
        // The candidate list is chosen from the text: an English reply must not
        // be retried against Chinese variants.
        TextToSpeech engine = mock(TextToSpeech.class);
        when(engine.setLanguage(Locale.ENGLISH)).thenReturn(TextToSpeech.LANG_AVAILABLE);

        assertTrue(SystemTtsEngine.applyLanguageFallback(engine, "hello"));
        verify(engine).setLanguage(Locale.ENGLISH);
    }

    // ── Public API guards (no engine needed) ───────────────────────────────

    @Test
    public void speak_rejectsEmptyInputWithoutTouchingTheEngine() {
        Context context = mock(Context.class);
        when(context.getApplicationContext()).thenReturn(context);
        SystemTtsEngine engine = new SystemTtsEngine(context, null);

        assertEquals("bad_request", engine.speak("", "id-1"));
        assertEquals("bad_request", engine.speak("   ", "id-1"));
        assertEquals("bad_request", engine.speak("你好", ""));
        assertEquals("bad_request", engine.speak("你好", null));
        assertEquals("bad_request", engine.speak(null, "id-1"));
    }

    @Test
    public void status_isInitializingBeforeAnythingIsSpoken() {
        Context context = mock(Context.class);
        when(context.getApplicationContext()).thenReturn(context);
        when(context.getPackageManager()).thenReturn(mock(PackageManager.class));

        SystemTtsEngine engine = new SystemTtsEngine(context, null);
        assertEquals(SystemTtsEngine.STATUS_INITIALIZING, engine.status());
    }
}
