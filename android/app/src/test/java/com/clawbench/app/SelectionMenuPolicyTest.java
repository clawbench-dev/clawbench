package com.clawbench.app;

import android.content.Context;
import android.view.Menu;
import android.view.MenuItem;
import android.widget.PopupMenu;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import java.util.ArrayList;
import java.util.List;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for the custom WebView text-selection menu policy.
 *
 * The logic lives in {@link SelectionMenuPolicy} (rather than inline in
 * {@link ClawBenchWebView}) precisely so it can be exercised here. There is no
 * emulator in this environment, so these tests are the only automated check on
 * the ordering / id rules — the actual menu appearance must be verified on a
 * device.
 *
 * The ordering tests run against a REAL {@code MenuBuilder} (via Robolectric),
 * not against the policy's own constants: an earlier version asserted
 * "comment's order (1) < 1" and passed while the real menu still placed the
 * system's assist item ahead of comment, because both used order 1 and the
 * framework won the tie. Asserting the constants only proves self-consistency;
 * these tests assert the resulting ORDER.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class SelectionMenuPolicyTest {

    private static final SelectionMenuPolicy.LabelResolver LABELS = action -> {
        switch (action) {
            case COPY: return "Copy";
            case QUOTE: return "Quote";
            case COMMENT: return "Comment";
            case SEARCH_FILE: return "Find file";
            default: throw new IllegalArgumentException();
        }
    };

    /** A fresh menu holding a stand-in for the framework's own selection items. */
    private Menu systemMenu() {
        Context ctx = RuntimeEnvironment.getApplication();
        Menu menu = new PopupMenu(ctx, null).getMenu();
        // Mirrors Chromium's select_action_menu.xml: assist group at 1, then
        // cut=2, copy=3, select-all=6. Added FIRST, as the framework builds its
        // menu before our wrapper appends to it.
        menu.add(Menu.NONE, 0x7f990001, 1, "assist");
        menu.add(Menu.NONE, android.R.id.cut, 2, "cut");
        menu.add(Menu.NONE, android.R.id.copy, 3, "copy");
        menu.add(Menu.NONE, android.R.id.selectAll, 6, "selectAll");
        return menu;
    }

    private static List<Integer> ids(Menu menu) {
        List<Integer> out = new ArrayList<>();
        for (int i = 0; i < menu.size(); i++) {
            out.add(menu.getItem(i).getItemId());
        }
        return out;
    }

    // ── Ordering against a real MenuBuilder ─────────────────────────────────

    /**
     * Both our items must land at the FRONT, ahead of the system's lowest item
     * (the assist group at order 1). This is the regression the probe caught:
     * with quote=0/comment=1, the assist item tied with comment and preceded it.
     */
    @Test
    public void ourItemsLeadTheMenu() {
        Menu menu = systemMenu();
        SelectionMenuPolicy.installItems(menu, LABELS);

        List<Integer> order = ids(menu);
        assertEquals(SelectionMenuPolicy.ID_QUOTE, (int) order.get(0));
        assertEquals(SelectionMenuPolicy.ID_COMMENT, (int) order.get(1));
        assertEquals(SelectionMenuPolicy.ID_SEARCH_FILE, (int) order.get(2));
    }

    @Test
    public void quotePrecedesComment() {
        Menu menu = systemMenu();
        SelectionMenuPolicy.installItems(menu, LABELS);

        List<Integer> order = ids(menu);
        assertTrue(order.indexOf(SelectionMenuPolicy.ID_QUOTE)
                < order.indexOf(SelectionMenuPolicy.ID_COMMENT));
    }

    /** The system's own entries must survive — we prepend, never replace. */
    @Test
    public void systemItemsArePreserved() {
        Menu menu = systemMenu();
        SelectionMenuPolicy.installItems(menu, LABELS);

        List<Integer> order = ids(menu);
        assertTrue(order.contains(android.R.id.cut));
        assertTrue(order.contains(android.R.id.copy));
        assertTrue(order.contains(android.R.id.selectAll));
        assertEquals(7, menu.size()); // 4 system + our 3
    }

    /**
     * onPrepareActionMode can run repeatedly; each pass must not append another
     * pair. Without the remove-then-add this ends with 4+ copies of our items.
     */
    @Test
    public void installIsIdempotent() {
        Menu menu = systemMenu();
        SelectionMenuPolicy.installItems(menu, LABELS);
        SelectionMenuPolicy.installItems(menu, LABELS);
        SelectionMenuPolicy.installItems(menu, LABELS);

        assertEquals(7, menu.size()); // 4 system + our 3, installed once
        assertEquals(SelectionMenuPolicy.ID_QUOTE, (int) ids(menu).get(0));
    }

    /** Labels come from the resolver, so the same code serves both locales. */
    @Test
    public void labelsComeFromTheResolver() {
        Menu menu = systemMenu();
        SelectionMenuPolicy.installItems(menu, LABELS);

        assertEquals("Quote", menu.findItem(SelectionMenuPolicy.ID_QUOTE).getTitle().toString());
        assertEquals("Comment", menu.findItem(SelectionMenuPolicy.ID_COMMENT).getTitle().toString());
    }

    @Test
    public void installToleratesANullMenu() {
        SelectionMenuPolicy.installItems(null, LABELS); // must not throw
    }

    // ── Constants ────────────────────────────────────────────────────────────

    /**
     * 0 is the only legal order strictly ahead of the system (which starts at
     * 1); negatives throw inside Menu.add (the int packs a category), so this
     * pins that both actions use exactly 0.
     */
    @Test
    public void allActionsUseOrderZero() {
        assertEquals(0, SelectionMenuPolicy.orderOf(SelectionMenuPolicy.Action.QUOTE));
        assertEquals(0, SelectionMenuPolicy.orderOf(SelectionMenuPolicy.Action.COMMENT));
        assertEquals(0, SelectionMenuPolicy.orderOf(SelectionMenuPolicy.Action.COPY));
    }

    @Test
    public void idsAreDistinctAndNonAdjacent() {
        assertTrue(SelectionMenuPolicy.ID_QUOTE != SelectionMenuPolicy.ID_COMMENT);
        assertTrue(SelectionMenuPolicy.ID_COPY != SelectionMenuPolicy.ID_QUOTE);
        assertTrue(SelectionMenuPolicy.ID_COPY != SelectionMenuPolicy.ID_COMMENT);
        // Non-adjacent on purpose: lets the tests below prove isOurItem is an
        // exact match, not a range check.
        assertTrue(Math.abs(SelectionMenuPolicy.ID_QUOTE - SelectionMenuPolicy.ID_COMMENT) > 1);
        assertTrue(Math.abs(SelectionMenuPolicy.ID_COPY - SelectionMenuPolicy.ID_QUOTE) > 1);
    }

    @Test
    public void idOfMatchesTheConstants() {
        assertEquals(SelectionMenuPolicy.ID_COPY,
                SelectionMenuPolicy.idOf(SelectionMenuPolicy.Action.COPY));
        assertEquals(SelectionMenuPolicy.ID_QUOTE,
                SelectionMenuPolicy.idOf(SelectionMenuPolicy.Action.QUOTE));
        assertEquals(SelectionMenuPolicy.ID_COMMENT,
                SelectionMenuPolicy.idOf(SelectionMenuPolicy.Action.COMMENT));
    }

    /**
     * Our ids must not collide with the framework's menu ids, which live in the
     * resource-id space (0x7f……). A collision would make {@link
     * SelectionMenuPolicy#isOurItem} claim a system item, and the wrapper would
     * swallow that click (Copy silently doing nothing).
     */
    @Test
    public void ourIdsSitOutsideTheFrameworkResourceIdRange() {
        assertTrue(SelectionMenuPolicy.ID_COPY < 0x7f000000);
        assertTrue(SelectionMenuPolicy.ID_QUOTE < 0x7f000000);
        assertTrue(SelectionMenuPolicy.ID_COMMENT < 0x7f000000);
    }

    // ── Classification ───────────────────────────────────────────────────────

    @Test
    public void ourIdsAreRecognised() {
        assertTrue(SelectionMenuPolicy.isOurItem(SelectionMenuPolicy.ID_COPY));
        assertTrue(SelectionMenuPolicy.isOurItem(SelectionMenuPolicy.ID_QUOTE));
        assertTrue(SelectionMenuPolicy.isOurItem(SelectionMenuPolicy.ID_COMMENT));
    }

    /**
     * The classification must be exact, not a range check: the system's own
     * items must fall through to the framework callback.
     */
    @Test
    public void systemIdsAreNotOurs() {
        assertFalse(SelectionMenuPolicy.isOurItem(android.R.id.copy));
        assertFalse(SelectionMenuPolicy.isOurItem(android.R.id.cut));
        assertFalse(SelectionMenuPolicy.isOurItem(android.R.id.selectAll));
        assertFalse(SelectionMenuPolicy.isOurItem(android.R.id.paste));
        assertFalse(SelectionMenuPolicy.isOurItem(0));
        assertFalse(SelectionMenuPolicy.isOurItem(-1));
    }

    /** Ids in the gap between ours must not be claimed (guards a range check). */
    @Test
    public void idsBetweenOursAreNotOurs() {
        assertFalse(SelectionMenuPolicy.isOurItem(SelectionMenuPolicy.ID_QUOTE + 1));
        assertFalse(SelectionMenuPolicy.isOurItem(SelectionMenuPolicy.ID_COMMENT - 1));
        assertFalse(SelectionMenuPolicy.isOurItem(SelectionMenuPolicy.ID_QUOTE - 1));
        assertFalse(SelectionMenuPolicy.isOurItem(SelectionMenuPolicy.ID_COMMENT + 1));
        assertFalse(SelectionMenuPolicy.isOurItem(SelectionMenuPolicy.ID_COPY + 1));
        assertFalse(SelectionMenuPolicy.isOurItem(SelectionMenuPolicy.ID_QUOTE - 2));
    }

    @Test
    public void actionForMapsOurIdsBack() {
        assertEquals(SelectionMenuPolicy.Action.COPY,
                SelectionMenuPolicy.actionFor(SelectionMenuPolicy.ID_COPY));
        assertEquals(SelectionMenuPolicy.Action.QUOTE,
                SelectionMenuPolicy.actionFor(SelectionMenuPolicy.ID_QUOTE));
        assertEquals(SelectionMenuPolicy.Action.COMMENT,
                SelectionMenuPolicy.actionFor(SelectionMenuPolicy.ID_COMMENT));
    }

    @Test
    public void actionForReturnsNullForSystemIds() {
        assertNull(SelectionMenuPolicy.actionFor(android.R.id.copy));
        assertNull(SelectionMenuPolicy.actionFor(0));
    }

    // ── Labels / JS action strings ───────────────────────────────────────────

    @Test
    public void eachActionHasItsOwnLabelResource() {
        assertTrue(SelectionMenuPolicy.labelRes(SelectionMenuPolicy.Action.QUOTE)
                != SelectionMenuPolicy.labelRes(SelectionMenuPolicy.Action.COMMENT));
    }

    /**
     * The action is called 讨论 ("Discuss") in Chinese, not 评论 ("Comment"):
     * it opens a free-form reply to the AI rather than a code-review comment.
     * The resource key stays {@code selection_action_comment} (and the JS action
     * string stays "comment") because those are the wire identifiers — only the
     * visible wording changed. Asserted against the REAL zh resource, so a
     * revert to 评论 fails here rather than on a device.
     */
    @Test
    public void discussActionIsLabelledDiscussInChinese() {
        String zh = RuntimeEnvironment.getApplication()
                .createConfigurationContext(zhConfig())
                .getString(R.string.selection_action_comment);
        assertEquals("讨论", zh);
    }

    /** A configuration forced to Simplified Chinese, for the test above. */
    private static android.content.res.Configuration zhConfig() {
        android.content.res.Configuration c =
                new android.content.res.Configuration(RuntimeEnvironment.getApplication().getResources().getConfiguration());
        c.setLocale(java.util.Locale.SIMPLIFIED_CHINESE);
        return c;
    }

    /**
     * The JS action string is the contract with the web app's handler. It must
     * be exactly the literal that handler switches on; a typo would make the
     * native item a silent no-op (the menu closes and nothing happens).
     */
    @Test
    public void jsActionStringsMatchTheWebHandler() {
        assertEquals("copy", SelectionMenuPolicy.jsAction(SelectionMenuPolicy.Action.COPY));
        assertEquals("quote", SelectionMenuPolicy.jsAction(SelectionMenuPolicy.Action.QUOTE));
        assertEquals("comment", SelectionMenuPolicy.jsAction(SelectionMenuPolicy.Action.COMMENT));
    }

    @Test
    public void everyActionIsFullyMapped() {
        for (SelectionMenuPolicy.Action action : SelectionMenuPolicy.Action.values()) {
            // Each of these throws for an unmapped action, so a new enum
            // constant that forgets a switch arm fails here rather than at
            // runtime on the device.
            SelectionMenuPolicy.orderOf(action);
            SelectionMenuPolicy.idOf(action);
            SelectionMenuPolicy.labelRes(action);
            SelectionMenuPolicy.jsAction(action);
            assertTrue(SelectionMenuPolicy.isOurItem(SelectionMenuPolicy.idOf(action)));
        }
    }

    /** MENU_ORDER must list exactly the long-press actions, quote first. */
    @Test
    public void menuOrderCoversAllLongPressActionsQuoteFirst() {
        assertEquals(3, SelectionMenuPolicy.MENU_ORDER.length);
        assertEquals(SelectionMenuPolicy.ID_QUOTE, SelectionMenuPolicy.MENU_ORDER[0]);
        assertEquals(SelectionMenuPolicy.ID_COMMENT, SelectionMenuPolicy.MENU_ORDER[1]);
        assertEquals(SelectionMenuPolicy.ID_SEARCH_FILE, SelectionMenuPolicy.MENU_ORDER[2]);
    }

    /**
     * "Find file" must be on the FREE-SELECTION (long-press) menu, not only on
     * the double-tap one: the whole point is to act on an arbitrary selected
     * string, which only the long-press flow produces.
     */
    @Test
    public void longPressMenuCarriesSearchFile() {
        Menu menu = systemMenu();
        SelectionMenuPolicy.installItems(menu, LABELS);

        assertTrue("the long-press menu must offer 搜索文件",
                menu.findItem(SelectionMenuPolicy.ID_SEARCH_FILE) != null);
    }

    // ── The programmatic (double-tap) menu ──────────────────────────────────

    /**
     * The long-press menu must NOT carry Copy: Chromium builds that menu and
     * already contributes its own Copy, so a second one would show the user two
     * identical entries.
     */
    @Test
    public void longPressMenuDoesNotInstallCopy() {
        Menu menu = systemMenu();
        SelectionMenuPolicy.installItems(menu, LABELS);

        assertNull("the long-press menu must leave Copy to Chromium",
                menu.findItem(SelectionMenuPolicy.ID_COPY));
        // The system's own Copy is still there, untouched.
        assertTrue(menu.findItem(android.R.id.copy) != null);
    }

    /**
     * The double-tap menu is ours alone, so it MUST carry Copy — nothing else
     * contributes to it. It leads, ahead of quote/comment.
     */
    @Test
    public void floatingMenuInstallsCopyWithAllThreeInOrder() {
        // A bare menu: the programmatic path builds it from scratch, so there is
        // no system Copy to rely on.
        Menu menu = new PopupMenu(RuntimeEnvironment.getApplication(), null).getMenu();

        SelectionMenuPolicy.installFloatingItems(menu, LABELS);

        assertEquals(4, menu.size());
        assertEquals(SelectionMenuPolicy.ID_COPY, (int) ids(menu).get(0));
        assertEquals(SelectionMenuPolicy.ID_QUOTE, (int) ids(menu).get(1));
        assertEquals(SelectionMenuPolicy.ID_COMMENT, (int) ids(menu).get(2));
        assertEquals(SelectionMenuPolicy.ID_SEARCH_FILE, (int) ids(menu).get(3));
        assertTrue(menu.findItem(SelectionMenuPolicy.ID_COPY) != null);
    }

    /** installFloatingItems runs on every prepare pass; it must stay idempotent. */
    @Test
    public void floatingInstallIsIdempotent() {
        Menu menu = new PopupMenu(RuntimeEnvironment.getApplication(), null).getMenu();
        SelectionMenuPolicy.installFloatingItems(menu, LABELS);
        SelectionMenuPolicy.installFloatingItems(menu, LABELS);
        SelectionMenuPolicy.installFloatingItems(menu, LABELS);

        assertEquals(4, menu.size());
    }

    /** FLOATING_MENU_ORDER must list all actions, copy first. */
    @Test
    public void floatingMenuOrderCoversAllActionsCopyFirst() {
        assertEquals(4, SelectionMenuPolicy.FLOATING_MENU_ORDER.length);
        assertEquals(SelectionMenuPolicy.ID_COPY, SelectionMenuPolicy.FLOATING_MENU_ORDER[0]);
        assertEquals(SelectionMenuPolicy.ID_QUOTE, SelectionMenuPolicy.FLOATING_MENU_ORDER[1]);
        assertEquals(SelectionMenuPolicy.ID_COMMENT, SelectionMenuPolicy.FLOATING_MENU_ORDER[2]);
        assertEquals(SelectionMenuPolicy.ID_SEARCH_FILE,
                SelectionMenuPolicy.FLOATING_MENU_ORDER[3]);
    }

    /**
     * The JS action literal must be "search-file" — the web handler switches on
     * that exact string, so a different spelling would silently do nothing.
     */
    @Test
    public void searchFileMapsToTheJsLiteralTheWebAppHandles() {
        assertEquals("search-file",
                SelectionMenuPolicy.jsAction(SelectionMenuPolicy.Action.SEARCH_FILE));
    }

    /** 搜索文件 in Chinese, 搜索文件's English counterpart otherwise. */
    @Test
    public void searchFileLabelIsLocalised() {
        String zh = RuntimeEnvironment.getApplication()
                .createConfigurationContext(zhConfig())
                .getString(R.string.selection_action_search_file);
        assertEquals("搜索文件", zh);
    }

    /**
     * Copy has its own label resource — reusing quote's or comment's would show
     * the wrong word on the double-tap menu.
     */
    @Test
    public void copyHasItsOwnLabelResource() {
        int copy = SelectionMenuPolicy.labelRes(SelectionMenuPolicy.Action.COPY);
        assertTrue(copy != SelectionMenuPolicy.labelRes(SelectionMenuPolicy.Action.QUOTE));
        assertTrue(copy != SelectionMenuPolicy.labelRes(SelectionMenuPolicy.Action.COMMENT));
    }

    /** The copy label is 复制 in Chinese, matching the web app's own wording. */
    @Test
    public void copyActionIsLabelledCopyInChinese() {
        String zh = RuntimeEnvironment.getApplication()
                .createConfigurationContext(zhConfig())
                .getString(R.string.selection_action_copy);
        assertEquals("复制", zh);
    }
}
