package com.clawbench.app;

import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.graphics.Rect;
import android.view.ActionMode;
import android.view.Menu;
import android.view.MenuItem;
import android.view.MotionEvent;
import android.view.View;
import android.widget.PopupMenu;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;
import org.robolectric.shadows.ShadowLooper;

import java.util.ArrayList;
import java.util.List;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for {@link ClawBenchWebView}'s selection-menu wrapper.
 *
 * <p>These exist because the wrapper's whole job is <em>delegation</em>: it must
 * forward every framework callback untouched and only add our two items. The
 * failure modes are silent — a replaced callback drops Copy/Select-all, and a
 * double wrapper duplicates our entries — so they are pinned here rather than
 * left to a device check.
 *
 * <p>Robolectric is required (not a plain JVM test): {@code ClawBenchWebView}
 * extends {@code WebView}, and {@code startActionMode} must reach a real
 * framework implementation to observe what was actually passed down. The
 * pure decision rules live in {@link SelectionMenuPolicy} and are covered by
 * {@link SelectionMenuPolicyTest}; this class covers the wiring that policy
 * cannot see.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class ClawBenchWebViewTest {

    private ClawBenchWebView webView;
    private Context ctx;

    @Before
    public void setUp() {
        ctx = RuntimeEnvironment.getApplication();
        webView = new ClawBenchWebView(ctx);
    }

    /** A menu shaped like Chromium's own, with the system entries already in it. */
    private Menu systemMenu() {
        Menu menu = new PopupMenu(ctx, null).getMenu();
        menu.add(Menu.NONE, android.R.id.cut, 2, "cut");
        menu.add(Menu.NONE, android.R.id.copy, 3, "copy");
        return menu;
    }

    /**
     * Records everything the wrapper delegates, and lets a test decide what the
     * framework "did" so the delegation contract can be asserted directly.
     */
    private static final class RecordingCallback extends ActionMode.Callback2 {
        int created;
        int prepared;
        int destroyed;
        int clicked;
        boolean createResult = true;
        boolean prepareResult = true;
        boolean clickResult = false;
        MenuItem lastClicked;
        Rect lastContentRect;

        @Override
        public boolean onCreateActionMode(ActionMode mode, Menu menu) {
            created++;
            return createResult;
        }

        @Override
        public boolean onPrepareActionMode(ActionMode mode, Menu menu) {
            prepared++;
            return prepareResult;
        }

        @Override
        public boolean onActionItemClicked(ActionMode mode, MenuItem item) {
            clicked++;
            lastClicked = item;
            return clickResult;
        }

        @Override
        public void onDestroyActionMode(ActionMode mode) {
            destroyed++;
        }

        @Override
        public void onGetContentRect(ActionMode mode, View view, Rect outRect) {
            lastContentRect = outRect;
        }
    }

    /** A minimal ActionMode stand-in; only finish() is observed by the wrapper. */
    private static final class FakeActionMode extends ActionMode {
        boolean finished;

        @Override
        public void setTitle(CharSequence title) {
        }

        @Override
        public void setTitle(int resId) {
        }

        @Override
        public void setSubtitle(CharSequence subtitle) {
        }

        @Override
        public void setSubtitle(int resId) {
        }

        @Override
        public void setCustomView(View view) {
        }

        @Override
        public void invalidate() {
        }

        @Override
        public void finish() {
            finished = true;
        }

        @Override
        public Menu getMenu() {
            return null;
        }

        @Override
        public CharSequence getTitle() {
            return null;
        }

        @Override
        public CharSequence getSubtitle() {
            return null;
        }

        @Override
        public View getCustomView() {
            return null;
        }

        @Override
        public android.view.MenuInflater getMenuInflater() {
            return null;
        }

        @Override
        public void setTitleOptionalHint(boolean titleOptional) {
        }

        @Override
        public boolean getTitleOptionalHint() {
            return false;
        }

        @Override
        public boolean isTitleOptional() {
            return false;
        }
    }

    /**
     * The 1-arg overload may delegate to the 2-arg one on some AOSP versions, so
     * the SAME callback can reach wrap() twice. The second pass must return the
     * already-wrapped callback, not a second wrapper — otherwise a future
     * non-idempotent addition would duplicate menu items.
     */
    @Test
    public void wrap_isIdempotent() {
        ActionMode.Callback original = new RecordingCallback();

        ActionMode.Callback first = webView.wrap(original);
        assertNotNull("wrap must produce a wrapper", first);

        ActionMode.Callback second = webView.wrap(first);
        assertSame("wrapping an already-wrapped callback must return it unchanged", first, second);
    }

    // ── delegation ──────────────────────────────────────────────────────

    /**
     * Every framework callback must reach the original, and the original's
     * return value must be preserved. Replacing rather than wrapping is the
     * classic bug here — it silently drops Copy / Select all / Share.
     */
    @Test
    public void delegatesEveryCallbackToTheOriginal() {
        RecordingCallback original = new RecordingCallback();
        original.createResult = false; // must be forwarded verbatim
        ActionMode.Callback wrapper = webView.wrap(original);

        FakeActionMode mode = new FakeActionMode();
        Menu menu = systemMenu();

        assertEquals("onCreateActionMode's return value must be forwarded",
                false, wrapper.onCreateActionMode(mode, menu));
        assertEquals(1, original.created);

        assertEquals("onPrepareActionMode's return value must be forwarded",
                true, wrapper.onPrepareActionMode(mode, menu));
        assertEquals(1, original.prepared);

        wrapper.onDestroyActionMode(mode);
        assertEquals(1, original.destroyed);

        // A system item's click must be delegated (the wrapper's clickResult is
        // false, and our ids are never in this menu).
        MenuItem copy = menu.findItem(android.R.id.copy);
        assertNotNull(copy);
        wrapper.onActionItemClicked(mode, copy);
        assertEquals("a non-our item's click must reach the framework", 1, original.clicked);
        assertSame(copy, original.lastClicked);
    }

    /**
     * onGetContentRect positions the floating toolbar over the selection. The
     * wrapper extends Callback2 so it can forward it — dropping it leaves the
     * menu mispositioned on the device.
     */
    @Test
    public void forwardsContentRectToCallback2Delegate() {
        RecordingCallback original = new RecordingCallback();
        ActionMode.Callback wrapper = webView.wrap(original);

        assertTrue("the wrapper must be a Callback2 to forward onGetContentRect",
                wrapper instanceof ActionMode.Callback2);

        Rect rect = new Rect(0, 0, 10, 10);
        ((ActionMode.Callback2) wrapper).onGetContentRect(new FakeActionMode(), webView, rect);
        assertSame("onGetContentRect must be forwarded to a Callback2 delegate", rect, original.lastContentRect);
    }

    // ── item installation gating ────────────────────────────────────────

    /**
     * With no listener the wrapper must leave the STOCK menu alone: showing our
     * items when nothing handles them closes the menu and does nothing, which
     * reads as a broken button.
     */
    @Test
    public void installsNoItemsWithoutAListener() {
        ActionMode.Callback wrapper = webView.wrap(new RecordingCallback());
        Menu menu = systemMenu();
        int before = menu.size();

        wrapper.onCreateActionMode(new FakeActionMode(), menu);

        assertEquals("the stock menu must be untouched when no listener is set",
                before, menu.size());
    }

    /** With a listener set, both of our items are installed. */
    @Test
    public void installsBothItemsWithAListener() {
        webView.setSelectionActionListener(action -> {
        });
        ActionMode.Callback wrapper = webView.wrap(new RecordingCallback());
        Menu menu = systemMenu();

        wrapper.onCreateActionMode(new FakeActionMode(), menu);

        assertNotNull(menu.findItem(SelectionMenuPolicy.ID_QUOTE));
        assertNotNull(menu.findItem(SelectionMenuPolicy.ID_COMMENT));
    }

    /**
     * installItems runs on every onPrepareActionMode pass; a non-idempotent
     * implementation would append another pair each time.
     */
    @Test
    public void doesNotDuplicateItemsAcrossPreparePasses() {
        webView.setSelectionActionListener(action -> {
        });
        ActionMode.Callback wrapper = webView.wrap(new RecordingCallback());
        Menu menu = systemMenu();

        wrapper.onCreateActionMode(new FakeActionMode(), menu);
        wrapper.onPrepareActionMode(new FakeActionMode(), menu);
        wrapper.onPrepareActionMode(new FakeActionMode(), menu);

        List<Integer> ids = new ArrayList<>();
        for (int i = 0; i < menu.size(); i++) {
            ids.add(menu.getItem(i).getItemId());
        }
        assertEquals("quote must appear exactly once", 1, countOf(ids, SelectionMenuPolicy.ID_QUOTE));
        assertEquals("comment must appear exactly once", 1, countOf(ids, SelectionMenuPolicy.ID_COMMENT));
    }

    private static int countOf(List<Integer> ids, int target) {
        int n = 0;
        for (int id : ids) {
            if (id == target) n++;
        }
        return n;
    }

    // ── our-item click handling ─────────────────────────────────────────

    /**
     * Clicking one of our items must finish the action mode and notify the
     * listener — and must NOT reach the framework callback (which would
     * double-handle the click).
     */
    @Test
    public void ourItemClickFinishesModeAndNotifiesListener() {
        List<SelectionMenuPolicy.Action> handled = new ArrayList<>();
        webView.setSelectionActionListener(handled::add);

        RecordingCallback original = new RecordingCallback();
        ActionMode.Callback wrapper = webView.wrap(original);
        Menu menu = systemMenu();
        wrapper.onCreateActionMode(new FakeActionMode(), menu);

        FakeActionMode mode = new FakeActionMode();
        MenuItem quote = menu.findItem(SelectionMenuPolicy.ID_QUOTE);
        assertNotNull(quote);

        boolean handledByWrapper = wrapper.onActionItemClicked(mode, quote);

        assertTrue("our item's click must be reported as handled", handledByWrapper);
        assertTrue("the action mode must be finished before dispatching", mode.finished);
        assertEquals(1, handled.size());
        assertEquals(SelectionMenuPolicy.Action.QUOTE, handled.get(0));
        assertEquals("our click must not also reach the framework callback", 0, original.clicked);
    }

    /** The COMMENT item routes to the COMMENT action. */
    @Test
    public void commentItemRoutesToCommentAction() {
        List<SelectionMenuPolicy.Action> handled = new ArrayList<>();
        webView.setSelectionActionListener(handled::add);

        ActionMode.Callback wrapper = webView.wrap(new RecordingCallback());
        Menu menu = systemMenu();
        wrapper.onCreateActionMode(new FakeActionMode(), menu);

        MenuItem comment = menu.findItem(SelectionMenuPolicy.ID_COMMENT);
        assertNotNull(comment);
        wrapper.onActionItemClicked(new FakeActionMode(), comment);

        assertEquals(1, handled.size());
        assertEquals(SelectionMenuPolicy.Action.COMMENT, handled.get(0));
    }

    /**
     * A null listener disables the feature entirely: our ids cannot appear in
     * the menu (installItems is gated), so this only pins that a click on an
     * item with our id does not NPE when the listener was cleared after install.
     */
    @Test
    public void ourItemClickWithoutListenerDoesNotThrow() {
        webView.setSelectionActionListener(action -> {
        });
        ActionMode.Callback wrapper = webView.wrap(new RecordingCallback());
        Menu menu = systemMenu();
        wrapper.onCreateActionMode(new FakeActionMode(), menu);

        webView.setSelectionActionListener(null); // cleared after the menu was built

        MenuItem quote = menu.findItem(SelectionMenuPolicy.ID_QUOTE);
        assertNotNull(quote);
        FakeActionMode mode = new FakeActionMode();
        assertTrue("the click is still ours, so it is still consumed",
                wrapper.onActionItemClicked(mode, quote));
        assertTrue("the mode must still be finished", mode.finished);
    }

    // ── outside-touch dismissal of the double-tap menu ──────────────────

    /**
     * A TYPE_FLOATING ActionMode gets NO automatic outside-tap dismissal (the
     * framework's own hook for it is @hide and off by default), so the view has
     * to close it on a touch that reaches the WebView. A touch reaching the
     * WebView is by definition outside the menu — the toolbar's popup is
     * FLAG_NOT_FOCUSABLE, which WindowManager documents as also enabling
     * FLAG_NOT_TOUCH_MODAL, so it does not swallow outside touches.
     */
    @Test
    public void outsideTouchDismissesTheFloatingMenu() {
        FakeActionMode mode = new FakeActionMode();
        webView.setFloatingActionModeForTest(mode);

        webView.dispatchTouchEvent(MotionEvent.obtain(
                0, 0, MotionEvent.ACTION_DOWN, 10f, 10f, 0));

        assertTrue("an outside ACTION_DOWN must finish the menu", mode.finished);
        assertFalse("the menu must no longer be tracked as open",
                webView.hasFloatingActionModeForTest());
    }

    /**
     * The tap that closes the menu must still reach the page: swallowing it would
     * make the first tap after opening feel dead, and would break scrolling.
     */
    @Test
    public void outsideTouchIsNotConsumed() {
        webView.setFloatingActionModeForTest(new FakeActionMode());

        boolean consumed = webView.dispatchTouchEvent(MotionEvent.obtain(
                0, 0, MotionEvent.ACTION_DOWN, 10f, 10f, 0));

        // A WebView with no content returns false; what matters is that we did not
        // short-circuit with `return true` just because we dismissed the menu.
        assertFalse("dismissal must not consume the event", consumed);
    }

    /** A MOVE/UP alone must not close the menu (only the touch's start does). */
    @Test
    public void moveDoesNotDismissTheFloatingMenu() {
        FakeActionMode mode = new FakeActionMode();
        webView.setFloatingActionModeForTest(mode);

        webView.dispatchTouchEvent(MotionEvent.obtain(
                0, 0, MotionEvent.ACTION_MOVE, 10f, 10f, 0));

        assertFalse("ACTION_MOVE must not dismiss", mode.finished);
        assertTrue("the menu is still open", webView.hasFloatingActionModeForTest());
    }

    /** No menu open → the touch path is a plain pass-through, no NPE. */
    @Test
    public void dispatchTouchWithoutAMenuIsHarmless() {
        assertFalse(webView.dismissSelectionMenu());
        assertFalse(webView.hasFloatingActionModeForTest());
        webView.dispatchTouchEvent(MotionEvent.obtain(
                0, 0, MotionEvent.ACTION_DOWN, 1f, 1f, 0));
    }

    /**
     * Dismissing twice must not finish twice: onDestroyActionMode already
     * cleared the field, and the second call reports "nothing to close".
     */
    @Test
    public void dismissIsIdempotent() {
        FakeActionMode mode = new FakeActionMode();
        webView.setFloatingActionModeForTest(mode);

        assertTrue(webView.dismissSelectionMenu());
        assertTrue(mode.finished);
        assertFalse("the second dismiss must report nothing to close",
                webView.dismissSelectionMenu());
    }

    // ── double-tap menu: anchor geometry ────────────────────────────────

    /**
     * The bridge hands over FRACTIONS of the viewport, because only this side
     * knows the physical size (CSS px × devicePixelRatio × the app's CSS zoom).
     * Interpreting them as pixels anchored the toolbar near the top of the
     * screen instead of at the tapped word — the bug this pins.
     */
    @Test
    public void anchorScalesFractionsByViewSize() {
        Rect anchor = ClawBenchWebView.buildFloatingAnchor(0.25f, 0.5f, 0.75f, 0.75f, 400, 800);

        assertEquals(100, anchor.left);
        assertEquals(400, anchor.top);
        assertEquals(300, anchor.right);
        assertEquals(600, anchor.bottom);
    }

    /**
     * A zero-area rect gives the floating toolbar nothing to sit above. The
     * framework would then place it at an arbitrary corner, so the rect is
     * widened to a point at the same position.
     */
    @Test
    public void zeroAreaAnchorCollapsesToAPoint() {
        Rect anchor = ClawBenchWebView.buildFloatingAnchor(0.5f, 0.5f, 0.5f, 0.5f, 400, 800);

        assertEquals(200, anchor.left);
        assertEquals(400, anchor.top);
        assertEquals("a degenerate rect must collapse to a point, not stay inverted",
                anchor.left, anchor.right);
        assertEquals(anchor.top, anchor.bottom);
    }

    /**
     * A rect with width but no height is also degenerate. Only BOTH dimensions
     * being non-positive triggers the collapse, so this one keeps its width —
     * pinning that the guard is the conjunction, not "either".
     */
    @Test
    public void zeroHeightAnchorKeepsItsWidth() {
        Rect anchor = ClawBenchWebView.buildFloatingAnchor(0.25f, 0.5f, 0.75f, 0.5f, 400, 800);

        assertEquals(100, anchor.left);
        assertEquals(300, anchor.right);
        assertEquals("height collapses to the top edge", anchor.top, anchor.bottom);
    }

    // ── double-tap menu: the callback it installs ───────────────────────

    /**
     * The floating menu is built entirely by us, so it must carry Copy — unlike
     * the long-press menu, where Chromium supplies its own.
     */
    @Test
    public void floatingCallbackInstallsCopyQuoteCommentAndSearch() {
        webView.setSelectionActionListener(action -> {
        });
        ActionMode.Callback2 callback = webView.newFloatingCallback(new Rect(0, 0, 1, 1));
        Menu menu = systemMenu();

        assertTrue(callback.onCreateActionMode(new FakeActionMode(), menu));

        assertNotNull("Copy must be on the programmatic menu", menu.findItem(SelectionMenuPolicy.ID_COPY));
        assertNotNull(menu.findItem(SelectionMenuPolicy.ID_QUOTE));
        assertNotNull(menu.findItem(SelectionMenuPolicy.ID_COMMENT));
        assertNotNull(menu.findItem(SelectionMenuPolicy.ID_SEARCH_FILE));
    }

    /** Re-preparing must not duplicate: the framework invalidates repeatedly. */
    @Test
    public void floatingCallbackDoesNotDuplicateItemsAcrossPreparePasses() {
        webView.setSelectionActionListener(action -> {
        });
        ActionMode.Callback2 callback = webView.newFloatingCallback(new Rect(0, 0, 1, 1));
        Menu menu = systemMenu();

        callback.onCreateActionMode(new FakeActionMode(), menu);
        callback.onPrepareActionMode(new FakeActionMode(), menu);
        callback.onPrepareActionMode(new FakeActionMode(), menu);

        List<Integer> ids = new ArrayList<>();
        for (int i = 0; i < menu.size(); i++) {
            ids.add(menu.getItem(i).getItemId());
        }
        assertEquals(1, countOf(ids, SelectionMenuPolicy.ID_COPY));
        assertEquals(1, countOf(ids, SelectionMenuPolicy.ID_QUOTE));
        assertEquals(1, countOf(ids, SelectionMenuPolicy.ID_COMMENT));
        assertEquals(1, countOf(ids, SelectionMenuPolicy.ID_SEARCH_FILE));
    }

    /**
     * Quote / Comment / Search must reach the SAME listener the long-press menu
     * uses — that convergence is the point of having one handler.
     */
    @Test
    public void floatingCallbackRoutesQuoteToTheListener() {
        List<SelectionMenuPolicy.Action> handled = new ArrayList<>();
        webView.setSelectionActionListener(handled::add);
        ActionMode.Callback2 callback = webView.newFloatingCallback(new Rect(0, 0, 1, 1));
        Menu menu = systemMenu();
        callback.onCreateActionMode(new FakeActionMode(), menu);

        FakeActionMode mode = new FakeActionMode();
        MenuItem quote = menu.findItem(SelectionMenuPolicy.ID_QUOTE);
        assertNotNull(quote);
        assertTrue(callback.onActionItemClicked(mode, quote));

        assertTrue("the mode must be finished before dispatching", mode.finished);
        assertEquals(1, handled.size());
        assertEquals(SelectionMenuPolicy.Action.QUOTE, handled.get(0));
    }

    /**
     * Copy must NOT go over the JS bridge: the text came from JS and the
     * clipboard is ours. Routing it to the listener would ask the frontend to
     * copy text it already has, and leave the native clipboard untouched.
     */
    @Test
    public void floatingCallbackCopiesNativelyWithoutNotifyingTheListener() {
        List<SelectionMenuPolicy.Action> handled = new ArrayList<>();
        webView.setSelectionActionListener(handled::add);
        webView.setFloatingSelectionTextForTest("tapped word");

        ActionMode.Callback2 callback = webView.newFloatingCallback(new Rect(0, 0, 1, 1));
        Menu menu = systemMenu();
        callback.onCreateActionMode(new FakeActionMode(), menu);

        FakeActionMode mode = new FakeActionMode();
        MenuItem copy = menu.findItem(SelectionMenuPolicy.ID_COPY);
        assertNotNull(copy);
        assertTrue(callback.onActionItemClicked(mode, copy));

        assertTrue(mode.finished);
        assertTrue("copy must not be routed to the JS bridge", handled.isEmpty());

        ClipboardManager cm = (ClipboardManager) ctx.getSystemService(Context.CLIPBOARD_SERVICE);
        assertNotNull(cm);
        ClipData clip = cm.getPrimaryClip();
        assertNotNull("the tapped text must reach the clipboard", clip);
        assertEquals("tapped word", clip.getItemAt(0).coerceToText(ctx).toString());
    }

    /**
     * An id that is not ours must be reported as unhandled so the framework can
     * act on it — claiming it would swallow a system item's action.
     */
    @Test
    public void floatingCallbackDeclinesForeignItems() {
        webView.setSelectionActionListener(action -> {
        });
        ActionMode.Callback2 callback = webView.newFloatingCallback(new Rect(0, 0, 1, 1));
        Menu menu = systemMenu();
        callback.onCreateActionMode(new FakeActionMode(), menu);

        MenuItem foreign = menu.findItem(android.R.id.copy);
        assertNotNull(foreign);
        assertFalse(callback.onActionItemClicked(new FakeActionMode(), foreign));
    }

    /**
     * The toolbar is placed from onGetContentRect — dropping it (or returning a
     * fresh empty rect) leaves the menu at the top of the window instead of over
     * the double-tapped word.
     */
    @Test
    public void floatingCallbackAnchorsTheToolbarOnTheSelection() {
        webView.setSelectionActionListener(action -> {
        });
        Rect anchor = new Rect(10, 20, 30, 40);
        ActionMode.Callback2 callback = webView.newFloatingCallback(anchor);

        Rect out = new Rect();
        callback.onGetContentRect(new FakeActionMode(), webView, out);

        assertEquals(anchor, out);
    }

    /**
     * onDestroyActionMode clears the tracked mode so the next touch is a plain
     * pass-through — but only when it is still the mode we tracked.
     */
    @Test
    public void floatingCallbackClearsTheTrackedModeOnDestroy() {
        webView.setSelectionActionListener(action -> {
        });
        ActionMode.Callback2 callback = webView.newFloatingCallback(new Rect(0, 0, 1, 1));
        FakeActionMode mode = new FakeActionMode();
        webView.setFloatingActionModeForTest(mode);

        callback.onDestroyActionMode(mode);

        assertFalse("destroying the tracked mode must clear it",
                webView.hasFloatingActionModeForTest());
    }

    /** A different mode's destruction must not clear the one we track. */
    @Test
    public void floatingCallbackIgnoresDestroyOfAnotherMode() {
        webView.setSelectionActionListener(action -> {
        });
        ActionMode.Callback2 callback = webView.newFloatingCallback(new Rect(0, 0, 1, 1));
        FakeActionMode tracked = new FakeActionMode();
        webView.setFloatingActionModeForTest(tracked);

        callback.onDestroyActionMode(new FakeActionMode());

        assertTrue("an unrelated mode's destruction must not clear ours",
                webView.hasFloatingActionModeForTest());
    }

    // ── double-tap menu: copy edge cases ────────────────────────────────

    /** Copying with no captured text is a no-op, not an NPE or an empty clip. */
    @Test
    public void copyWithNoSelectionDoesNothing() {
        ClipboardManager cm = (ClipboardManager) ctx.getSystemService(Context.CLIPBOARD_SERVICE);
        assertNotNull(cm);
        cm.setPrimaryClip(ClipData.newPlainText("sentinel", "untouched"));

        webView.setFloatingSelectionTextForTest(null);
        webView.copyFloatingSelectionForTest();

        assertEquals("no captured text must leave the clipboard alone",
                "untouched", cm.getPrimaryClip().getItemAt(0).coerceToText(ctx).toString());
    }

    /** An empty selection must not replace the user's clipboard with "". */
    @Test
    public void copyWithEmptySelectionDoesNothing() {
        ClipboardManager cm = (ClipboardManager) ctx.getSystemService(Context.CLIPBOARD_SERVICE);
        assertNotNull(cm);
        cm.setPrimaryClip(ClipData.newPlainText("sentinel", "untouched"));

        webView.setFloatingSelectionTextForTest("");
        webView.copyFloatingSelectionForTest();

        assertEquals("an empty selection must leave the clipboard alone",
                "untouched", cm.getPrimaryClip().getItemAt(0).coerceToText(ctx).toString());
    }

    // ── double-tap entry point ──────────────────────────────────────────

    /**
     * With no listener there is nothing to handle the menu's actions, so opening
     * one would show buttons that close it and do nothing. The entry point must
     * bail BEFORE scheduling work.
     */
    @Test
    public void showSelectionMenuIsANoOpWithoutAListener() {
        webView.setSelectionActionListener(null);
        webView.showSelectionMenuNowForTest("word", 0.1f, 0.1f, 0.2f, 0.2f);

        assertFalse("no menu may be opened without a listener",
                webView.hasFloatingActionModeForTest());
    }

    /**
     * A second double-tap must supersede the first menu rather than stack a
     * second one on top of it.
     */
    @Test
    public void showSelectionMenuClosesThePreviousMenu() {
        webView.setSelectionActionListener(action -> {
        });
        FakeActionMode previous = new FakeActionMode();
        webView.setFloatingActionModeForTest(previous);

        // Robolectric returns null from startActionMode (no selection
        // controller), which is fine: what matters is that the PREVIOUS mode was
        // finished before the new one was requested.
        webView.showSelectionMenuNowForTest("word", 0.1f, 0.1f, 0.2f, 0.2f);

        assertTrue("the superseded menu must be finished", previous.finished);
    }

    /**
     * The programmatic entry point is reached from a @JavascriptInterface method,
     * i.e. off the UI thread, so it must POST rather than call startActionMode
     * inline.
     *
     * An unattached View queues the runnable instead of running it, which is
     * exactly the "not inline" property being pinned: the supersede work has not
     * happened yet at the moment showSelectionMenu returns.
     */
    @Test
    public void showSelectionMenuDefersToTheUiThread() {
        webView.setSelectionActionListener(action -> {
        });
        FakeActionMode previous = new FakeActionMode();
        webView.setFloatingActionModeForTest(previous);

        webView.showSelectionMenu("word", 0.1f, 0.1f, 0.2f, 0.2f);
        assertFalse("the work must not run inline", previous.finished);
    }

    /** A null selection text must not NPE — it becomes "". */
    @Test
    public void showSelectionMenuAcceptsNullText() {
        webView.setSelectionActionListener(action -> {
        });

        webView.showSelectionMenuNowForTest(null, 0.1f, 0.1f, 0.2f, 0.2f);
        // No assertion on the mode (Robolectric yields null); reaching here
        // without an exception is the contract.
        assertFalse(webView.hasFloatingActionModeForTest());
    }

    // ── constructors ────────────────────────────────────────────────────

    /**
     * The view is inflated from XML by name, so the (Context, AttributeSet)
     * constructor is the one the app actually uses. It must not throw.
     */
    @Test
    public void constructsFromAttributeSet() {
        android.util.AttributeSet attrs =
                org.robolectric.Robolectric.buildAttributeSet().build();
        ClawBenchWebView fromXml = new ClawBenchWebView(ctx, attrs);
        assertNotNull(fromXml);
    }

    /** Same, with an explicit defStyleAttr (the 3-arg overload). */
    @Test
    public void constructsFromAttributeSetWithDefStyle() {
        android.util.AttributeSet attrs =
                org.robolectric.Robolectric.buildAttributeSet().build();
        ClawBenchWebView fromXml = new ClawBenchWebView(ctx, attrs, 0);
        assertNotNull(fromXml);
    }

    // ── label resolution ────────────────────────────────────────────────

    /**
     * Outside a MainActivity the label falls back to the plain resource string.
     * The Robolectric application context is not a MainActivity, so this pins
     * the fallback branch (the MainActivity branch needs the real Activity).
     */
    @Test
    public void labelFallsBackToTheResourceString() {
        String expected = ctx.getString(SelectionMenuPolicy.labelRes(SelectionMenuPolicy.Action.QUOTE));
        String actual = webView.labelForForTest(SelectionMenuPolicy.Action.QUOTE);
        assertEquals(expected, actual);
    }

    // ── wrapper: non-Callback2 delegate ─────────────────────────────────

    /**
     * A plain {@code ActionMode.Callback} delegate has no onGetContentRect, so
     * the wrapper must fall back to its own super implementation instead of
     * casting. Reaching the cast would throw ClassCastException on a device.
     */
    @Test
    public void wrapperFallsBackToSuperForNonCallback2Delegate() {
        ActionMode.Callback plain = new ActionMode.Callback() {
            @Override
            public boolean onCreateActionMode(ActionMode mode, Menu menu) {
                return true;
            }

            @Override
            public boolean onPrepareActionMode(ActionMode mode, Menu menu) {
                return true;
            }

            @Override
            public boolean onActionItemClicked(ActionMode mode, MenuItem item) {
                return false;
            }

            @Override
            public void onDestroyActionMode(ActionMode mode) {
            }
        };
        ActionMode.Callback wrapper = webView.wrap(plain);
        assertTrue(wrapper instanceof ActionMode.Callback2);

        Rect rect = new Rect(1, 2, 3, 4);
        // Callback2's default onGetContentRect calls outRect.setEmpty() — that is
        // the framework behaviour a plain Callback delegate gets. The point of
        // this test is that the super path is reached without a ClassCastException;
        // the emptied rect is the observable proof it was the super, not a cast.
        ((ActionMode.Callback2) wrapper).onGetContentRect(new FakeActionMode(), webView, rect);
        assertTrue("the default Callback2 implementation empties the rect",
                rect.isEmpty());
    }
}
