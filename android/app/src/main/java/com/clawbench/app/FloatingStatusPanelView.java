package com.clawbench.app;

import android.content.Context;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.view.Gravity;
import android.view.View;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;

/**
 * Grouped session-list panel for the desktop floating status window.
 *
 * Renders the /api/ai/sessions/overview response as a scrollable list grouped
 * by project. UI is built in code (no XML): a header row with a live-session
 * count and a collapse ("×") button, followed by per-project group headers and
 * session rows. Each session row shows a status indicator (green running dot,
 * yellow pending-approval dot, or nothing), a single-line ellipsized title,
 * and a red circular unread badge when unreadCount > 0. Tapping a row invokes
 * the onSessionClick callback.
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

    // github-dark palette.
    private static final int COLOR_BG = 0xF0161B22; // translucent dark background
    private static final int COLOR_BORDER = 0xFF30363D;
    private static final int COLOR_TEXT_PRIMARY = 0xFFE6EDF3;
    private static final int COLOR_TEXT_SECONDARY = 0xFF9DA7B3;
    private static final int COLOR_UNREAD_BADGE = 0xFFE53935; // red
    private static final int COLOR_UNREAD_BADGE_TEXT = 0xFFFFFFFF;

    // Layout constants.
    private static final int PANEL_WIDTH_DP = 280;
    private static final int MAX_HEIGHT_DP = 400;
    private static final int CORNER_RADIUS_DP = 18;
    private static final int PADDING_H_DP = 14;
    private static final int PADDING_V_DP = 10;
    private static final int HEADER_TITLE_SIZE_SP = 14;
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

    private final float density;
    private final TextView headerTitleView;
    private final LinearLayout listContainer;
    private Runnable onCollapseClick;

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

    public FloatingStatusPanelView(Context context, Consumer<String> onSessionClick) {
        super(context);
        density = getResources().getDisplayMetrics().density;

        // Background: rounded dark translucent panel with a thin border.
        GradientDrawable bg = new GradientDrawable();
        bg.setColor(COLOR_BG);
        bg.setCornerRadius(dp(CORNER_RADIUS_DP));
        bg.setStroke(dp(1), COLOR_BORDER);
        setBackground(bg);
        setPadding(dp(PADDING_H_DP), dp(PADDING_V_DP), dp(PADDING_H_DP), dp(PADDING_V_DP));

        LinearLayout root = new LinearLayout(context);
        root.setOrientation(LinearLayout.VERTICAL);

        // Header: live-session count + collapse button.
        LinearLayout header = new LinearLayout(context);
        header.setOrientation(LinearLayout.HORIZONTAL);
        header.setGravity(Gravity.CENTER_VERTICAL);
        headerTitleView = new TextView(context);
        headerTitleView.setTextSize(HEADER_TITLE_SIZE_SP);
        headerTitleView.setTextColor(COLOR_TEXT_PRIMARY);
        headerTitleView.setTypeface(Typeface.DEFAULT_BOLD);
        headerTitleView.setIncludeFontPadding(false);
        header.addView(headerTitleView, new LinearLayout.LayoutParams(
                0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));

        TextView collapseBtn = new TextView(context);
        collapseBtn.setText("×");
        collapseBtn.setTextSize(COLLAPSE_BTN_TEXT_SIZE_SP);
        collapseBtn.setTextColor(COLOR_TEXT_SECONDARY);
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

        // Scrollable session list.
        ScrollView scrollView = new ScrollView(context);
        scrollView.setVerticalScrollBarEnabled(false);
        listContainer = new LinearLayout(context);
        listContainer.setOrientation(LinearLayout.VERTICAL);
        scrollView.addView(listContainer, new ScrollView.LayoutParams(
                ScrollView.LayoutParams.MATCH_PARENT, ScrollView.LayoutParams.WRAP_CONTENT));
        root.addView(scrollView, new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));

        addView(root, new LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT));

        // Panel size: ~280dp wide, max ~400dp tall (scrolls beyond that).
        LayoutParams selfLp = (LayoutParams) getLayoutParams();
        if (selfLp == null) {
            selfLp = new LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT);
        }
        selfLp.width = dp(PANEL_WIDTH_DP);
        selfLp.height = dp(MAX_HEIGHT_DP);
        setLayoutParams(selfLp);
    }

    public void setOnCollapseClickListener(Runnable onCollapseClick) {
        this.onCollapseClick = onCollapseClick;
    }

    /**
     * Rebuild the panel content from an overview JSON object. Safe to call on
     * the UI thread; replaces the entire list so refreshes never accumulate
     * stale rows.
     */
    public void render(JSONObject overview, Consumer<String> onSessionClick) {
        List<ProjectGroup> groups = buildGroups(overview);
        int runningCount = 0;
        for (ProjectGroup g : groups) {
            for (SessionItem s : g.sessions) {
                if (s.running) {
                    runningCount++;
                }
            }
        }
        AppLog.d("FloatingPanelView", "render groups=" + groups.size()
                + " running=" + runningCount);

        headerTitleView.setText(runningCount > 0
                ? runningCount + " 个会话运行中" : "会话列表");

        listContainer.removeAllViews();
        for (ProjectGroup group : groups) {
            listContainer.addView(buildProjectHeader(group.name));
            for (SessionItem session : group.sessions) {
                listContainer.addView(buildSessionRow(session, onSessionClick));
            }
        }
    }

    private View buildProjectHeader(String name) {
        TextView header = new TextView(getContext());
        header.setText(name);
        header.setTextSize(PROJECT_HEADER_SIZE_SP);
        header.setTextColor(COLOR_TEXT_SECONDARY);
        header.setTypeface(Typeface.DEFAULT_BOLD);
        header.setIncludeFontPadding(false);
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        lp.topMargin = dp(PROJECT_HEADER_PADDING_TOP_DP);
        header.setLayoutParams(lp);
        return header;
    }

    private View buildSessionRow(SessionItem session, Consumer<String> onSessionClick) {
        LinearLayout row = new LinearLayout(getContext());
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.CENTER_VERTICAL);

        // Status indicator: green dot (running), yellow dot (pending approval), none otherwise.
        if (session.running || session.pendingApproval) {
            View dot = new View(getContext());
            GradientDrawable dotDrawable = new GradientDrawable();
            dotDrawable.setShape(GradientDrawable.OVAL);
            dotDrawable.setColor(session.running
                    ? COLOR_RUNNING : COLOR_PERMISSION_PENDING);
            dot.setBackground(dotDrawable);
            LinearLayout.LayoutParams dotLp = new LinearLayout.LayoutParams(
                    dp(DOT_SIZE_DP), dp(DOT_SIZE_DP));
            dotLp.setMargins(0, 0, dp(DOT_MARGIN_END_DP), 0);
            row.addView(dot, dotLp);
        }

        TextView title = new TextView(getContext());
        title.setText(session.title == null || session.title.isEmpty() ? "(无标题)" : session.title);
        title.setTextSize(SESSION_TITLE_SIZE_SP);
        title.setTextColor(COLOR_TEXT_PRIMARY);
        title.setSingleLine(true);
        title.setMaxLines(1);
        title.setEllipsize(android.text.TextUtils.TruncateAt.END);
        title.setIncludeFontPadding(false);
        row.addView(title, new LinearLayout.LayoutParams(
                0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));

        // Unread badge: red circle with the unread count when > 0.
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
                onSessionClick.accept(session.id);
            }
        });

        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        lp.topMargin = dp(SESSION_ROW_PADDING_TOP_DP);
        row.setLayoutParams(lp);
        return row;
    }

    private int dp(int value) {
        return Math.round(value * density);
    }
}
