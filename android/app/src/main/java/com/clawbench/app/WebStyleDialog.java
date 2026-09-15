package com.clawbench.app;

import android.app.Activity;
import android.app.Dialog;
import android.content.Context;
import android.graphics.drawable.GradientDrawable;
import android.util.TypedValue;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.widget.ImageView;
import android.widget.TextView;

/**
 * Native counterpart of the web's centered dialog card
 * ({@code web/src/components/common/DialogOverlay.vue}) — same scrim, same
 * 14dp-radius card, same title row with an accent-tinted icon chip, same
 * right-aligned button pair.
 *
 * <p>The web version is the reference: markup lives in
 * {@code res/layout/dialog_web_card.xml} and every color is derived at runtime
 * from the persisted theme palette ({@link FloatingThemeColors}) so the dialog
 * follows the user's theme exactly like the in-app UI does.
 *
 * <p>Colors and the card background are built in code rather than as XML
 * drawables on purpose: a parallel set of theme-colored drawables would need
 * every one of the ~40 themes mirrored in XML, which is exactly the kind of
 * duplication that drifts. The palette is already persisted for the floating
 * window, so it is the single source of truth here too.
 *
 * <p>Framework dialogs always draw their own panel and title strip, so
 * {@code Theme.ClawBench.WebDialog} makes the window transparent and this class
 * supplies the scrim + card itself.
 */
final class WebStyleDialog {

    private WebStyleDialog() {
    }

    /** Web parity: .dlg-box max-width: 320px. */
    static final int MAX_CARD_WIDTH_DP = 320;
    /** Web parity: .dlg-overlay padding: 0 20px (the card's side margin). */
    static final int CARD_SIDE_MARGIN_DP = 20;
    /** Web parity: .dlg-box border-radius: 14px (--radius-lg). */
    private static final int CARD_RADIUS_DP = 14;
    /** Web parity: .dlg-title-icon border-radius: 6px (--radius-sm). */
    private static final int ICON_CHIP_RADIUS_DP = 6;
    /** Web parity: .dlg-btn border-radius: 6px (--radius-sm). */
    private static final int BUTTON_RADIUS_DP = 6;

    /**
     * Resolved geometry/colors for one dialog instance. Kept as a value object
     * so the palette is read once and every view is styled from the same values.
     */
    static final class Style {
        final int cardBg;
        final int cardRadiusPx;
        final int iconChipBg;
        final int iconChipRadiusPx;
        final int accent;
        final int textPrimary;
        final int textSecondary;
        final int cancelBg;
        final int buttonRadiusPx;
        final int cardWidthPx;

        Style(int cardBg, int cardRadiusPx, int iconChipBg, int iconChipRadiusPx,
              int accent, int textPrimary, int textSecondary, int cancelBg,
              int buttonRadiusPx, int cardWidthPx) {
            this.cardBg = cardBg;
            this.cardRadiusPx = cardRadiusPx;
            this.iconChipBg = iconChipBg;
            this.iconChipRadiusPx = iconChipRadiusPx;
            this.accent = accent;
            this.textPrimary = textPrimary;
            this.textSecondary = textSecondary;
            this.cancelBg = cancelBg;
            this.buttonRadiusPx = buttonRadiusPx;
            this.cardWidthPx = cardWidthPx;
        }
    }

    /**
     * Build the style for a dialog on {@code context}, reading the persisted
     * palette and the screen width. {@code screenWidthPx} is the full display
     * width; the card is capped at 320dp and shrunk to fit the 20dp side margins
     * (LinearLayout has no maxWidth, so this is resolved here).
     */
    static Style buildStyle(Context context, int screenWidthPx) {
        int[] palette = FloatingThemeColors.get(context);
        int bg = palette[0];
        int text = palette[1];
        int textSecondary = palette[2];
        int accent = palette[3];
        return new Style(
                bg,
                dp(context, CARD_RADIUS_DP),
                FloatingThemeColors.accentTint(accent),
                dp(context, ICON_CHIP_RADIUS_DP),
                accent,
                text,
                textSecondary,
                FloatingThemeColors.tertiaryColor(bg, text),
                dp(context, BUTTON_RADIUS_DP),
                cardWidthPx(context, screenWidthPx));
    }

    /**
     * The card's width in pixels: {@code min(320dp, screenWidth - 2*20dp)}.
     * Pure arithmetic apart from the dp conversion. Never returns a
     * non-positive width — a nonsensical screen width falls back to the cap.
     */
    static int cardWidthPx(Context context, int screenWidthPx) {
        int maxWidth = dp(context, MAX_CARD_WIDTH_DP);
        if (screenWidthPx <= 0) {
            return maxWidth;
        }
        int available = screenWidthPx - 2 * dp(context, CARD_SIDE_MARGIN_DP);
        return Math.max(1, Math.min(maxWidth, available));
    }

    /**
     * Show the dialog. Must be called on the UI thread.
     *
     * @param activity  host activity; a finishing/destroyed activity is a no-op
     * @param title     card title (the title row is hidden when blank)
     * @param message   body text; newlines are preserved
     * @param confirm   primary button label
     * @param cancel    secondary button label
     * @param onConfirm run when the primary button is tapped, after dismissal
     * @param onCancel  run when the secondary button is tapped, after dismissal
     * @return the shown dialog, or null when nothing was shown (e.g. activity
     *         already finishing) — callers do not need it, tests do
     */
    static Dialog show(Activity activity, String title, String message,
                       String confirm, String cancel,
                       Runnable onConfirm, Runnable onCancel) {
        if (activity == null || activity.isFinishing() || activity.isDestroyed()) {
            return null;
        }

        View content = LayoutInflater.from(activity).inflate(R.layout.dialog_web_card, null);
        Dialog dialog = new Dialog(activity, R.style.Theme_ClawBench_WebDialog);
        dialog.setContentView(content);
        // Not cancelable: BACK and taps outside must not dismiss the gate, which
        // mirrors the previous AlertDialog.setCancelable(false) behavior.
        dialog.setCancelable(false);

        Window window = dialog.getWindow();
        if (window != null) {
            window.setLayout(ViewGroup.LayoutParams.MATCH_PARENT,
                    ViewGroup.LayoutParams.WRAP_CONTENT);
        }

        Style style = buildStyle(activity,
                activity.getResources().getDisplayMetrics().widthPixels);
        apply(content, style, title, message, confirm, cancel, dialog, onConfirm, onCancel);

        dialog.show();
        return dialog;
    }

    /**
     * Apply the style + texts + click handlers to an inflated dialog_web_card.
     * Split out from {@link #show} so the wiring can be exercised without a
     * real Dialog/Window.
     */
    static void apply(View content, Style style, String title, String message,
                      String confirm, String cancel, Dialog dialog,
                      Runnable onConfirm, Runnable onCancel) {
        View card = content.findViewById(R.id.webDialogCard);
        card.setBackground(rounded(style.cardBg, style.cardRadiusPx));
        ViewGroup.LayoutParams lp = card.getLayoutParams();
        if (lp != null) {
            lp.width = style.cardWidthPx;
            card.setLayoutParams(lp);
        }

        View titleRow = content.findViewById(R.id.webDialogTitleRow);
        TextView titleView = content.findViewById(R.id.webDialogTitle);
        titleView.setTextColor(style.textPrimary);
        titleView.setText(title == null ? "" : title);
        // The web hides the title row entirely when no title is supplied.
        titleRow.setVisibility(isBlank(title) ? View.GONE : View.VISIBLE);

        View icon = content.findViewById(R.id.webDialogIcon);
        icon.setBackground(rounded(style.iconChipBg, style.iconChipRadiusPx));
        if (icon instanceof ImageView) {
            // The glyph is a white vector; tint it to the accent like the web's
            // icon inherits currentColor from the accent-colored chip.
            ((ImageView) icon).setColorFilter(style.accent);
        }

        TextView messageView = content.findViewById(R.id.webDialogMessage);
        messageView.setTextColor(style.textSecondary);
        messageView.setText(message == null ? "" : message);

        TextView confirmView = content.findViewById(R.id.webDialogConfirm);
        confirmView.setText(confirm == null ? "" : confirm);
        confirmView.setTextColor(0xFFFFFFFF);
        confirmView.setBackground(rounded(style.accent, style.buttonRadiusPx));
        confirmView.setOnClickListener(v -> {
            dismiss(dialog);
            if (onConfirm != null) {
                onConfirm.run();
            }
        });

        TextView cancelView = content.findViewById(R.id.webDialogCancel);
        cancelView.setText(cancel == null ? "" : cancel);
        cancelView.setTextColor(style.textSecondary);
        cancelView.setBackground(rounded(style.cancelBg, style.buttonRadiusPx));
        cancelView.setOnClickListener(v -> {
            dismiss(dialog);
            if (onCancel != null) {
                onCancel.run();
            }
        });
    }

    /** Dismiss safely — the dialog may already be gone (activity teardown). */
    private static void dismiss(Dialog dialog) {
        if (dialog == null || !dialog.isShowing()) {
            return;
        }
        try {
            dialog.dismiss();
        } catch (IllegalArgumentException e) {
            // Dismissing a dialog whose activity is being destroyed throws on
            // some ROMs; the dialog is going away anyway.
            AppLog.w("WebDialog", "dismiss failed", e);
        }
    }

    private static GradientDrawable rounded(int color, int radiusPx) {
        GradientDrawable d = new GradientDrawable();
        d.setShape(GradientDrawable.RECTANGLE);
        d.setColor(color);
        d.setCornerRadius(radiusPx);
        return d;
    }

    private static boolean isBlank(String s) {
        return s == null || s.trim().isEmpty();
    }

    private static int dp(Context context, int value) {
        return Math.round(TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, value,
                context.getResources().getDisplayMetrics()));
    }
}
