package com.clawbench.app;

import android.app.Activity;
import android.app.Application;
import android.app.Dialog;
import android.content.Context;
import android.content.res.TypedArray;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.view.ContextThemeWrapper;
import android.view.Window;
import android.view.WindowManager;
import android.widget.TextView;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.Robolectric;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import java.util.concurrent.atomic.AtomicBoolean;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for the web-style native dialog card.
 *
 * <p>The card is the native counterpart of
 * {@code web/src/components/common/DialogOverlay.vue}; these tests pin the
 * parity-critical details (width cap, text wiring, click routing, theme colors)
 * so the two do not drift.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class WebStyleDialogTest {

    private Application appContext;

    @Before
    public void setUp() {
        appContext = RuntimeEnvironment.getApplication();
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .edit().clear().commit();
    }

    // =====================================================
    // cardWidthPx: min(320dp, screen - 2*20dp)
    // =====================================================

    @Test
    public void cardWidth_wideScreen_cappedAt320dp() {
        // A tablet must not stretch the card beyond the web's 320px cap.
        assertEquals(dp(320), WebStyleDialog.cardWidthPx(appContext, dp(1000)));
    }

    @Test
    public void cardWidth_narrowScreen_shrinksToFitSideMargins() {
        // On a 360dp-wide phone the card must leave the 20dp margins on both sides.
        assertEquals(dp(360) - 2 * dp(20), WebStyleDialog.cardWidthPx(appContext, dp(360)));
    }

    @Test
    public void cardWidth_exactlyAtCap_staysAtCap() {
        // 360dp screen == 320dp cap + 2*20dp margins: the boundary must not overflow.
        assertEquals(dp(320), WebStyleDialog.cardWidthPx(appContext, dp(360)));
    }

    @Test
    public void cardWidth_nonsenseScreenWidth_fallsBackToCap() {
        assertEquals("a zero/unknown screen width must not produce a 0-width card",
                dp(320), WebStyleDialog.cardWidthPx(appContext, 0));
        assertEquals(dp(320), WebStyleDialog.cardWidthPx(appContext, -1));
    }

    @Test
    public void cardWidth_veryNarrowScreen_neverNonPositive() {
        // Smaller than the margins themselves — must still yield a usable width.
        assertTrue(WebStyleDialog.cardWidthPx(appContext, dp(10)) > 0);
    }

    // =====================================================
    // apply: texts, visibility and click routing
    // =====================================================

    @Test
    public void apply_wiresTextsAndRoutesClicks() {
        View content = inflate();
        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));
        AtomicBoolean confirmed = new AtomicBoolean(false);
        AtomicBoolean cancelled = new AtomicBoolean(false);

        WebStyleDialog.apply(content, style, "版本不一致", "正文内容",
                "下载 APK", "强制跳过", null,
                () -> confirmed.set(true), () -> cancelled.set(true));

        assertEquals("版本不一致", text(content, R.id.webDialogTitle));
        assertEquals("正文内容", text(content, R.id.webDialogMessage));
        assertEquals("下载 APK", text(content, R.id.webDialogConfirm));
        assertEquals("强制跳过", text(content, R.id.webDialogCancel));

        content.findViewById(R.id.webDialogConfirm).performClick();
        assertTrue("confirm click must run the confirm action", confirmed.get());
        assertTrue("cancel must not fire on a confirm click", !cancelled.get());

        content.findViewById(R.id.webDialogCancel).performClick();
        assertTrue("cancel click must run the cancel action", cancelled.get());
    }

    @Test
    public void apply_blankTitle_hidesTitleRow() {
        View content = inflate();
        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        WebStyleDialog.apply(content, style, "", "body", "ok", "no", null, null, null);

        assertEquals("an empty title must collapse the whole row, as the web does",
                View.GONE, content.findViewById(R.id.webDialogTitleRow).getVisibility());
    }

    @Test
    public void apply_nullTitleAndMessage_doNotThrow() {
        View content = inflate();
        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        WebStyleDialog.apply(content, style, null, null, null, null, null, null, null);

        assertEquals(View.GONE, content.findViewById(R.id.webDialogTitleRow).getVisibility());
        assertEquals("", text(content, R.id.webDialogMessage));
    }

    @Test
    public void apply_sizesCardFromStyle() {
        View content = inflate();
        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        WebStyleDialog.apply(content, style, "t", "m", "ok", "no", null, null, null);

        View card = content.findViewById(R.id.webDialogCard);
        assertEquals("the card must take the resolved (capped) width",
                style.cardWidthPx, card.getLayoutParams().width);
        assertNotNull("the card must carry a themed background",
                card.getBackground());
    }

    // =====================================================
    // buildStyle: colors come from the persisted theme ID
    // =====================================================

    @Test
    public void buildStyle_usesPersistedThemeId() {
        // Simulate the app having persisted dracula. The dialog must resolve the
        // real dracula values from the theme table, not a derived approximation.
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE).edit()
                .putString("theme_base", "dracula")
                .commit();

        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        assertEquals("card bg must be dracula --bg-secondary", 0xFF21222C, style.cardBg);
        assertEquals("title text must be dracula --text-primary", 0xFFF8F8F2, style.textPrimary);
        assertEquals("message text must be dracula --text-secondary", 0xFFB0B0C4, style.textSecondary);
        assertEquals("cancel button must be the real dracula --bg-tertiary",
                0xFF343746, style.cancelBg);
        assertEquals(0xFFBD93F9, style.accent);
        assertEquals("the title chip is the accent at 12% alpha",
                FloatingThemeColors.accentTint(0xFFBD93F9), style.iconChipBg);
    }

    @Test
    public void buildStyle_withoutPersistedTheme_usesGithubDarkDefaults() {
        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        ThemePalette fallback = ThemePalette.forId(ThemePalette.DEFAULT_THEME_ID);
        assertEquals(fallback.bgSecondary, style.cardBg);
        assertEquals(fallback.textPrimary, style.textPrimary);
        assertEquals(fallback.accent, style.accent);
    }

    @Test
    public void buildStyle_unknownThemeId_fallsBackToGithubDark() {
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE).edit()
                .putString("theme_base", "no-such-theme")
                .commit();

        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        ThemePalette fallback = ThemePalette.forId(ThemePalette.DEFAULT_THEME_ID);
        assertEquals(fallback.bgSecondary, style.cardBg);
        assertEquals(fallback.textPrimary, style.textPrimary);
    }

    /**
     * The regression this whole palette refactor exists for: on cold start the
     * login page calls the 1-arg setTheme(theme) bridge overload, which blanks
     * the FloatingThemeColors slots. The dialog must not read that palette — it
     * used to, and fell back to github-dark, painting a dark card over a light
     * splash backdrop.
     */
    @Test
    public void buildStyle_ignoresBlankedFloatingPalette() {
        // Light theme selected, but the bridge-blanked palette (all empty) is what
        // FloatingThemeColors would read. This is the post-login-page state.
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE).edit()
                .putString("theme_base", "gruvbox-light")
                .putString("theme_bg", "")
                .putString("theme_text", "")
                .putString("theme_text_secondary", "")
                .putString("theme_accent", "")
                .commit();

        // Sanity: the floating palette really did degrade to github-dark here.
        assertEquals("precondition: the blanked floating palette is github-dark",
                0xFF161B22, FloatingThemeColors.get(appContext)[0]);

        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        assertEquals("the card must follow gruvbox-light, not the blanked palette",
                0xFFF2E5BC, style.cardBg);
        assertEquals(0xFF3C3836, style.textPrimary);
        assertTrue("a light theme must produce a light card", brightness(style.cardBg) > 128);
    }

    @Test
    public void buildStyle_lightAndDarkThemes_differInCardBackground() {
        WebStyleDialog.Style dark = styleForTheme("github-dark");
        WebStyleDialog.Style light = styleForTheme("github-light");

        assertTrue("dark theme card must be dark", brightness(dark.cardBg) < 128);
        assertTrue("light theme card must be light", brightness(light.cardBg) > 128);
        assertTrue("dark theme must use light text", brightness(dark.textPrimary) > 128);
        assertTrue("light theme must use dark text", brightness(light.textPrimary) < 128);
    }

    @Test
    public void buildStyle_radii_matchWebTokens() {
        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        // --radius-lg 14px for the card, --radius-sm 6px for the chip and buttons.
        assertEquals(dp(14), style.cardRadiusPx);
        assertEquals(dp(6), style.iconChipRadiusPx);
        assertEquals(dp(6), style.buttonRadiusPx);
    }

    @Test
    public void buildStyle_cardHasElevation_forShadowSeparation() {
        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        assertTrue("the card needs elevation to mirror the web's box-shadow",
                style.cardElevationPx > 0);
    }

    // =====================================================
    // Window theme: full-screen scrim
    // =====================================================

    /**
     * The parent dialog theme sets windowIsFloating=true, which makes PhoneWindow
     * force the window to WRAP_CONTENT — the scrim then covers only a card-sized
     * band instead of the screen. The override must stay false so the window is
     * full-screen like the web's `position:fixed; inset:0` overlay.
     */
    @Test
    public void windowTheme_isNotFloating_soScrimCoversScreen() {
        Context themed = new ContextThemeWrapper(appContext, R.style.Theme_ClawBench_WebDialog);
        TypedArray a = themed.obtainStyledAttributes(
                new int[]{android.R.attr.windowIsFloating});
        try {
            assertFalse("windowIsFloating must be false or the scrim is card-sized",
                    a.getBoolean(0, true));
        } finally {
            a.recycle();
        }
    }

    @Test
    public void windowTheme_isTranslucentWithTransparentBackground() {
        Context themed = new ContextThemeWrapper(appContext, R.style.Theme_ClawBench_WebDialog);
        TypedArray a = themed.obtainStyledAttributes(
                new int[]{android.R.attr.windowIsTranslucent, android.R.attr.windowBackground});
        try {
            assertTrue("the framework dialog panel must not be drawn",
                    a.getBoolean(0, false));
            assertNotNull("windowBackground must be transparent, not the dialog panel",
                    a.getDrawable(1));
        } finally {
            a.recycle();
        }
    }

    // =====================================================
    // Card elevation is applied to the view
    // =====================================================

    @Test
    public void apply_setsCardElevation() {
        View content = inflate();
        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        WebStyleDialog.apply(content, style, "t", "m", "ok", "no", null, null, null);

        View card = content.findViewById(R.id.webDialogCard);
        assertEquals("the card must carry the resolved elevation",
                (float) style.cardElevationPx, card.getElevation(), 0.01f);
    }

    // =====================================================
    // show(): the real window geometry
    // =====================================================

    /**
     * End-to-end check of the scrim fix on a real (Robolectric) Activity: the
     * dialog window must be full-screen, otherwise the #73000000 scrim only
     * covers a card-sized band and the card stops reading as sitting on a dimmed
     * backdrop.
     *
     * <p>Both halves of the fix are pinned here. The MATCH_PARENT attributes come
     * from the explicit {@code setLayout} call, and the LAYOUT_IN_SCREEN /
     * LAYOUT_INSET_DECOR flags only survive when the theme sets
     * {@code windowIsFloating=false} — a floating window is forced to
     * WRAP_CONTENT and drops those flags, which is what shrank the scrim.
     */
    @Test
    public void show_windowIsFullScreen_soScrimCoversDisplay() {
        Activity activity = Robolectric.buildActivity(Activity.class)
                .setup().get();

        Dialog dialog = WebStyleDialog.show(activity, "版本不一致", "正文",
                "下载 APK", "强制跳过", null, null);
        try {
            assertNotNull("the dialog must be shown", dialog);
            Window window = dialog.getWindow();
            assertNotNull(window);

            WindowManager.LayoutParams lp = window.getAttributes();
            assertEquals("the scrim must span the full width",
                    ViewGroup.LayoutParams.MATCH_PARENT, lp.width);
            assertEquals("the scrim must span the full height",
                    ViewGroup.LayoutParams.MATCH_PARENT, lp.height);
            assertTrue("the window must be laid out in the screen, not as a floating box",
                    (lp.flags & WindowManager.LayoutParams.FLAG_LAYOUT_IN_SCREEN) != 0);
        } finally {
            if (dialog != null && dialog.isShowing()) {
                dialog.dismiss();
            }
        }
    }

    @Test
    public void show_stylesTheRealDialogViews() {
        Activity activity = Robolectric.buildActivity(Activity.class).setup().get();
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE).edit()
                .putString("theme_base", "github-light")
                .commit();

        Dialog dialog = WebStyleDialog.show(activity, "版本不一致", "正文",
                "下载 APK", "强制跳过", null, null);
        try {
            View content = dialog.getWindow().getDecorView();
            View card = content.findViewById(R.id.webDialogCard);
            assertNotNull("the card must be present in the shown window", card);
            assertTrue("the shown card must carry the elevation",
                    card.getElevation() > 0);
            assertEquals("版本不一致",
                    ((TextView) content.findViewById(R.id.webDialogTitle)).getText().toString());
        } finally {
            if (dialog != null && dialog.isShowing()) {
                dialog.dismiss();
            }
        }
    }

    // =====================================================
    // Theme-derived helper colors (FloatingThemeColors)
    // =====================================================

    @Test
    public void accentTint_keepsHueAndAppliesTwelvePercentAlpha() {
        int tint = FloatingThemeColors.accentTint(0xFF58A6FF);

        assertEquals("RGB must be the accent untouched", 0x58A6FF, tint & 0x00FFFFFF);
        int alpha = (tint >>> 24) & 0xFF;
        assertTrue("alpha must be ~12% (31/255), got " + alpha,
                Math.abs(alpha - 31) <= 1);
    }

    // =====================================================
    // Helpers
    // =====================================================

    private WebStyleDialog.Style styleForTheme(String themeId) {
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE).edit()
                .putString("theme_base", themeId)
                .commit();
        return WebStyleDialog.buildStyle(appContext, dp(360));
    }

    private View inflate() {
        return LayoutInflater.from(appContext).inflate(R.layout.dialog_web_card, null);
    }

    private static String text(View root, int id) {
        TextView tv = root.findViewById(id);
        return tv.getText().toString();
    }

    private int dp(int value) {
        return Math.round(android.util.TypedValue.applyDimension(
                android.util.TypedValue.COMPLEX_UNIT_DIP, value,
                appContext.getResources().getDisplayMetrics()));
    }

    private static int brightness(int argb) {
        int r = (argb >> 16) & 0xFF;
        int g = (argb >> 8) & 0xFF;
        int b = argb & 0xFF;
        return (r + g + b) / 3;
    }
}
