package com.clawbench.app;

import android.content.Context;
import android.content.SharedPreferences;

/**
 * Reads the persisted theme palette for the floating status window.
 *
 * The WebView sends a resolved palette (bg / text / textSecondary / accent)
 * through the setTheme bridge; this class is the read-back path used by
 * FloatingStatusView (capsule) and FloatingStatusPanelView (panel) at
 * construction time. When no colors have been persisted yet the github-dark
 * defaults are returned so the floating window never renders with undefined
 * colors.
 *
 * parseColor is a pure function (no framework deps) so it is unit-testable
 * with plain JUnit.
 */
public final class FloatingThemeColors {

    private FloatingThemeColors() {
    }

    private static final String PREFS_NAME = "clawbench_prefs";
    private static final String KEY_THEME_BG = "theme_bg";
    private static final String KEY_THEME_TEXT = "theme_text";
    private static final String KEY_THEME_TEXT_SECONDARY = "theme_text_secondary";
    private static final String KEY_THEME_ACCENT = "theme_accent";

    // github-dark defaults (fallback when nothing has been persisted).
    static final int DEFAULT_BG = 0xFF161B22;
    static final int DEFAULT_TEXT = 0xFFC9D1D9;
    static final int DEFAULT_TEXT_SECONDARY = 0xFF8B949E;
    static final int DEFAULT_ACCENT = 0xFF58A6FF;

    /**
     * Read the persisted palette as ARGB ints.
     *
     * @return int[]{bg, text, textSecondary, accent}
     */
    public static int[] get(Context context) {
        SharedPreferences prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);
        return new int[]{
                parseColor(prefs.getString(KEY_THEME_BG, ""), DEFAULT_BG),
                parseColor(prefs.getString(KEY_THEME_TEXT, ""), DEFAULT_TEXT),
                parseColor(prefs.getString(KEY_THEME_TEXT_SECONDARY, ""), DEFAULT_TEXT_SECONDARY),
                parseColor(prefs.getString(KEY_THEME_ACCENT, ""), DEFAULT_ACCENT),
        };
    }

    /**
     * Parse a "#RRGGBB" hex string into an opaque ARGB int (0xFFRRGGBB).
     * Pure: no framework deps. Invalid or null input returns the default.
     */
    public static int parseColor(String hex, int defaultValue) {
        if (hex == null || hex.length() != 7 || hex.charAt(0) != '#') {
            return defaultValue;
        }
        try {
            return 0xFF000000 | (int) Long.parseLong(hex.substring(1), 16);
        } catch (NumberFormatException e) {
            return defaultValue;
        }
    }

    /**
     * Derive a visible border color from a background color by nudging each
     * RGB channel away from the background's own luminance: dark backgrounds
     * get a lighter border, light backgrounds get a darker one. The border
     * stays in the same hue family as the background (a "theme-tinted" edge)
     * while remaining distinguishable from it. Pure: no framework deps.
     */
    public static int borderColorFromBackground(int bg) {
        int r = (bg >> 16) & 0xFF;
        int g = (bg >> 8) & 0xFF;
        int b = bg & 0xFF;
        int luma = (r + g + b) / 3;
        boolean dark = luma < 128;
        // Mix 40% toward white (dark bg) or 40% toward black (light bg).
        float mix = dark ? 0.40f : -0.40f;
        int nr = channel(r, mix);
        int ng = channel(g, mix);
        int nb = channel(b, mix);
        return 0xFF000000 | (nr << 16) | (ng << 8) | nb;
    }

    private static int channel(int v, float mix) {
        if (mix >= 0) {
            return Math.min(255, Math.round(v + (255 - v) * mix));
        }
        return Math.max(0, Math.round(v * (1 + mix)));
    }
}
