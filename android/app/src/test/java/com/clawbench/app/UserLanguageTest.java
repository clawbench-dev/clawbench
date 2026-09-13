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
}
