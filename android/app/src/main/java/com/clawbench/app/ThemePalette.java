package com.clawbench.app;

import android.content.Context;
import android.content.SharedPreferences;

/**
 * Resolved colours for a named theme, keyed by the persisted theme <em>ID</em>.
 *
 * <p>This is the single native source of truth for theme colours. It exists
 * because there used to be two: {@link MainActivity#applyThemeColors} mapped the
 * theme ID to colours for the status bar and splash backdrop, while
 * {@link WebStyleDialog} read the WebView-pushed palette from
 * {@link FloatingThemeColors}. Those two disagreed on cold start — the login
 * page calls the 1-arg {@code setTheme(theme)} bridge overload
 * ({@code MainActivity.WebAppInterface#setTheme(String)}), which blanks the
 * persisted colour slots, so the dialog silently fell back to github-dark and
 * painted a dark card over a light backdrop. Reading the theme ID here removes
 * the second source entirely.
 *
 * <p>The table is transcribed from {@code web/css/variables.css}, which is the
 * authoritative theme definition, and is verified against it field by field.
 * The per-theme colours the dialog needs (bg-secondary, bg-tertiary,
 * text-secondary) are therefore the real theme values rather than values
 * derived from a persisted palette.
 *
 * <p>Values are stored as hex strings and parsed once per instance, so the
 * lookup stays pure and unit-testable without a Context.
 */
final class ThemePalette {

    /** SharedPreferences key holding the resolved theme ID (see MainActivity.KEY_THEME). */
    static final String PREFS_NAME = "clawbench_prefs";
    static final String KEY_THEME = "theme_base";

    /** Theme used when nothing is persisted or the ID is unknown. */
    static final String DEFAULT_THEME_ID = "github-dark";

    final String id;
    final int bgPrimary;
    final int bgSecondary;
    final int bgTertiary;
    final int textPrimary;
    final int textSecondary;
    final int textMuted;
    final int textHint;
    final int accent;
    final boolean light;

    private ThemePalette(String id, String bgPrimary, String bgSecondary, String bgTertiary,
                         String textPrimary, String textSecondary, String textMuted,
                         String textHint, String accent, boolean light) {
        this.id = id;
        this.bgPrimary = parse(bgPrimary);
        this.bgSecondary = parse(bgSecondary);
        this.bgTertiary = parse(bgTertiary);
        this.textPrimary = parse(textPrimary);
        this.textSecondary = parse(textSecondary);
        this.textMuted = parse(textMuted);
        this.textHint = parse(textHint);
        this.accent = parse(accent);
        this.light = light;
    }

    /**
     * Resolve the palette for a theme ID. Null, empty or unknown IDs resolve to
     * {@link #DEFAULT_THEME_ID}, mirroring the switch default this replaced.
     */
    static ThemePalette forId(String themeId) {
        if (themeId != null) {
            for (ThemePalette p : TABLE) {
                if (p.id.equals(themeId)) {
                    return p;
                }
            }
        }
        return FALLBACK;
    }

    /** Resolve the palette for the persisted theme ID. */
    static ThemePalette current(Context context) {
        SharedPreferences prefs =
                context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);
        return forId(prefs.getString(KEY_THEME, DEFAULT_THEME_ID));
    }

    /** Every known theme ID, in table order. Test/verification hook. */
    static String[] ids() {
        String[] out = new String[TABLE.length];
        for (int i = 0; i < TABLE.length; i++) {
            out[i] = TABLE[i].id;
        }
        return out;
    }

    /** All palettes, for drift verification against the web theme table. */
    static ThemePalette[] all() {
        return TABLE.clone();
    }

    /**
     * Parse "#rrggbb" through {@link FloatingThemeColors#parseColor}, so both
     * palettes share one hex parser. Table values are always valid, so the
     * fallback is never used in practice.
     */
    private static int parse(String hex) {
        return FloatingThemeColors.parseColor(hex, 0xFF000000);
    }

    /**
     * Theme table transcribed from {@code web/css/variables.css}
     * ({@code [data-theme="<id>"]} blocks), field order:
     * id, bg-primary, bg-secondary, bg-tertiary, text-primary, text-secondary,
     * text-muted, text-hint, accent-color, light.
     */
    private static final ThemePalette[] TABLE = {
            new ThemePalette("alabaster",            "#f7f7f7", "#eeeeee", "#e2e2e2", "#272727", "#4a4a4a", "#777777", "#959595", "#0086b3", true),
            new ThemePalette("ayu-dark",             "#0a0e14", "#0d1017", "#131721", "#b3b1ad", "#8f8f8a", "#626a73", "#4d545d", "#e6b450", false),
            new ThemePalette("ayu-light",            "#fafafa", "#f3f3f3", "#e8e8e8", "#5c6166", "#777c81", "#a0a5aa", "#b8bdc2", "#ff9940", true),
            new ThemePalette("bluloco-dark",         "#1d212c", "#242936", "#2e3440", "#e2e8f0", "#c0c9d6", "#94a3b8", "#7c8aa0", "#52a5ff", false),
            new ThemePalette("bluloco-light",        "#f7f9fc", "#edf1f7", "#e0e6ee", "#292d3e", "#434857", "#718096", "#8d98ab", "#2b7bda", true),
            new ThemePalette("catppuccin-latte",     "#eff1f5", "#e6e9ef", "#ccd0da", "#4c4f69", "#5c5f77", "#8c8fa1", "#acb0be", "#1e66f5", true),
            new ThemePalette("catppuccin-mocha",     "#1e1e2e", "#181825", "#313244", "#cdd6f4", "#bac2de", "#6c7086", "#585b70", "#89b4fa", false),
            new ThemePalette("dark-plus",            "#1e1e1e", "#252526", "#2d2d2d", "#d4d4d4", "#b0b0b0", "#8c8c8c", "#757575", "#0e639c", false),
            new ThemePalette("dracula",              "#282a36", "#21222c", "#343746", "#f8f8f2", "#b0b0c4", "#6272a4", "#4d5b83", "#bd93f9", false),
            new ThemePalette("everforest-dark",      "#1e2326", "#22282b", "#2d353b", "#d3c6aa", "#a6ad96", "#859289", "#6a766e", "#a7c080", false),
            new ThemePalette("everforest-light",     "#fdf6e3", "#f2e9d0", "#e5dcc0", "#5c6a72", "#6f7b80", "#939f91", "#a8b3a4", "#7a8478", true),
            new ThemePalette("github-dark",          "#0d1117", "#161b22", "#21262d", "#c9d1d9", "#8b949e", "#6e7681", "#59626d", "#58a6ff", false),
            new ThemePalette("github-light",         "#ffffff", "#f8f9fa", "#e9ecef", "#212529", "#495057", "#6c757d", "#9aa4af", "#4a90d9", true),
            new ThemePalette("gruvbox-dark",         "#282828", "#1d2021", "#3c3836", "#ebdbb2", "#d5c4a1", "#9d9188", "#7c6f64", "#fe8019", false),
            new ThemePalette("gruvbox-light",        "#fbf1c7", "#f2e5bc", "#ebdbb2", "#3c3836", "#504945", "#928374", "#a89984", "#af3a03", true),
            new ThemePalette("high-contrast-dark",   "#000000", "#0a0a0a", "#1a1a1a", "#ffffff", "#e0e0e0", "#a0a0a0", "#808080", "#00ccff", false),
            new ThemePalette("high-contrast-light",  "#ffffff", "#f5f5f5", "#e0e0e0", "#000000", "#1a1a1a", "#444444", "#666666", "#0055cc", true),
            new ThemePalette("kanagawa",             "#1f1f28", "#16161d", "#2a2a37", "#dcd7ba", "#c8c093", "#727169", "#5c5a52", "#7e9cd8", false),
            new ThemePalette("light-modern",         "#fafafa", "#f3f3f3", "#e9e9e9", "#1a1a1a", "#444446", "#717175", "#929298", "#0b57d0", true),
            new ThemePalette("light-plus",           "#ffffff", "#f5f5f5", "#e8e8e8", "#000000", "#3b3b3b", "#6f6f6f", "#8f8f8f", "#0065bf", true),
            new ThemePalette("material-darker",      "#212121", "#282828", "#303030", "#eeffff", "#c4d7d8", "#92a6a7", "#7a8b8c", "#80cbc4", false),
            new ThemePalette("material-lighter",     "#fafafa", "#f0f0f0", "#e4e4e4", "#212121", "#494949", "#9e9e9e", "#b0b0b0", "#1976d2", true),
            new ThemePalette("monokai",              "#272822", "#2e2f29", "#373832", "#f8f8f2", "#c8c8b4", "#a6a68e", "#8f8f78", "#ae81ff", false),
            new ThemePalette("night-owl",            "#011627", "#001122", "#0b253a", "#d6deeb", "#a0b4cc", "#5f7e97", "#45677e", "#82aaff", false),
            new ThemePalette("nord",                 "#171e27", "#202833", "#2a3441", "#e6ecf4", "#aab8c9", "#7f8fa5", "#5f6f85", "#6cb2f0", false),
            new ThemePalette("nord-light",           "#eceff4", "#e5e9f0", "#d8dee9", "#2e3440", "#4c566a", "#8a93a5", "#a5adbb", "#5e81ac", true),
            new ThemePalette("one-dark-pro",         "#282c34", "#21252b", "#2c313a", "#abb2bf", "#9da4b0", "#8c93a1", "#636d83", "#61afef", false),
            new ThemePalette("one-light",            "#fafafa", "#f0f0f0", "#e5e5e5", "#383a42", "#4f525e", "#a0a1a7", "#b8b8c0", "#4078f2", true),
            new ThemePalette("quiet-light",          "#f5f5f5", "#ececec", "#e0e0e0", "#333333", "#4d4d4d", "#767676", "#959595", "#4a6f8b", true),
            new ThemePalette("rose-pine",            "#191724", "#1f1d2e", "#26233a", "#e0def4", "#908caa", "#6e6a86", "#595577", "#ebbcba", false),
            new ThemePalette("solarized-dark",       "#002b36", "#0a3541", "#12434f", "#a0b0b4", "#8a9ba1", "#677f86", "#50666c", "#2e9fd8", false),
            new ThemePalette("solarized-deep",       "#0c141d", "#15212b", "#1d2b37", "#dce5ec", "#a4b3c2", "#7d8ea0", "#5d6e80", "#3bb8e0", false),
            new ThemePalette("solarized-light",      "#fdf6e3", "#eee8d5", "#e0dcc8", "#657b83", "#586e75", "#93a1a1", "#a8b4b4", "#268bd2", true),
            new ThemePalette("tokyo-night",          "#1a1b26", "#16161e", "#292e42", "#c0caf5", "#a9b1d6", "#7f87af", "#565f89", "#7aa2f7", false),
            new ThemePalette("vitesse-dark",         "#121212", "#181818", "#222222", "#dbd7ca", "#c8c5b8", "#758575", "#5c6a5c", "#4d9375", false),
            new ThemePalette("vitesse-light",        "#ffffff", "#f6f6f4", "#ebebe8", "#393a34", "#5b5a54", "#999999", "#adacaa", "#4d9375", true),
    };

    /** The github-dark entry, used for null/empty/unknown IDs. */
    private static final ThemePalette FALLBACK = TABLE[11];

    static {
        // FALLBACK is indexed positionally; fail loudly at class-init if the
        // github-dark row ever moves instead of silently resolving to another
        // theme.
        if (!DEFAULT_THEME_ID.equals(FALLBACK.id)) {
            throw new IllegalStateException(
                    "ThemePalette table row 11 must be " + DEFAULT_THEME_ID
                            + " but was " + FALLBACK.id);
        }
    }
}
