package com.clawbench.app;

import android.app.Application;
import android.content.Context;
import android.graphics.drawable.GradientDrawable;
import android.view.LayoutInflater;
import android.view.View;
import android.widget.TextView;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import java.util.concurrent.atomic.AtomicBoolean;

import static org.junit.Assert.assertEquals;
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
    // buildStyle: colors come from the persisted theme palette
    // =====================================================

    @Test
    public void buildStyle_usesPersistedPalette() {
        // Simulate the WebView having pushed a dracula palette.
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE).edit()
                .putString("theme_bg", "#21222c")
                .putString("theme_text", "#f8f8f2")
                .putString("theme_text_secondary", "#6272a4")
                .putString("theme_accent", "#bd93f9")
                .commit();

        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        assertEquals(0xFF21222C, style.cardBg);
        assertEquals(0xFFF8F8F2, style.textPrimary);
        assertEquals(0xFF6272A4, style.textSecondary);
        assertEquals(0xFFBD93F9, style.accent);
        assertEquals("the title chip is the accent at 12% alpha",
                FloatingThemeColors.accentTint(0xFFBD93F9), style.iconChipBg);
    }

    @Test
    public void buildStyle_withoutPersistedTheme_usesGithubDarkDefaults() {
        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        assertEquals(FloatingThemeColors.DEFAULT_BG, style.cardBg);
        assertEquals(FloatingThemeColors.DEFAULT_TEXT, style.textPrimary);
        assertEquals(FloatingThemeColors.DEFAULT_ACCENT, style.accent);
    }

    @Test
    public void buildStyle_radii_matchWebTokens() {
        WebStyleDialog.Style style = WebStyleDialog.buildStyle(appContext, dp(360));

        // --radius-lg 14px for the card, --radius-sm 6px for the chip and buttons.
        assertEquals(dp(14), style.cardRadiusPx);
        assertEquals(dp(6), style.iconChipRadiusPx);
        assertEquals(dp(6), style.buttonRadiusPx);
    }

    // =====================================================
    // Theme-derived helper colors (FloatingThemeColors)
    // =====================================================

    @Test
    public void tertiaryColor_darkTheme_staysInBackgroundFamily() {
        // github-dark: --bg-secondary #161b22 with --text-primary #c9d1d9.
        int tertiary = FloatingThemeColors.tertiaryColor(0xFF161B22, 0xFFC9D1D9);

        assertTrue("bg-tertiary must be lighter than bg-secondary on a dark theme",
                brightness(tertiary) > brightness(0xFF161B22));
        assertTrue("the nudge must stay subtle, not become a light gray",
                brightness(tertiary) < brightness(0xFFC9D1D9));
    }

    @Test
    public void tertiaryColor_lightTheme_darkens() {
        // github-light: --bg-secondary #f8f9fa with --text-primary #212529.
        int tertiary = FloatingThemeColors.tertiaryColor(0xFFF8F9FA, 0xFF212529);

        assertTrue("bg-tertiary must be darker than bg-secondary on a light theme",
                brightness(tertiary) < brightness(0xFFF8F9FA));
    }

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
