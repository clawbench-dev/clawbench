package com.clawbench.app;

import android.animation.ObjectAnimator;
import android.content.Context;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.FrameLayout;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

import java.util.concurrent.atomic.AtomicLong;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.function.BiConsumer;

/**
 * Grouped session-list panel for the desktop floating status window.
 *
 * Renders the /api/ai/sessions/overview response as a scrollable list grouped
 * by project. UI is built in code (no XML): a header row with the shared
 * capsule content (logo + live stat counts, see FloatingStatusContentView)
 * and a collapse (chevron-up) button, followed by per-project group headers and
 * session rows. Each session row shows a tri-color status indicator, a
 * single-line ellipsized title, and a red circular unread badge when
 * unreadCount > 0. Tapping a row invokes the onSessionClick callback.
 *
 * Status dots follow a fixed priority (yellow > green > blue):
 *   - PENDING  (yellow): pendingApproval, regardless of running/unread
 *   - RUNNING  (green):  running && !pendingApproval — the ring-arc spins
 *   - UNREAD   (blue):   !running && !pendingApproval && unreadCount > 0
 *   - NONE:     no dot
 * The decision is the static pure function statusDotKind(SessionItem) so it
 * is unit-testable without an Android framework.
 *
 * The panel's height is content-driven: the controller measures
 * measureContentHeight(widthPx) after rendering and clamps it to the screen.
 * constrainListHeight caps the inner ScrollView at (panel - header) so the
 * list scrolls instead of stretching the window.
 *
 * The static buildGroups pure function parses overview JSON into model lists
 * with no Android framework dependency (org.json + plain lists), so it is
 * unit-testable with plain JUnit.
 *
 * Drag-to-move is NOT handled here: the controller owns WindowManager /
 * LayoutParams and attaches its own touch listener (Task 5). This view only
 * exposes content rendering plus the session-click and collapse callbacks.
 */
public class FloatingStatusPanelView extends FrameLayout {

    // Colors as inline ARGB literals to keep pure functions framework-free.
    private static final int COLOR_RUNNING = 0xFF00CC00; // green
    private static final int COLOR_PERMISSION_PENDING = 0xFFE6A23C; // yellow
    private static final int COLOR_UNREAD = 0xFF3B82F6; // blue

    // github-dark fallback palette (overridden at construction by the
    // persisted theme palette via FloatingThemeColors).
    private static final int COLOR_BORDER = 0xFF30363D;

    // Layout constants.
    private static final int PANEL_WIDTH_DP = 280;
    private static final int CORNER_RADIUS_DP = 20;
    private static final int PADDING_H_DP = 14;
    private static final int PADDING_V_DP = 10;
    private static final int COLLAPSE_BTN_SIZE_DP = 22;
    /** Icon size inside the collapse button (the touch target stays larger). */
    private static final int COLLAPSE_BTN_ICON_SIZE_DP = 16;
    private static final int PROJECT_HEADER_SIZE_SP = 11;
    private static final int PROJECT_HEADER_PADDING_TOP_DP = 10;
    /** Gap between the bold project name and the following path in a group header. */
    private static final int PROJECT_HEADER_NAME_PATH_GAP_DP = 6;
    private static final int SESSION_ROW_PADDING_TOP_DP = 10;
    private static final int SESSION_TITLE_SIZE_SP = 13;
    private static final int DOT_SIZE_DP = 8;
    private static final int DOT_MARGIN_END_DP = 8;

    // Spin animation for a running session's green ring-arc (same rhythm as
    // the capsule's running indicator in FloatingStatusView).
    private static final long SPIN_MS = 900;
    // Skeleton placeholder rows breathe (alpha pulse) while loading.
    private static final float SKELETON_ALPHA_MAX = 1.0f;
    private static final long SKELETON_BREATH_MS = 800;

    // Skeleton loading row layout.
    private static final int SKELETON_ROWS = 4;
    private static final int SKELETON_DOT_SIZE_DP = 8;
    private static final int SKELETON_BAR_HEIGHT_DP = 10;
    private static final int SKELETON_BAR_MARGIN_END_DP = 26;
    private static final int SKELETON_ROW_PADDING_TOP_DP = 10;
    private static final float SKELETON_BAR_ALPHA = 0.35f;

    /** Per-instance animation counter so overlapping skeleton breaths serialize. */
    private final AtomicLong skeletonBreatheSeq = new AtomicLong();
    /** True while the list shows skeleton placeholder rows. UI thread only. */
    private boolean skeletonShowing;

    /**
     * Status-dot kind for a session row. Pure: no framework deps.
     *
     * Priority is yellow > green > blue (pending approval needs user action,
     * then running activity, then unread content).
     */
    public enum StatusDotKind {
        /** Pending approval (yellow) — wins over running and unread. */
        PENDING,
        /** Running without pending approval (green, spinning ring-arc). */
        RUNNING,
        /** Idle with unread messages (blue). */
        UNREAD,
        /** No dot. */
        NONE
    }

    private final float density;
    private final FloatingStatusContentView headerContentView;
    private final LinearLayout headerLayout;
    private final LinearLayout listContainer;
    private final ScrollView scrollView;
    private final int colorTextPrimary;
    private final int colorTextSecondary;
    /** Theme-derived gray used for skeleton placeholder rows (mix of textSecondary with the panel bg). */
    private final int skeletonGray;
    private Runnable onCollapseClick;
    /** Running ring-arc views currently spinning; stopped when rows are rebuilt. */
    private final List<View> spinningDots = new ArrayList<>();

    /**
     * A single session as it appears in the overview list.
     */
    public static class SessionItem {
        public final String id;
        public final String title;
        public final boolean running;
        public final boolean pendingApproval;
        public final int unreadCount;

        public SessionItem(String id, String title, boolean running,
                           boolean pendingApproval, int unreadCount) {
            this.id = id;
            this.title = title;
            this.running = running;
            this.pendingApproval = pendingApproval;
            this.unreadCount = unreadCount;
        }
    }

    /**
     * Sessions grouped under one project path.
     */
    public static class ProjectGroup {
        public final String name;
        public final List<SessionItem> sessions;

        public ProjectGroup(String name, List<SessionItem> sessions) {
            this.name = name;
            this.sessions = sessions;
        }
    }

    /**
     * Parse a /api/ai/sessions/overview JSON object into project groups.
     * Pure: no instance fields, no framework deps. Returns an empty list for
     * null input, a missing "projects" key, or empty groups (defensive so the
     * UI never has to special-case malformed payloads).
     */
    public static List<ProjectGroup> buildGroups(JSONObject overview) {
        List<ProjectGroup> groups = new ArrayList<>();
        if (overview == null) {
            return groups;
        }
        JSONArray projects = overview.optJSONArray("projects");
        if (projects == null) {
            return groups;
        }
        for (int i = 0; i < projects.length(); i++) {
            JSONObject project = projects.optJSONObject(i);
            if (project == null) {
                continue;
            }
            List<SessionItem> sessions = new ArrayList<>();
            JSONArray sessionArray = project.optJSONArray("sessions");
            if (sessionArray != null) {
                for (int j = 0; j < sessionArray.length(); j++) {
                    JSONObject s = sessionArray.optJSONObject(j);
                    if (s == null) {
                        continue;
                    }
                    sessions.add(new SessionItem(
                            s.optString("id", ""),
                            s.optString("title", ""),
                            s.optBoolean("running", false),
                            s.optBoolean("pendingApproval", false),
                            s.optInt("unreadCount", 0)));
                }
            }
            if (!sessions.isEmpty()) {
                groups.add(new ProjectGroup(project.optString("name", ""), sessions));
            }
        }
        return groups;
    }

    /**
     * Decide the status-dot kind for a session. Pure: no framework deps.
     * Pending (yellow) wins over running (green); either wins over unread
     * (blue). Sessions that are neither running nor pending with no unread get
     * no dot.
     */
    public static StatusDotKind statusDotKind(SessionItem s) {
        if (s.pendingApproval) {
            return StatusDotKind.PENDING;
        }
        if (s.running) {
            return StatusDotKind.RUNNING;
        }
        if (s.unreadCount > 0) {
            return StatusDotKind.UNREAD;
        }
        return StatusDotKind.NONE;
    }

    public FloatingStatusPanelView(Context context, BiConsumer<String, String> onSessionClick) {
        super(context);
        density = getResources().getDisplayMetrics().density;

        // Theme palette read once at construction (floating window rebuilds on
        // theme change pick up the new colors).
        int[] palette = FloatingThemeColors.get(context);
        int bgColor = (palette[0] & 0x00FFFFFF) | 0xF0000000; // keep ~94% opacity
        // Border derived from the background color via a luminance nudge so the
        // panel edge is visible on both light and dark themes while staying in
        // the background's hue family.
        int borderColor = FloatingThemeColors.borderColorFromBackground(palette[0]);
        colorTextPrimary = palette[1];
        colorTextSecondary = palette[2];
        // Skeleton gray: the text-secondary hue mixed toward the panel
        // background, so the placeholder is visible on both dark and light
        // themes while staying clearly fainter than real content.
        skeletonGray = mixArgb(colorTextSecondary, bgColor, 0.5f);

        // Background: rounded translucent theme panel with a thin border.
        GradientDrawable bg = new GradientDrawable();
        bg.setColor(bgColor);
        bg.setCornerRadius(dp(CORNER_RADIUS_DP));
        bg.setStroke(dp(1), borderColor);
        setBackground(bg);
        setPadding(dp(PADDING_H_DP), dp(PADDING_V_DP), dp(PADDING_H_DP), dp(PADDING_V_DP));

        LinearLayout root = new LinearLayout(context);
        root.setOrientation(LinearLayout.VERTICAL);

        // Header: shared stats content row (same logo + count items as the
        // collapsed capsule, left-aligned) + collapse button. The content row
        // is built once here and kept across renders, so the capsule visuals
        // and their spin animation are stable while the session list
        // below is rebuilt. Its intrinsic height (24dp logo) plus the panel's
        // 10dp vertical padding yields a ~44dp header bar; constrainListHeight
        // accounts for the measured header height.
        LinearLayout header = new LinearLayout(context);
        header.setOrientation(LinearLayout.HORIZONTAL);
        header.setGravity(Gravity.CENTER_VERTICAL);
        headerLayout = header;
        headerContentView = new FloatingStatusContentView(context);
        header.addView(headerContentView, new LinearLayout.LayoutParams(
                0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));

        // Collapse button: a chevron-up icon (Web's ChevronUp) that collapses
        // the expanded panel back to the capsule. Tinted with the theme's
        // secondary text color so it matches the rest of the header on both
        // light and dark themes.
        ImageView collapseBtn = new ImageView(context);
        collapseBtn.setImageResource(R.drawable.ic_panel_collapse);
        collapseBtn.setColorFilter(colorTextSecondary);
        collapseBtn.setScaleType(ImageView.ScaleType.FIT_CENTER);
        // Keep the 24dp icon visually smaller (16dp) inside the 22dp touch
        // target via symmetric padding.
        int iconPad = dp((COLLAPSE_BTN_SIZE_DP - COLLAPSE_BTN_ICON_SIZE_DP) / 2);
        collapseBtn.setPadding(iconPad, iconPad, iconPad, iconPad);
        collapseBtn.setClickable(true);
        collapseBtn.setOnClickListener(v -> {
            if (onCollapseClick != null) {
                onCollapseClick.run();
            }
        });
        header.addView(collapseBtn, new LinearLayout.LayoutParams(
                dp(COLLAPSE_BTN_SIZE_DP), dp(COLLAPSE_BTN_SIZE_DP)));
        root.addView(header, new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT));

        // Scrollable session list. The scroll area's height is capped by
        // constrainListHeight so the whole panel stays at the content height
        // (and scrolls once content exceeds the window).
        scrollView = new ScrollView(context);
        scrollView.setVerticalScrollBarEnabled(false);
        listContainer = new LinearLayout(context);
        listContainer.setOrientation(LinearLayout.VERTICAL);
        scrollView.addView(listContainer, new ScrollView.LayoutParams(
                ScrollView.LayoutParams.MATCH_PARENT, ScrollView.LayoutParams.WRAP_CONTENT));
        root.addView(scrollView, new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT));

        addView(root, new LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT));

        // Panel width: fixed ~280dp. Height is content-driven and set by the
        // controller after render (measureContentHeight + clamp to screen).
        LayoutParams selfLp = (LayoutParams) getLayoutParams();
        if (selfLp == null) {
            selfLp = new LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT);
        }
        selfLp.width = dp(PANEL_WIDTH_DP);
        selfLp.height = LayoutParams.WRAP_CONTENT;
        setLayoutParams(selfLp);
    }

    public void setOnCollapseClickListener(Runnable onCollapseClick) {
        this.onCollapseClick = onCollapseClick;
    }

    /**
     * Rebuild the panel content from an overview JSON object. Safe to call on
     * the UI thread; replaces the entire list so refreshes never accumulate
     * stale rows. Running-dot spin is restarted for the new rows (and
     * stopped for any rows discarded by this rebuild).
     *
     * @param onSessionClick receives (sessionId, projectPath) for the tapped
     *                       session; projectPath is the owning ProjectGroup.name
     *                       so cross-project deep links can pass it through.
     */
    public void render(JSONObject overview, BiConsumer<String, String> onSessionClick) {
        List<ProjectGroup> groups = buildGroups(overview);
        AppLog.d("FloatingPanelView", "render groups=" + groups.size());

        // The title bar shows the shared content row (logo + stats). Compute
        // the stats from the overview the same way the capsule does, so the
        // panel header and the collapsed capsule always agree. The three
        // groups are mutually exclusive: pending wins over running for
        // both-flag sessions; unread only counts idle sessions with unread.
        int runningCount = 0;
        int pendingCount = 0;
        int unreadCount = 0;
        for (ProjectGroup g : groups) {
            for (SessionItem s : g.sessions) {
                if (s.pendingApproval) {
                    pendingCount++;
                } else if (s.running) {
                    runningCount++;
                } else if (s.unreadCount > 0) {
                    unreadCount++;
                }
            }
        }
        // Stop stale row/header animations BEFORE re-rendering so the header's
        // spin restarts below (renderHeaderStats starts it again). Ordering
        // matters: stopBreathing() cancels the header animator, so it must run
        // before renderHeaderStats re-arms it — otherwise the title-bar ring
        // never spins.
        stopBreathing();
        renderHeaderStats(runningCount, pendingCount, unreadCount);

        listContainer.removeAllViews();
        spinningDots.clear();
        for (ProjectGroup group : groups) {
            listContainer.addView(buildProjectHeader(group.name));
            for (SessionItem session : group.sessions) {
                listContainer.addView(buildSessionRow(session, group.name, onSessionClick));
            }
        }
        startBreathing();
    }

    /**
     * Show a skeleton (placeholder) list while the first overview is loading.
     * Builds {@value #SKELETON_ROWS} pseudo session rows — a small dot plus a
     * rounded title bar in a theme-derived gray — that breathe between 35%
     * and full opacity, then removes them when the real overview renders.
     * This replaces the previous blank gap between the panel opening and the
     * first /api/ai/sessions/overview round trip. Safe to call repeatedly;
     * each call rebuilds the rows. UI thread only.
     */
    public void showSkeleton() {
        stopBreathing();
        listContainer.removeAllViews();
        spinningDots.clear();

        for (int i = 0; i < SKELETON_ROWS; i++) {
            listContainer.addView(buildSkeletonRow());
        }
        skeletonShowing = true;
        skeletonBreatheSeq.incrementAndGet();
        breatheSkeleton(listContainer, skeletonBreatheSeq.get());
    }

    /**
     * Remove the skeleton rows. No-op when the list already holds real
     * content (or is empty from a no-session overview). UI thread only.
     */
    public void hideSkeleton() {
        if (!skeletonShowing) {
            return;
        }
        cancelSkeletonBreath();
        listContainer.removeAllViews();
        skeletonShowing = false;
    }

    /** True when the list currently shows skeleton placeholder rows. UI thread only. */
    public boolean isSkeletonShowing() {
        return skeletonShowing;
    }

    private View buildSkeletonRow() {
        LinearLayout row = new LinearLayout(getContext());
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.CENTER_VERTICAL);

        View dot = new View(getContext());
        GradientDrawable dotDrawable = new GradientDrawable();
        dotDrawable.setShape(GradientDrawable.OVAL);
        dotDrawable.setColor(skeletonGray);
        dot.setBackground(dotDrawable);
        LinearLayout.LayoutParams dotLp = new LinearLayout.LayoutParams(
                dp(SKELETON_DOT_SIZE_DP), dp(SKELETON_DOT_SIZE_DP));
        dotLp.setMargins(0, 0, dp(DOT_MARGIN_END_DP), 0);
        row.addView(dot, dotLp);

        View bar = new View(getContext());
        GradientDrawable barDrawable = new GradientDrawable();
        barDrawable.setColor(skeletonGray);
        barDrawable.setCornerRadius(dp(SKELETON_BAR_HEIGHT_DP) / 2f);
        bar.setBackground(barDrawable);
        bar.setAlpha(SKELETON_BAR_ALPHA);
        LinearLayout.LayoutParams barLp = new LinearLayout.LayoutParams(
                0, dp(SKELETON_BAR_HEIGHT_DP), 1f);
        barLp.setMargins(0, 0, dp(SKELETON_BAR_MARGIN_END_DP), 0);
        row.addView(bar, barLp);

        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        lp.topMargin = dp(SKELETON_ROW_PADDING_TOP_DP);
        row.setLayoutParams(lp);
        return row;
    }

    /**
     * Breathe the whole skeleton between 35% and full opacity. The rows pulse
     * as one unit so the placeholder reads as "content is coming" rather than
     * as moving list items. The animator re-checks the calling sequence on
     * every frame, so a newer showSkeleton()/hideSkeleton() call cancels it.
     * UI thread only.
     */
    private void breatheSkeleton(ViewGroup container, long seq) {
        ObjectAnimator anim = ObjectAnimator.ofFloat(container, "alpha",
                SKELETON_BAR_ALPHA, SKELETON_ALPHA_MAX);
        anim.setDuration(SKELETON_BREATH_MS * 2);
        anim.setRepeatCount(ObjectAnimator.INFINITE);
        anim.setRepeatMode(ObjectAnimator.REVERSE);
        anim.addUpdateListener(a -> {
            if (skeletonBreatheSeq.get() != seq) {
                a.removeAllUpdateListeners();
                a.cancel();
            }
        });
        anim.start();
        container.setTag(anim);
    }

    /**
     * Cancel an in-flight skeleton breath animation and restore the list to
     * full opacity, so the pulsing does not leak onto real content. Called by
     * stopBreathing() (every render and teardown) and hideSkeleton(). UI
     * thread only.
     */
    private void cancelSkeletonBreath() {
        skeletonBreatheSeq.incrementAndGet();
        Object tag = listContainer.getTag();
        if (tag instanceof ObjectAnimator) {
            ((ObjectAnimator) tag).cancel();
            listContainer.setTag(null);
        }
        listContainer.setAlpha(1f);
        // Any render/teardown path cancels the placeholder state too, so the
        // flag cannot stick after real content (or an empty overview) lands.
        skeletonShowing = false;
    }

    /**
     * Render the stats into the title bar's shared content row (logo + three
     * count groups), mirroring the collapsed capsule. The content row is
     * stable across renders — only the session list below is rebuilt.
     */
    public void renderHeaderStats(int running, int pending, int unread) {
        headerContentView.renderStats(running, pending, unread);
    }

    /**
     * Measure the panel's desired height for its current content at the given
     * width. The panel is laid out at width x 0 so the header and list compute
     * their intrinsic heights; the result is the content height including
     * padding. UI thread only.
     *
     * The scroll area's height is temporarily reset to WRAP_CONTENT: a previous
     * constrainListHeight() set a fixed height, and a fixed LayoutParams height
     * would make the parent generate an EXACTLY measure spec that clamps the
     * scroll area to that old (possibly 0) value — hiding the session rows from
     * the measurement. Restoring WRAP_CONTENT lets the list report its true
     * content height, then constrainListHeight() re-caps it for the final size.
     */
    public int measureContentHeight(int widthPx) {
        ViewGroup.LayoutParams svLp = scrollView.getLayoutParams();
        int savedScrollHeight = svLp.height;
        if (savedScrollHeight != ViewGroup.LayoutParams.WRAP_CONTENT) {
            svLp.height = ViewGroup.LayoutParams.WRAP_CONTENT;
            scrollView.setLayoutParams(svLp);
        }
        try {
            measure(
                    View.MeasureSpec.makeMeasureSpec(widthPx, View.MeasureSpec.EXACTLY),
                    View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED));
            return getMeasuredHeight();
        } finally {
            if (savedScrollHeight != ViewGroup.LayoutParams.WRAP_CONTENT) {
                svLp.height = savedScrollHeight;
                scrollView.setLayoutParams(svLp);
            }
        }
    }

    /**
     * Cap the inner scroll area so the whole panel's height stays at targetPx:
     * the scroll area gets exactly (targetPx - fixed header height - padding),
     * and the list scrolls once content exceeds that. UI thread only.
     */
    public void constrainListHeight(int targetPx) {
        int fixedPx = headerLayout.getMeasuredHeight()
                + getPaddingTop() + getPaddingBottom();
        int maxScrollPx = Math.max(0, targetPx - fixedPx);
        ViewGroup.LayoutParams lp = scrollView.getLayoutParams();
        lp.height = maxScrollPx;
        scrollView.setLayoutParams(lp);
    }

    private View buildProjectHeader(String name) {
        // Project group header: the short project name (last path segment) in
        // bold primary text leads, followed by the full path in regular
        // secondary text. The path is given the remaining width and ellipsizes
        // on one line, so a long path never pushes the name off-screen.
        LinearLayout header = new LinearLayout(getContext());
        header.setOrientation(LinearLayout.HORIZONTAL);
        header.setGravity(Gravity.CENTER_VERTICAL);

        TextView nameView = new TextView(getContext());
        nameView.setText(baseName(name));
        nameView.setTextSize(PROJECT_HEADER_SIZE_SP);
        nameView.setTextColor(colorTextPrimary);
        nameView.setTypeface(Typeface.DEFAULT_BOLD);
        nameView.setIncludeFontPadding(false);
        nameView.setSingleLine(true);
        header.addView(nameView, new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT));

        TextView pathView = new TextView(getContext());
        pathView.setText(name);
        pathView.setTextSize(PROJECT_HEADER_SIZE_SP);
        pathView.setTextColor(colorTextSecondary);
        pathView.setTypeface(Typeface.DEFAULT);
        pathView.setIncludeFontPadding(false);
        pathView.setSingleLine(true);
        pathView.setMaxLines(1);
        pathView.setEllipsize(android.text.TextUtils.TruncateAt.END);
        LinearLayout.LayoutParams pathLp = new LinearLayout.LayoutParams(
                0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f);
        pathLp.setMargins(dp(PROJECT_HEADER_NAME_PATH_GAP_DP), 0, 0, 0);
        header.addView(pathView, pathLp);

        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        lp.topMargin = dp(PROJECT_HEADER_PADDING_TOP_DP);
        header.setLayoutParams(lp);
        return header;
    }

    /**
     * Extract the last path segment (project name) from a project path, e.g.
     * "/home/user/proj" -> "proj". Pure: handles both '/' and '\' separators
     * and trailing slashes, with no Android framework dependency.
     */
    static String baseName(String path) {
        if (path == null || path.isEmpty()) {
            return path == null ? "" : path;
        }
        int end = path.length();
        while (end > 0 && (path.charAt(end - 1) == '/' || path.charAt(end - 1) == '\\')) {
            end--;
        }
        int start = end;
        while (start > 0) {
            char c = path.charAt(start - 1);
            if (c == '/' || c == '\\') {
                break;
            }
            start--;
        }
        String base = path.substring(start, end);
        return base.isEmpty() ? path : base;
    }

    private View buildSessionRow(SessionItem session, String projectPath,
                                 BiConsumer<String, String> onSessionClick) {
        LinearLayout row = new LinearLayout(getContext());
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.CENTER_VERTICAL);

        // Tri-color status dot: yellow (pending) > green (running, spinning
        // ring-arc) > blue (unread), else none.
        StatusDotKind kind = statusDotKind(session);
        if (kind != StatusDotKind.NONE) {
            View dot = new View(getContext());
            if (kind == StatusDotKind.RUNNING) {
                // Running: the same ring-arc spinner as the capsule/header —
                // a faint full ring plus a short foreground arc that rotates.
                dot.setBackground(new ArcProgressDrawable(COLOR_RUNNING, dp(DOT_SIZE_DP)));
                spinningDots.add(dot);
            } else {
                GradientDrawable dotDrawable = new GradientDrawable();
                dotDrawable.setShape(GradientDrawable.OVAL);
                dotDrawable.setColor(colorFor(kind));
                dot.setBackground(dotDrawable);
            }
            LinearLayout.LayoutParams dotLp = new LinearLayout.LayoutParams(
                    dp(DOT_SIZE_DP), dp(DOT_SIZE_DP));
            dotLp.setMargins(0, 0, dp(DOT_MARGIN_END_DP), 0);
            row.addView(dot, dotLp);
        }

        TextView title = new TextView(getContext());
        title.setText(session.title == null || session.title.isEmpty()
                ? UserLanguage.resolve(getContext(), R.string.session_untitled) : session.title);
        title.setTextSize(SESSION_TITLE_SIZE_SP);
        title.setTextColor(colorTextPrimary);
        title.setSingleLine(true);
        title.setMaxLines(1);
        title.setEllipsize(android.text.TextUtils.TruncateAt.END);
        title.setIncludeFontPadding(false);
        row.addView(title, new LinearLayout.LayoutParams(
                0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));

        row.setClickable(true);
        row.setOnClickListener(v -> {
            if (onSessionClick != null) {
                onSessionClick.accept(session.id, projectPath);
            }
        });

        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        lp.topMargin = dp(SESSION_ROW_PADDING_TOP_DP);
        row.setLayoutParams(lp);
        return row;
    }

    private int colorFor(StatusDotKind kind) {
        switch (kind) {
            case PENDING:
                return COLOR_PERMISSION_PENDING;
            case UNREAD:
                return COLOR_UNREAD;
            case RUNNING:
            default:
                return COLOR_RUNNING;
        }
    }

    /**
     * Start the rotation loop on every running session's ring-arc. Each dot
     * animates independently so one session finishing does not stall the others;
     * the animators are cancelled in stopBreathing() (called at the top of the
     * next render and on teardown). A linear interpolator keeps the rotation
     * constant-speed — the default AccelerateDecelerate reads as a hiccup per lap.
     */
    private void startBreathing() {
        for (View dot : spinningDots) {
            ObjectAnimator anim = ObjectAnimator.ofFloat(dot, "rotation", 0f, 360f);
            anim.setDuration(SPIN_MS);
            anim.setRepeatCount(ObjectAnimator.INFINITE);
            anim.setInterpolator(new android.view.animation.LinearInterpolator());
            anim.start();
            dot.setTag(anim);
        }
    }

    /**
     * Stop all running-dot spin animations and reset rotation.
     * Covers the title bar's shared content row (which owns its own spin
     * animation) and the session rows. Called before every list rebuild so
     * stale rows never keep animating, and by the controller on teardown so
     * infinite animators cannot keep posting frame callbacks after the window
     * is removed. UI thread only.
     */
    public void stopBreathing() {
        cancelSkeletonBreath();
        headerContentView.stopBreathing();
        for (View dot : spinningDots) {
            Object tag = dot.getTag();
            if (tag instanceof ObjectAnimator) {
                ((ObjectAnimator) tag).cancel();
            }
            dot.setRotation(0f);
        }
        spinningDots.clear();
    }

    /**
     * Linear RGB mix of two opaque colors at the given fraction of {@code b}.
     * Alpha is taken from {@code a}. Pure: no framework deps.
     */
    static int mixArgb(int a, int b, float frac) {
        int r = (a >> 16) & 0xFF, g = (a >> 8) & 0xFF, bl = a & 0xFF;
        int r2 = (b >> 16) & 0xFF, g2 = (b >> 8) & 0xFF, bl2 = b & 0xFF;
        int nr = Math.round(r + (r2 - r) * frac);
        int ng = Math.round(g + (g2 - g) * frac);
        int nb = Math.round(bl + (bl2 - bl) * frac);
        return (a & 0xFF000000) | ((nr & 0xFF) << 16) | ((ng & 0xFF) << 8) | (nb & 0xFF);
    }

    private int dp(int value) {
        return Math.round(value * density);
    }
}
