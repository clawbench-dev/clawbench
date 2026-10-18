package com.clawbench.app;

import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.graphics.Rect;
import android.util.AttributeSet;
import android.view.ActionMode;
import android.view.Menu;
import android.view.MenuItem;
import android.view.MotionEvent;
import android.view.View;
import android.webkit.WebView;

import androidx.annotation.Nullable;

/**
 * The main app WebView, extended so we can add our own entries to the system's
 * text-selection menu.
 *
 * ## Why a subclass is the only option
 *
 * A WebView has no {@code setCustomSelectionActionModeCallback} — that API
 * belongs to {@code TextView}. The one hook the framework offers is
 * {@link View#startActionMode}, which the WebView's selection controller calls
 * when a long-press produces a selection.
 *
 * ## Why we WRAP the callback instead of replacing it
 *
 * The callback passed to {@code startActionMode} is Chromium's own: it builds
 * the Copy / Select all / Share menu and handles those clicks. Replacing it
 * would silently drop every system action (this is the well-known trap with
 * this technique). So we pass a wrapper to {@code super} that delegates
 * everything to the original and only adds our two items.
 *
 * ## Ordering
 *
 * Our items go in at order 0, ahead of the system's lowest (1). See
 * {@link SelectionMenuPolicy} for the probe that established this — notably
 * that a distinct order for the second item would tie with a system item and
 * lose the tie-break.
 *
 * A custom View inflated from XML by name is referenced reflectively, so it is
 * kept by an explicit ProGuard rule (see proguard-rules.pro) — without that the
 * release build would fail to inflate it.
 */
public class ClawBenchWebView extends WebView {

    /** Notified when the user picks one of our selection-menu items. */
    public interface SelectionActionListener {
        void onSelectionAction(SelectionMenuPolicy.Action action);
    }

    @Nullable
    private SelectionActionListener selectionActionListener;

    /** The text a programmatic (double-tap) menu is acting on. */
    @Nullable
    private String floatingSelectionText;

    /** The action mode we started for a double-tap, so a second one can close it. */
    @Nullable
    private ActionMode floatingActionMode;

    public ClawBenchWebView(Context context) {
        super(context);
    }

    public ClawBenchWebView(Context context, AttributeSet attrs) {
        super(context, attrs);
    }

    public ClawBenchWebView(Context context, AttributeSet attrs, int defStyleAttr) {
        super(context, attrs, defStyleAttr);
    }

    /**
     * Set the listener invoked for our menu items. Null disables the feature —
     * the wrapper then simply delegates everything, leaving the stock menu.
     */
    public void setSelectionActionListener(@Nullable SelectionActionListener listener) {
        this.selectionActionListener = listener;
    }

    /**
     * Show our selection menu for a double-tap.
     *
     * Why this exists: a double-tap on text in a WebView is handled by Blink,
     * which selects the word but shows NO menu — the page-level double-click
     * handler used to copy outright instead. The user asked for the double-tap
     * to open the SAME native menu a long-press does, so the frontend hands us
     * the text it resolved and we raise the menu ourselves.
     *
     * The rect arrives as FRACTIONS of the viewport (0..1), not pixels. Only this
     * side knows the view's physical size, and the CSS-px → physical-px ratio
     * (devicePixelRatio × the app's CSS `zoom` on {@code <html>}) is not
     * something the web layer can compute. Passing raw CSS px anchored the menu
     * near the top of the screen instead of at the tapped text.
     *
     * TYPE_FLOATING (not TYPE_PRIMARY) so it appears as the familiar floating
     * toolbar over the text rather than a bar docked to the top of the window.
     * The position comes from {@code onGetContentRect} on the callback below —
     * that is what the floating-toolbar implementation uses to place itself.
     *
     * The selection is made by the CALLER (JS sets the DOM selection before
     * calling in), because the quote path reads it back through the shared
     * selection snapshot; this method only needs the text for Copy.
     */
    public void showSelectionMenu(
            @Nullable String text,
            float left, float top, float right, float bottom) {
        if (selectionActionListener == null) return;

        // MUST run on the UI thread: this is reached from a @JavascriptInterface
        // method, which the WebView invokes on its own JavaBridge thread, and
        // startActionMode is a View operation. Posting also lets a queued menu
        // from a rapid second double-tap be superseded rather than raced.
        final String selectionText = text == null ? "" : text;
        final float l = left, t = top, r = right, b = bottom;
        post(() -> showSelectionMenuOnUiThread(selectionText, l, t, r, b));
    }

    private void showSelectionMenuOnUiThread(
            String text, float left, float top, float right, float bottom) {
        if (selectionActionListener == null) return;

        // A previous menu (double-tap twice) would otherwise stack.
        if (floatingActionMode != null) {
            floatingActionMode.finish();
            floatingActionMode = null;
        }

        floatingSelectionText = text;

        // getWidth/getHeight are the WebView's own size, the same coordinate
        // space onGetContentRect and the FloatingToolbar position in.
        final int w = getWidth() > 0 ? getWidth() : getResources().getDisplayMetrics().widthPixels;
        final int h = getHeight() > 0 ? getHeight() : getResources().getDisplayMetrics().heightPixels;
        final Rect anchor = buildFloatingAnchor(left, top, right, bottom, w, h);

        // `super.startActionMode` (not the overridden one) on purpose: the
        // override wraps the callback to prepend our items to CHROMIUM's menu.
        // This menu is entirely ours, so wrapping would both duplicate our items
        // and route COPY through the long-press handler — which forwards it to
        // the JS bridge instead of copying natively.
        floatingActionMode = super.startActionMode(
                newFloatingCallback(anchor), ActionMode.TYPE_FLOATING);
    }

    /**
     * Fractions of the viewport → the anchor rect the floating toolbar sits above.
     *
     * Split out (and package-private) so the arithmetic — including the
     * zero-area fallback — is testable without a real {@code startActionMode},
     * which only returns a mode for a view attached to a window.
     *
     * A zero-area rect (unmeasurable selection) gives the floating toolbar
     * nothing to sit above; it is widened to a point so it still anchors.
     */
    static Rect buildFloatingAnchor(
            float left, float top, float right, float bottom, int viewWidth, int viewHeight) {
        final Rect anchor = new Rect(
                Math.round(left * viewWidth),
                Math.round(top * viewHeight),
                Math.round(right * viewWidth),
                Math.round(bottom * viewHeight));
        if (anchor.width() <= 0 && anchor.height() <= 0) {
            anchor.right = anchor.left;
            anchor.bottom = anchor.top;
        }
        return anchor;
    }

    /**
     * The callback for the programmatic (double-tap) menu.
     *
     * Package-private so a test can drive every framework callback directly:
     * a real TYPE_FLOATING mode only exists for an attached view.
     */
    ActionMode.Callback2 newFloatingCallback(Rect anchor) {
        return new ActionMode.Callback2() {
            @Override
            public boolean onCreateActionMode(ActionMode mode, Menu menu) {
                SelectionMenuPolicy.installFloatingItems(menu, ClawBenchWebView.this::labelFor);
                return true;
            }

            @Override
            public boolean onPrepareActionMode(ActionMode mode, Menu menu) {
                SelectionMenuPolicy.installFloatingItems(menu, ClawBenchWebView.this::labelFor);
                return true;
            }

            @Override
            public boolean onActionItemClicked(ActionMode mode, MenuItem item) {
                SelectionMenuPolicy.Action action = SelectionMenuPolicy.actionFor(item.getItemId());
                if (action == null) return false;

                SelectionActionListener listener = selectionActionListener;
                mode.finish();
                // Copy is handled natively (the text came from JS, the clipboard
                // is ours). Quote / Discuss go through the SAME listener the
                // long-press menu uses, so both menus converge on one handler.
                if (action == SelectionMenuPolicy.Action.COPY) {
                    copyFloatingSelection();
                } else if (listener != null) {
                    listener.onSelectionAction(action);
                }
                return true;
            }

            @Override
            public void onDestroyActionMode(ActionMode mode) {
                if (floatingActionMode == mode) floatingActionMode = null;
            }

            @Override
            public void onGetContentRect(ActionMode mode, View view, Rect outRect) {
                // Positions the floating toolbar over the double-tapped text.
                outRect.set(anchor);
            }
        };
    }

    /** Copy the text a programmatic menu was opened for. */
    private void copyFloatingSelection() {
        String text = floatingSelectionText;
        if (text == null || text.isEmpty()) return;
        try {
            ClipboardManager cm = (ClipboardManager) getContext().getSystemService(Context.CLIPBOARD_SERVICE);
            if (cm != null) cm.setPrimaryClip(ClipData.newPlainText("ClawBench", text));
        } catch (Exception ignored) {
            // Clipboard access can be denied by policy; the menu still closes.
        }
    }

    /**
     * Close the double-tap menu if one is showing. Returns true when it closed one.
     *
     * Needed because a TYPE_FLOATING ActionMode is dismissed ONLY programmatically.
     * The framework DOES have an automatic outside-tap dismissal
     * (`FloatingActionMode.setOutsideTouchable`, whose own doc says "the action
     * mode view will dismiss itself on touch events outside of its window"), but
     * it is {@code @hide} and defaults to FALSE, and {@code DecorView} never turns
     * it on — so an app that starts a floating mode itself gets no outside-tap
     * dismissal at all. Chromium's long-press selection menu closes because Blink
     * calls hideActionMode() from its own touch handling.
     */
    public boolean dismissSelectionMenu() {
        ActionMode mode = floatingActionMode;
        if (mode == null) return false;
        // Clear first: finish() may re-enter onDestroyActionMode asynchronously,
        // and the callback only nulls the field when it still matches.
        floatingActionMode = null;
        mode.finish();
        return true;
    }

    /**
     * Outside-touch dismissal for the double-tap menu.
     *
     * A touch reaching this WebView is BY DEFINITION outside the menu, so no
     * hit-testing is needed. That is guaranteed by how the floating toolbar's
     * window is configured, not assumed:
     *
     *  - `FloatingToolbar` builds its PopupWindow with `mFocusable == false`
     *    (PopupWindow's default), which adds FLAG_NOT_FOCUSABLE; and
     *  - WindowManager documents that FLAG_NOT_FOCUSABLE "will also enable
     *    FLAG_NOT_TOUCH_MODAL whether or not that is explicitly set",
     *
     * so outside touches are NOT swallowed by the popup and are delivered to the
     * window behind it — the exact property FloatingActionMode's own
     * setOutsideTouchable doc relies on ("touches outside of the window will be
     * delivered to the window behind").
     *
     * Only ACTION_DOWN / ACTION_OUTSIDE dismiss: the double-tap that OPENED the
     * menu has already been dispatched by the time the bridge call posts the
     * menu, so its own stream cannot close it.
     *
     * The event is passed through (not consumed): a tap that closes the menu
     * should still do whatever it would have done, and consuming would swallow
     * scroll gestures and make the first tap after opening feel dead.
     */
    @Override
    public boolean dispatchTouchEvent(MotionEvent ev) {
        if (floatingActionMode != null) {
            int action = ev.getActionMasked();
            if (action == MotionEvent.ACTION_DOWN || action == MotionEvent.ACTION_OUTSIDE) {
                dismissSelectionMenu();
            }
        }
        return super.dispatchTouchEvent(ev);
    }

    /**
     * Test seam: install a stand-in action mode.
     *
     * A real TYPE_FLOATING mode only comes back from {@code startActionMode} when
     * the view is attached to a window, which Robolectric does not provide — so
     * the dismissal wiring would otherwise be untestable without a device.
     */
    void setFloatingActionModeForTest(@Nullable ActionMode mode) {
        this.floatingActionMode = mode;
    }

    /** Test seam: whether a menu is currently tracked as open. */
    boolean hasFloatingActionModeForTest() {
        return floatingActionMode != null;
    }

    /** Test seam: the text a programmatic menu would copy. */
    void setFloatingSelectionTextForTest(@Nullable String text) {
        this.floatingSelectionText = text;
    }

    /** Test seam: run the copy path the menu item triggers. */
    void copyFloatingSelectionForTest() {
        copyFloatingSelection();
    }

    /** Resolve a label through the Activity's user-language resolver. */
    private String labelFor(SelectionMenuPolicy.Action action) {
        Context ctx = getContext();
        if (ctx instanceof MainActivity) {
            return ((MainActivity) ctx).userLangString(SelectionMenuPolicy.labelRes(action));
        }
        return ctx.getString(SelectionMenuPolicy.labelRes(action));
    }

    @Override
    public ActionMode startActionMode(ActionMode.Callback callback) {
        return super.startActionMode(wrap(callback));
    }

    @Override
    public ActionMode startActionMode(ActionMode.Callback callback, int type) {
        return super.startActionMode(wrap(callback), type);
    }

    /**
     * Wrap the framework callback so we can inject our items.
     *
     * IDEMPOTENT: both startActionMode overloads are overridden because the
     * framework may reach either one, and on some AOSP versions the 1-arg
     * overload delegates to the 2-arg one — which would wrap twice. A double
     * wrapper happens to be harmless today (installItems removes before adding,
     * and the delegation chain is transparent), but that is a fragile accident
     * to depend on: any future non-idempotent addition in the wrapper would
     * then duplicate menu items. Returning an already-wrapped callback
     * unchanged removes the whole class of problem.
     *
     * Package-private rather than private so {@code ClawBenchWebViewTest} can
     * assert the idempotency directly: {@code startActionMode} returns null
     * under Robolectric (no real selection controller), so the wrapper cannot
     * be read back off the return value.
     */
    ActionMode.Callback wrap(ActionMode.Callback original) {
        if (original instanceof SelectionMenuWrapper) {
            return original;
        }
        return new SelectionMenuWrapper(original);
    }

    /**
     * Delegating wrapper that prepends 引用 / 评论 to the selection menu.
     *
     * Extends {@code Callback2} (not plain {@code Callback}) so
     * {@code onGetContentRect} can be forwarded — that is what positions the
     * floating toolbar over the selection; dropping it would leave the menu
     * mispositioned on the device.
     */
    private final class SelectionMenuWrapper extends ActionMode.Callback2 {

        private final ActionMode.Callback delegate;

        SelectionMenuWrapper(ActionMode.Callback delegate) {
            this.delegate = delegate;
        }

        private boolean isCallback2() {
            return delegate instanceof ActionMode.Callback2;
        }

        @Override
        public boolean onCreateActionMode(ActionMode mode, Menu menu) {
            // Let the framework build its own menu first; we append to it.
            boolean created = delegate.onCreateActionMode(mode, menu);
            installItems(menu);
            return created;
        }

        @Override
        public boolean onPrepareActionMode(ActionMode mode, Menu menu) {
            boolean prepared = delegate.onPrepareActionMode(mode, menu);
            // The framework calls this again on every invalidate(); installItems
            // is idempotent so we do not accumulate duplicates.
            installItems(menu);
            return prepared;
        }

        @Override
        public boolean onActionItemClicked(ActionMode mode, MenuItem item) {
            SelectionMenuPolicy.Action action =
                    SelectionMenuPolicy.actionFor(item.getItemId());
            // COPY is never installed on the long-press menu (Chromium supplies
            // its own), so reaching here with it means the id collided with
            // something else — delegate rather than swallow the click.
            if (action != null && action != SelectionMenuPolicy.Action.COPY) {
                SelectionActionListener listener = selectionActionListener;
                // Finish the action mode BEFORE dispatching: the selection is
                // torn down with it, and the frontend reads the selection when
                // the callback arrives. (The frontend also keeps a snapshot, so
                // ordering is not load-bearing, but dismissing first matches
                // the system's own items and avoids a lingering menu.)
                mode.finish();
                if (listener != null) {
                    listener.onSelectionAction(action);
                }
                return true;
            }
            return delegate.onActionItemClicked(mode, item);
        }

        @Override
        public void onDestroyActionMode(ActionMode mode) {
            delegate.onDestroyActionMode(mode);
        }

        @Override
        public void onGetContentRect(ActionMode mode, View view, android.graphics.Rect outRect) {
            if (isCallback2()) {
                ((ActionMode.Callback2) delegate).onGetContentRect(mode, view, outRect);
                return;
            }
            super.onGetContentRect(mode, view, outRect);
        }

        /**
         * Insert our items, resolving labels in the app's chosen language.
         *
         * Gated on the listener so a WebView without one keeps the STOCK menu:
         * showing our items when nothing handles them would close the menu and
         * do nothing, which reads as a broken button.
         */
        private void installItems(Menu menu) {
            if (selectionActionListener == null) return;
            SelectionMenuPolicy.installItems(menu, ClawBenchWebView.this::labelFor);
        }
    }
}
