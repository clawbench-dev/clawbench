package com.clawbench.app;

import android.content.Context;
import android.graphics.Rect;
import android.view.ActionMode;
import android.view.Menu;
import android.view.MenuItem;
import android.view.View;
import android.widget.PopupMenu;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import java.util.ArrayList;
import java.util.List;

import static org.junit.Assert.assertEquals;
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
}
