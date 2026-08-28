package com.clawbench.app;

import android.animation.ObjectAnimator;
import android.content.Context;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

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
 * and a collapse ("×") button, followed by per-project group headers and
 * session rows. Each session row shows a tri-color status indicator, a
 * single-line ellipsized title, and a red circular unread badge when
 * unreadCount > 0. Tapping a row invokes the onSessionClick callback.
 *
 * Status dots follow a fixed priority (yellow > green > blue):
 *   - PENDING  (yellow): pendingApproval, regardless of running/unread
 *   - RUNNING  (green):  running && !pendingApproval — the dot breathes
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
    private static final int COLOR_UNREAD_BADGE = 0xFFE53935; // red
    private static final int COLOR_UNREAD_BADGE_TEXT = 0xFFFFFFFF;

    // Layout constants.
    private static final int PANEL_WIDTH_DP = 280;
    private static final int CORNER_RADIUS_DP = 20;
    private static final int PADDING_H_DP = 14;
    private static final int PADDING_V_DP = 10;
    private static final int COLLAPSE_BTN_SIZE_DP = 22;
    private static final int COLLAPSE_BTN_TEXT_SIZE_SP = 16;
    private static final int PROJECT_HEADER_SIZE_SP = 11;
    private static final int PROJECT_HEADER_PADDING_TOP_DP = 10;
    private static final int SESSION_ROW_PADDING_TOP_DP = 10;
    private static final int SESSION_TITLE_SIZE_SP = 13;
    private static final int DOT_SIZE_DP = 8;
    private static final int DOT_MARGIN_END_DP = 8;
    private static final int BADGE_SIZE_DP = 18;
    private static final int BADGE_TEXT_SIZE_SP = 10;
    private static final int BADGE_MARGIN_START_DP = 6;

    // Breathing animation for a running session's green dot (same rhythm as
    // the capsule's running dot in FloatingStatusView).
    private static final float BREATH_ALPHA_MIN = 0.3f;
    private static final float BREATH_ALPHA_MAX = 1.0f;
    private static final long BREATH_MS = 800;

    /**
     * Status-dot kind for a session row. Pure: no framework deps.
     *
     * Priority is yellow > green > blue (pending approval needs user action,
     * then running activity, then unread content).
     */
    public enum StatusDotKind {
        /** Pending approval (yellow) — wins over running and unread. */
        PENDING,
        /** Running without pending approval (green, breathing). */
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
    private Runnable onCollapseClick;
    /** Views currently breathing; stopped when rows are rebuilt. */
    private final List<View> breathingDots = new ArrayList<>();

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
        // and their breathing animation are stable while the session list
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

        TextView collapseBtn = new TextView(context);
        collapseBtn.setText("×");
        collapseBtn.setTextSize(COLLAPSE_BTN_TEXT_SIZE_SP);
        collapseBtn.setTextColor(colorTextSecondary);
        collapseBtn.setGravity(Gravity.CENTER);
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
     * stale rows. Running-dot breathing is restarted for the new rows (and
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
        renderHeaderStats(runningCount, pendingCount, unreadCount);

        stopBreathing();
        listContainer.removeAllViews();
        breathingDots.clear();
        for (ProjectGroup group : groups) {
            listContainer.addView(buildProjectHeader(group.name));
            for (SessionItem session : group.sessions) {
                listContainer.addView(buildSessionRow(session, group.name, onSessionClick));
            }
        }
        startBreathing();
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
        TextView header = new TextView(getContext());
        header.setText(name);
        header.setTextSize(PROJECT_HEADER_SIZE_SP);
        header.setTextColor(colorTextSecondary);
        header.setTypeface(Typeface.DEFAULT_BOLD);
        header.setIncludeFontPadding(false);
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        lp.topMargin = dp(PROJECT_HEADER_PADDING_TOP_DP);
        header.setLayoutParams(lp);
        return header;
    }

    private View buildSessionRow(SessionItem session, String projectPath,
                                 BiConsumer<String, String> onSessionClick) {
        LinearLayout row = new LinearLayout(getContext());
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.CENTER_VERTICAL);

        // Tri-color status dot: yellow (pending) > green (running, breathing)
        // > blue (unread), else none.
        StatusDotKind kind = statusDotKind(session);
        if (kind != StatusDotKind.NONE) {
            View dot = new View(getContext());
            GradientDrawable dotDrawable = new GradientDrawable();
            dotDrawable.setShape(GradientDrawable.OVAL);
            dotDrawable.setColor(colorFor(kind));
            dot.setBackground(dotDrawable);
            if (kind == StatusDotKind.RUNNING) {
                breathingDots.add(dot);
            }
            LinearLayout.LayoutParams dotLp = new LinearLayout.LayoutParams(
                    dp(DOT_SIZE_DP), dp(DOT_SIZE_DP));
            dotLp.setMargins(0, 0, dp(DOT_MARGIN_END_DP), 0);
            row.addView(dot, dotLp);
        }

        TextView title = new TextView(getContext());
        title.setText(session.title == null || session.title.isEmpty() ? "(无标题)" : session.title);
        title.setTextSize(SESSION_TITLE_SIZE_SP);
        title.setTextColor(colorTextPrimary);
        title.setSingleLine(true);
        title.setMaxLines(1);
        title.setEllipsize(android.text.TextUtils.TruncateAt.END);
        title.setIncludeFontPadding(false);
        row.addView(title, new LinearLayout.LayoutParams(
                0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));

        // Unread badge: red circle with the unread count when > 0. Kept even
        // when a blue dot already signals unread — the badge shows the count.
        if (session.unreadCount > 0) {
            TextView badge = new TextView(getContext());
            badge.setText(String.valueOf(session.unreadCount));
            badge.setTextSize(BADGE_TEXT_SIZE_SP);
            badge.setTextColor(COLOR_UNREAD_BADGE_TEXT);
            badge.setGravity(Gravity.CENTER);
            GradientDrawable badgeBg = new GradientDrawable();
            badgeBg.setShape(GradientDrawable.OVAL);
            badgeBg.setColor(COLOR_UNREAD_BADGE);
            badge.setBackground(badgeBg);
            LinearLayout.LayoutParams badgeLp = new LinearLayout.LayoutParams(
                    dp(BADGE_SIZE_DP), dp(BADGE_SIZE_DP));
            badgeLp.setMargins(dp(BADGE_MARGIN_START_DP), 0, 0, 0);
            row.addView(badge, badgeLp);
        }

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
     * Start the breathing alpha loop on every running session's dot. Each dot
     * animates independently so one session finishing does not stall the others;
     * the animators are cancelled in stopBreathing() (called at the top of the
     * next render and on teardown).
     */
    private void startBreathing() {
        for (View dot : breathingDots) {
            ObjectAnimator anim = ObjectAnimator.ofFloat(dot, "alpha",
                    BREATH_ALPHA_MIN, BREATH_ALPHA_MAX);
            anim.setDuration(BREATH_MS);
            anim.setRepeatCount(ObjectAnimator.INFINITE);
            anim.setRepeatMode(ObjectAnimator.REVERSE);
            anim.start();
            dot.setTag(anim);
        }
    }

    /**
     * Stop all running-dot breathing animations and restore full opacity.
     * Covers the title bar's shared content row (which owns its own breathing
     * animation) and the session rows. Called before every list rebuild so
     * stale rows never keep animating, and by the controller on teardown so
     * infinite animators cannot keep posting frame callbacks after the window
     * is removed. UI thread only.
     */
    public void stopBreathing() {
        headerContentView.stopBreathing();
        for (View dot : breathingDots) {
            Object tag = dot.getTag();
            if (tag instanceof ObjectAnimator) {
                ((ObjectAnimator) tag).cancel();
            }
            dot.setAlpha(BREATH_ALPHA_MAX);
        }
        breathingDots.clear();
    }

    private int dp(int value) {
        return Math.round(value * density);
    }
}
