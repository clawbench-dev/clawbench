package com.clawbench.app;

import android.content.Context;
import android.content.SharedPreferences;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import static org.junit.Assert.*;

/**
 * Unit tests for UserLanguage — in-app language resolution for native UI owned
 * by BackgroundService (floating window). The language comes from the
 * user_language pref (synced by the Web frontend via setLanguage); when unset,
 * resources resolve against the system locale.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class UserLanguageTest {

    private Context context;
    private SharedPreferences prefs;

    @Before
    public void setUp() throws Exception {
        context = RuntimeEnvironment.getApplication();
        prefs = context.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE);
        prefs.edit().clear().commit();
    }

    @Test
    public void get_noPref_returnsNull() {
        assertNull(UserLanguage.get(context));
    }

    @Test
    public void get_zhPref_returnsZh() {
        prefs.edit().putString("user_language", "zh").commit();
        assertEquals("zh", UserLanguage.get(context));
    }

    @Test
    public void get_invalidPref_returnsNull() {
        prefs.edit().putString("user_language", "fr").commit();
        assertNull(UserLanguage.get(context));
    }

    @Test
    public void resolve_enPref_formattedStat_returnsEnglishWithCount() {
        prefs.edit().putString("user_language", "en").commit();
        assertEquals("Running 3", UserLanguage.resolve(context, R.string.floating_stat_running, 3));
    }

    @Test
    public void resolve_zhPref_formattedStat_returnsChineseWithCount() {
        prefs.edit().putString("user_language", "zh").commit();
        assertEquals("执行中 3", UserLanguage.resolve(context, R.string.floating_stat_running, 3));
    }

    @Test
    public void resolve_noPref_fallsBackToSystemLocale() {
        // No user_language pref — must fall back to the default resource set.
        assertNotNull(UserLanguage.resolve(context, R.string.floating_stat_running, 1));
    }

    @Test
    public void resolve_enPref_unformattedString_returnsEnglish() {
        // The no-format-args path: a plain string must resolve without going
        // through String.format.
        prefs.edit().putString("user_language", "en").commit();
        assertEquals("Refresh", UserLanguage.resolve(context, R.string.browser_refresh));
    }

    @Test
    public void resolve_zhPref_unformattedString_returnsChinese() {
        prefs.edit().putString("user_language", "zh").commit();
        assertEquals("刷新", UserLanguage.resolve(context, R.string.browser_refresh));
    }

    @Test
    public void resolve_noPref_unformattedString_fallsBackToSystemLocale() {
        assertNotNull(UserLanguage.resolve(context, R.string.browser_refresh));
    }

    @Test
    public void resolve_prefButLocalizedContextThrows_fallsBackToDefaultResources() {
        // A pref is set, so resolve() enters the localization branch; if
        // createConfigurationContext throws (a broken context in production),
        // it must fall back to the default resources instead of crashing.
        prefs.edit().putString("user_language", "en").commit();
        Context throwing = new ThrowingContext(context, true);
        assertEquals("Refresh", UserLanguage.resolve(throwing, R.string.browser_refresh));
    }

    @Test
    public void resolve_prefButLocalizedContextThrows_withFormatArgs_fallsBack() {
        // Same fallback, but through the formatted branch (String.format must
        // still be applied on the default resources).
        prefs.edit().putString("user_language", "en").commit();
        Context throwing = new ThrowingContext(context, true);
        assertEquals("Running 7", UserLanguage.resolve(throwing, R.string.floating_stat_running, 7));
    }

    @Test
    public void get_prefsAccessThrows_returnsNull() {
        // A context whose prefs are unreadable must degrade to "no preference"
        // rather than propagate the exception into the floating window.
        assertNull(UserLanguage.get(new ThrowingContext(context, false)));
    }

    @Test
    public void resolve_prefsAccessThrows_fallsBackToDefaultResources() {
        // When the pref cannot be read, resolve() must behave as "no language"
        // and use the default resources, for both the plain and formatted forms.
        Context throwing = new ThrowingContext(context, false);
        assertEquals("Refresh", UserLanguage.resolve(throwing, R.string.browser_refresh));
        assertEquals("Running 2", UserLanguage.resolve(throwing, R.string.floating_stat_running, 2));
    }

    /**
     * A Context that delegates to a real one but can fail the operations
     * UserLanguage relies on, so its defensive catch blocks are reachable
     * without mocking the whole Context surface.
     *
     * @param failLocalized when true, {@code createConfigurationContext} throws
     *                      (prefs still work, so the localization branch is
     *                      entered); when false, {@code getSharedPreferences}
     *                      throws instead.
     */
    private static final class ThrowingContext extends android.content.ContextWrapper {
        private final boolean failLocalized;

        ThrowingContext(Context base, boolean failLocalized) {
            super(base);
            this.failLocalized = failLocalized;
        }

        @Override
        public SharedPreferences getSharedPreferences(String name, int mode) {
            if (failLocalized) {
                return super.getSharedPreferences(name, mode);
            }
            throw new IllegalStateException("prefs unavailable");
        }

        @Override
        public Context createConfigurationContext(android.content.res.Configuration config) {
            if (failLocalized) {
                throw new IllegalStateException("localized context unavailable");
            }
            return super.createConfigurationContext(config);
        }
    }
}
