package com.clawbench.app;

import android.content.Context;
import android.util.AttributeSet;
import android.view.ActionMode;
import android.view.Menu;
import android.view.MenuItem;
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
     */
    private ActionMode.Callback wrap(ActionMode.Callback original) {
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
            if (action != null) {
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
            SelectionMenuPolicy.installItems(menu, action -> {
                // Prefer the Activity's user-language resolver: the menu belongs
                // to the Activity, and the app's language setting is not the
                // system locale. Fall back to the resource default when the
                // context is not a MainActivity (e.g. a test harness).
                Context ctx = getContext();
                if (ctx instanceof MainActivity) {
                    return ((MainActivity) ctx).userLangString(SelectionMenuPolicy.labelRes(action));
                }
                return ctx.getString(SelectionMenuPolicy.labelRes(action));
            });
        }
    }
}
