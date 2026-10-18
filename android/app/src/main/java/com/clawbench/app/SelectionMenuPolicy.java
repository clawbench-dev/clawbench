package com.clawbench.app;

import android.view.Menu;
import android.view.MenuItem;

/**
 * Pure decision logic for the custom items we add to the Android WebView's
 * text-selection menu.
 *
 * Extracted from {@link ClawBenchWebView} so the rules can be unit-tested
 * without an Android runtime — the same reason {@link NativeNotificationPolicy}
 * exists.
 *
 * Background: a WebView has no {@code setCustomSelectionActionModeCallback}
 * (that is a {@code TextView} API). The only hook is overriding
 * {@code View.startActionMode}, which means we must wrap the framework's
 * callback rather than replace it — replacing it drops the system's own
 * Copy / Select all / Share entries.
 *
 * ## Ordering (empirically verified with Robolectric, not assumed)
 *
 * Chromium's own {@code select_action_menu.xml} assigns {@code orderInCategory}
 * 1 to the smart-selection "assist" group and 2..8 to cut/copy/paste/share/
 * select-all/paste-as-plain/web-search. A probe against the real
 * {@code MenuBuilder} showed:
 *
 * <pre>
 *   ours at 0 and 1 -> [quote(0), assist(1), comment(1), cut(2), copy(3)]
 * </pre>
 *
 * i.e. putting COMMENT at 1 TIES with the assist item, and because the framework
 * populates its own items before our wrapper appends, the system wins the tie
 * and pushes comment back. Order 0 is the only slot strictly ahead of
 * everything the system contributes.
 *
 * Negative orders are NOT an option: {@code Menu.add}'s order argument packs a
 * category in its high 16 bits, and a negative value decodes to an out-of-range
 * category — {@code IllegalArgumentException: order does not contain a valid
 * category}.
 *
 * So BOTH items use order 0, and their mutual order is the menu's own tie-break
 * for equal orders, which is stable in our favour either way: we add quote
 * first, and quote's id is the smaller of the two.
 *
 * Note this orders the items but does not guarantee both are VISIBLE: the
 * floating toolbar packs items by width and pushes the tail into an overflow
 * menu when space runs out. A very narrow selection can still hide ours.
 */
public final class SelectionMenuPolicy {

    private SelectionMenuPolicy() {
    }

    /** The actions we contribute, in the order they should appear. */
    public enum Action {
        /**
         * Copy the selection. Only ever installed on the PROGRAMMATIC
         * (double-tap) menu — see {@link #FLOATING_MENU_ORDER}.
         *
         * The long-press menu must NOT carry it: that menu is Chromium's own,
         * which already contributes Copy, and adding a second one would show
         * the user two identical entries.
         */
        COPY,
        QUOTE,
        COMMENT,
        /**
         * Treat the selected text as a file path and reveal it in the file
         * manager — the same action as tapping a dashed (annotated) path in
         * rendered markdown, but for ANY string the user selects, including
         * text the renderer did not annotate.
         */
        SEARCH_FILE
    }

    /**
     * Item ids for our entries, in menu order.
     *
     * Kept above the framework's resource-id space (system menu items are
     * {@code R.id.*}, i.e. 0x7f……) so {@link #isOurItem} cannot mistake a
     * system entry for ours. The ids are deliberately NOT adjacent: a
     * non-adjacent set lets a test prove the classification is an exact match
     * rather than a range check (an id in a gap must be rejected).
     *
     * Plain ints rather than {@code Menu.FIRST} so this file — and its test —
     * stay free of Android imports.
     */
    public static final int ID_COPY = 0x00010001;
    public static final int ID_QUOTE = 0x00010011;
    public static final int ID_COMMENT = 0x00010021;
    public static final int ID_SEARCH_FILE = 0x00010031;

    /**
     * The items the LONG-PRESS wrapper installs, in order.
     *
     * Copy is absent on purpose: Chromium's selection controller builds that
     * menu and already provides it (see {@link Action#COPY}).
     */
    public static final int[] MENU_ORDER = {ID_QUOTE, ID_COMMENT, ID_SEARCH_FILE};

    /**
     * The items the PROGRAMMATIC (double-tap) menu installs, in order.
     *
     * This menu is ours from scratch — nothing else contributes to it — so it
     * has to carry Copy itself.
     *
     * Copy comes LAST, not first, so the two menus agree on what the user sees.
     * On the long-press menu Chromium contributes Copy itself, and it lands in
     * the system group (orderInCategory 3) AFTER our items (order 0):
     *
     * <pre>
     *   long-press: 引用 讨论 搜索文件 | assist 剪切 复制 粘贴 分享 全选 …
     *   double-tap: 引用 讨论 搜索文件 复制
     * </pre>
     *
     * The shared prefix — the items WE add — is therefore identical on both
     * menus, and Copy occupies the same "system action" slot at the end. Putting
     * Copy first made the double-tap menu start with an entry the long-press
     * menu shows fifth, which is the inconsistency this order removes.
     */
    public static final int[] FLOATING_MENU_ORDER = {
            ID_QUOTE, ID_COMMENT, ID_SEARCH_FILE, ID_COPY};

    /**
     * Menu order for an action — always 0, for both.
     *
     * 0 is the lowest legal value (negatives throw) and the only one strictly
     * ahead of the system's lowest, which is 1 (the assist group). Using a
     * distinct value for the second item would tie it with a system item and
     * lose the tie-break.
     */
    public static int orderOf(Action action) {
        switch (action) {
            case COPY:
            case QUOTE:
            case COMMENT:
            case SEARCH_FILE:
                return 0;
            default:
                throw new IllegalArgumentException("unknown action: " + action);
        }
    }

    /** The menu item id for an action. */
    public static int idOf(Action action) {
        switch (action) {
            case COPY:
                return ID_COPY;
            case QUOTE:
                return ID_QUOTE;
            case COMMENT:
                return ID_COMMENT;
            case SEARCH_FILE:
                return ID_SEARCH_FILE;
            default:
                throw new IllegalArgumentException("unknown action: " + action);
        }
    }

    /**
     * Whether a clicked menu item is one of ours.
     *
     * The wrapper uses this to decide between handling the click itself and
     * delegating to the framework callback — getting it wrong either swallows a
     * system action (Copy does nothing) or double-handles one.
     */
    public static boolean isOurItem(int itemId) {
        return itemId == ID_COPY || itemId == ID_QUOTE
                || itemId == ID_COMMENT || itemId == ID_SEARCH_FILE;
    }

    /** The action for one of our item ids, or null when the id is not ours. */
    public static Action actionFor(int itemId) {
        if (itemId == ID_COPY) return Action.COPY;
        if (itemId == ID_QUOTE) return Action.QUOTE;
        if (itemId == ID_COMMENT) return Action.COMMENT;
        if (itemId == ID_SEARCH_FILE) return Action.SEARCH_FILE;
        return null;
    }

    /** String resource id for an action's label. */
    public static int labelRes(Action action) {
        switch (action) {
            case COPY:
                return R.string.selection_action_copy;
            case QUOTE:
                return R.string.selection_action_quote;
            case COMMENT:
                return R.string.selection_action_comment;
            case SEARCH_FILE:
                return R.string.selection_action_search_file;
            default:
                throw new IllegalArgumentException("unknown action: " + action);
        }
    }

    /** Supplies the already-localised label for an action. */
    public interface LabelResolver {
        String labelFor(Action action);
    }

    /**
     * Insert our two items into a selection menu, idempotently.
     *
     * Called from BOTH {@code onCreateActionMode} and {@code onPrepareActionMode}
     * — the framework may invoke the latter repeatedly (every
     * {@code ActionMode.invalidate()}), so the remove-then-add is not defensive
     * noise: without it each prepare pass would append another duplicate pair.
     *
     * Order is {@link #orderOf} (0 for both), and the items are added in
     * {@link #MENU_ORDER} so quote precedes comment.
     *
     * {@code SHOW_AS_ACTION_ALWAYS} asks the floating toolbar to keep them as
     * buttons rather than fold them into the overflow; the toolbar may still
     * overflow them when the selection is too narrow, which is unavoidable.
     *
     * Takes a {@link LabelResolver} rather than a {@code Context} so this
     * method — the part with real logic — is exercised by a plain test against
     * a real {@code MenuBuilder}, with no resource lookup and no device.
     */
    public static void installItems(Menu menu, LabelResolver labels) {
        install(menu, labels, MENU_ORDER);
    }

    /**
     * Insert the items for the PROGRAMMATIC (double-tap) menu, idempotently.
     *
     * Separate entry point from {@link #installItems} because the two menus
     * carry different sets: this one must include Copy (nothing else contributes
     * to it), while the long-press menu must not (Chromium already adds one).
     * Both share {@link #install} so the ordering/idempotency rules live once.
     */
    public static void installFloatingItems(Menu menu, LabelResolver labels) {
        install(menu, labels, FLOATING_MENU_ORDER);
    }

    private static void install(Menu menu, LabelResolver labels, int[] order) {
        if (menu == null) return;
        for (int id : order) {
            Action action = actionFor(id);
            if (action == null) continue;
            // Idempotent: drop any previous copy before re-adding, so a second
            // onPrepareActionMode pass cannot duplicate the items.
            menu.removeItem(id);
            MenuItem item = menu.add(Menu.NONE, id, orderOf(action), labels.labelFor(action));
            item.setShowAsAction(MenuItem.SHOW_AS_ACTION_ALWAYS);
        }
    }

    /**
     * The JS action string passed to the frontend bridge.
     *
     * Deliberately the same spelling the web app's handler switches on, so the
     * two sides cannot drift into different literals.
     *
     * "copy" is never sent over the bridge — the view copies natively — but it
     * is mapped here anyway so {@link #jsAction} stays total over the enum and
     * a future change that routes copy through JS cannot silently miss it.
     */
    public static String jsAction(Action action) {
        switch (action) {
            case COPY:
                return "copy";
            case QUOTE:
                return "quote";
            case COMMENT:
                return "comment";
            case SEARCH_FILE:
                return "search-file";
            default:
                throw new IllegalArgumentException("unknown action: " + action);
        }
    }
}
