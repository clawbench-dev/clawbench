package com.clawbench.app;

import android.content.Context;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import java.util.HashSet;
import java.util.Set;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for the native theme table.
 *
 * <p>{@link ThemePalette} is the single native source of truth for theme colours
 * (transcribed from {@code web/css/variables.css}). Both the status bar / splash
 * backdrop ({@code MainActivity.applyThemeColors}) and the version-mismatch
 * dialog ({@link WebStyleDialog}) resolve through it, so these tests pin the
 * values and the resolution rules that keep the two consistent.
 *
 * <p>Values are asserted for a few representative themes rather than all 36 —
 * the table's fidelity to variables.css is verified wholesale by the extraction
 * script, while these tests guard the lookup semantics and catch accidental
 * edits to the rows the dialog depends on.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class ThemePaletteTest {

    private Context appContext;

    @Before
    public void setUp() {
        appContext = RuntimeEnvironment.getApplication();
        appContext.getSharedPreferences(ThemePalette.PREFS_NAME, Context.MODE_PRIVATE)
                .edit().clear().commit();
    }

    // =====================================================
    // forId: resolution
    // =====================================================

    @Test
    public void forId_knownTheme_returnsThatTheme() {
        assertEquals("github-dark", ThemePalette.forId("github-dark").id);
        assertEquals("gruvbox-light", ThemePalette.forId("gruvbox-light").id);
    }

    @Test
    public void forId_nullEmptyAndUnknown_fallBackToGithubDark() {
        String fallback = ThemePalette.DEFAULT_THEME_ID;
        assertEquals(fallback, ThemePalette.forId(null).id);
        assertEquals(fallback, ThemePalette.forId("").id);
        assertEquals(fallback, ThemePalette.forId("not-a-theme").id);
    }

    @Test
    public void forId_isCaseSensitive_likeTheWebDataAttribute() {
        // data-theme="GitHub-Dark" would not match either; falling back is correct.
        assertEquals(ThemePalette.DEFAULT_THEME_ID, ThemePalette.forId("GitHub-Dark").id);
    }

    @Test
    public void forId_sameInstanceReturned_forSameId() {
        assertSame(ThemePalette.forId("dracula"), ThemePalette.forId("dracula"));
    }

    // =====================================================
    // current: reads the persisted theme ID
    // =====================================================

    @Test
    public void current_readsPersistedThemeId() {
        appContext.getSharedPreferences(ThemePalette.PREFS_NAME, Context.MODE_PRIVATE)
                .edit().putString(ThemePalette.KEY_THEME, "nord").commit();

        assertEquals("nord", ThemePalette.current(appContext).id);
    }

    @Test
    public void current_noPersistedTheme_fallsBackToGithubDark() {
        assertEquals(ThemePalette.DEFAULT_THEME_ID, ThemePalette.current(appContext).id);
    }

    // =====================================================
    // Table integrity
    // =====================================================

    @Test
    public void table_hasEveryWebTheme_noDuplicates() {
        ThemePalette[] all = ThemePalette.all();
        assertEquals("the web theme list has 36 themes", 36, all.length);

        Set<String> seen = new HashSet<>();
        for (ThemePalette p : all) {
            assertNotNull(p.id);
            assertTrue("duplicate theme id: " + p.id, seen.add(p.id));
        }
    }

    @Test
    public void table_fallbackRowIsGithubDark() {
        // The static initialiser enforces this; assert it so a reordering that
        // slips past class-init is still caught here.
        assertSame(ThemePalette.forId(ThemePalette.DEFAULT_THEME_ID),
                ThemePalette.forId("__unknown__"));
    }

    // =====================================================
    // Representative values (github-dark / github-light / gruvbox-light)
    // =====================================================

    @Test
    public void githubDark_matchesVariablesCss() {
        ThemePalette p = ThemePalette.forId("github-dark");

        assertEquals(0xFF0D1117, p.bgPrimary);
        assertEquals(0xFF161B22, p.bgSecondary);
        assertEquals(0xFF21262D, p.bgTertiary);
        assertEquals(0xFFC9D1D9, p.textPrimary);
        assertEquals(0xFF8B949E, p.textSecondary);
        assertEquals(0xFF6E7681, p.textMuted);
        assertEquals(0xFF59626D, p.textHint);
        assertEquals(0xFF58A6FF, p.accent);
        assertFalse(p.light);
    }

    @Test
    public void githubLight_matchesVariablesCss() {
        ThemePalette p = ThemePalette.forId("github-light");

        assertEquals(0xFFFFFFFF, p.bgPrimary);
        assertEquals(0xFFF8F9FA, p.bgSecondary);
        assertEquals(0xFFE9ECEF, p.bgTertiary);
        assertEquals(0xFF212529, p.textPrimary);
        assertEquals(0xFF495057, p.textSecondary);
        assertEquals(0xFF6C757D, p.textMuted);
        assertEquals(0xFF9AA4AF, p.textHint);
        assertEquals(0xFF4A90D9, p.accent);
        assertTrue(p.light);
    }

    @Test
    public void gruvboxLight_matchesVariablesCss() {
        ThemePalette p = ThemePalette.forId("gruvbox-light");

        assertEquals(0xFFFBF1C7, p.bgPrimary);
        assertEquals(0xFFF2E5BC, p.bgSecondary);
        assertEquals(0xFFEBDBB2, p.bgTertiary);
        assertEquals(0xFF3C3836, p.textPrimary);
        assertEquals(0xFF504945, p.textSecondary);
        assertEquals(0xFFAF3A03, p.accent);
        assertTrue(p.light);
    }

    @Test
    public void dracula_matchesVariablesCss() {
        ThemePalette p = ThemePalette.forId("dracula");

        assertEquals(0xFF21222C, p.bgSecondary);
        assertEquals(0xFF343746, p.bgTertiary);
        assertEquals(0xFFF8F8F2, p.textPrimary);
        assertEquals(0xFFB0B0C4, p.textSecondary);
        assertEquals(0xFFBD93F9, p.accent);
        assertFalse(p.light);
    }

    /**
     * text-hint is a distinct token from text-muted in variables.css. The old
     * MainActivity switch aliased the two, which is the kind of shortcut that
     * lets native and web drift; the table must keep them separate.
     */
    @Test
    public void textHint_isNotAliasedToTextMuted() {
        ThemePalette p = ThemePalette.forId("github-dark");

        assertNotEquals("text-hint must be its own token, not text-muted",
                p.textMuted, p.textHint);
    }

    @Test
    public void everyTheme_hasOpaqueColors() {
        for (ThemePalette p : ThemePalette.all()) {
            for (int c : new int[]{p.bgPrimary, p.bgSecondary, p.bgTertiary,
                    p.textPrimary, p.textSecondary, p.textMuted, p.textHint, p.accent}) {
                assertEquals("theme " + p.id + " has a translucent colour",
                        0xFF, (c >>> 24) & 0xFF);
            }
        }
    }

    /**
     * Text must be readable against the surface it sits on. This is a coarse
     * guard (the real check is that the values come from variables.css) but it
     * catches a row transcribed with light text on a light background.
     */
    @Test
    public void everyTheme_textContrastsWithBackground() {
        for (ThemePalette p : ThemePalette.all()) {
            int bgLuma = luma(p.bgSecondary);
            int textLuma = luma(p.textPrimary);
            int delta = Math.abs(bgLuma - textLuma);
            assertTrue("theme " + p.id + " has low text/background contrast ("
                            + bgLuma + " vs " + textLuma + ")",
                    delta >= 60);
        }
    }

    @Test
    public void lightFlag_agreesWithBackgroundLuminance() {
        for (ThemePalette p : ThemePalette.all()) {
            boolean darkBg = luma(p.bgPrimary) < 128;
            assertEquals("theme " + p.id + " has a light flag that disagrees with its bg",
                    darkBg, !p.light);
        }
    }

    // =====================================================
    // ids()
    // =====================================================

    @Test
    public void ids_listsEveryThemeInTableOrder() {
        String[] ids = ThemePalette.ids();
        assertEquals(ThemePalette.all().length, ids.length);
        assertEquals("alabaster", ids[0]);
        assertTrue(java.util.Arrays.asList(ids).contains("github-dark"));
        assertTrue(java.util.Arrays.asList(ids).contains("vitesse-light"));
    }

    // =====================================================
    // Independence from the WebView-pushed floating palette
    // =====================================================

    /**
     * The bug this class fixes: the dialog used to read FloatingThemeColors,
     * which the login page blanks. ThemePalette must be driven purely by the
     * theme ID, so a blanked floating palette cannot affect it.
     */
    @Test
    public void resolution_ignoresFloatingPaletteSlots() {
        appContext.getSharedPreferences(ThemePalette.PREFS_NAME, Context.MODE_PRIVATE).edit()
                .putString(ThemePalette.KEY_THEME, "gruvbox-light")
                .putString("theme_bg", "")
                .putString("theme_text", "")
                .putString("theme_text_secondary", "")
                .putString("theme_accent", "")
                .commit();

        ThemePalette p = ThemePalette.current(appContext);

        assertEquals("gruvbox-light", p.id);
        assertEquals(0xFFF2E5BC, p.bgSecondary);
        assertTrue("must not degrade to the github-dark floating fallback", p.light);
    }

    // =====================================================
    // Shared parser with FloatingThemeColors
    // =====================================================

    @Test
    public void tableColors_parseThroughTheSharedHexParser() {
        // ThemePalette delegates to FloatingThemeColors.parseColor so there is
        // one hex parser; verify the delegation holds for a known value.
        assertEquals(FloatingThemeColors.parseColor("#161b22", 0),
                ThemePalette.forId("github-dark").bgSecondary);
    }

    private static int luma(int argb) {
        int r = (argb >> 16) & 0xFF;
        int g = (argb >> 8) & 0xFF;
        int b = argb & 0xFF;
        return (r + g + b) / 3;
    }
}
