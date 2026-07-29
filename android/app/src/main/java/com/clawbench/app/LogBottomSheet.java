package com.clawbench.app;

import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.Editable;
import android.text.TextWatcher;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.EditText;
import android.widget.ImageView;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.core.content.ContextCompat;
import androidx.recyclerview.widget.LinearLayoutManager;
import androidx.recyclerview.widget.RecyclerView;

import com.google.android.material.bottomsheet.BottomSheetDialogFragment;

import java.net.HttpURLConnection;
import java.net.URL;
import java.net.URLEncoder;
import java.text.SimpleDateFormat;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Date;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Set;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

public class LogBottomSheet extends BottomSheetDialogFragment {

    private static final SimpleDateFormat TIME_FORMAT = new SimpleDateFormat("HH:mm:ss", Locale.ROOT);
    private static final SimpleDateFormat TIME_FORMAT_MILLIS = new SimpleDateFormat("HH:mm:ss.SSS", Locale.ROOT);
    private static final int MAX_SEND_LENGTH = 10000;

    private RecyclerView recyclerView;
    private TextView emptyView;
    private EditText messageInput;
    private LogAdapter adapter;
    private final Handler uiHandler = new Handler(Looper.getMainLooper());
    private final ExecutorService networkExecutor = Executors.newSingleThreadExecutor();

    private char currentFilter = '\0'; // '\0' = all
    private String currentSessionId;
    private String serverUrl;
    private String allCookies;

    // Selected log entry sequence numbers (unique identity, unlike timestamps which can collide)
    private final Set<Long> selectedSeqs = new HashSet<>();

    private static final int COLOR_SELECTION = 0x1F58a6ff; // ~12% alpha accent blue for selection
    private Runnable refreshRunnable;
    private static final long REFRESH_INTERVAL_MS = 500;
    private Toast loadingToast;

    public static LogBottomSheet newInstance() {
        return new LogBottomSheet();
    }

    @Override
    public void onCreate(@Nullable Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        // Get credentials from BrowserActivity's in-memory store.
        // BrowserActivity already called getAndClear() in onCreate() to minimize
        // SharedPreferences exposure window, so we must not read from prefs again.
        if (getActivity() instanceof BrowserActivity) {
            BrowserSessionCredentials.Creds creds = ((BrowserActivity) getActivity()).getSessionCredentials();
            if (creds != null) {
                currentSessionId = creds.sessionId;
                serverUrl = creds.serverUrl;
                allCookies = creds.allCookies;
            }
        }
    }

    @Nullable
    @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container, @Nullable Bundle savedInstanceState) {
        return inflater.inflate(R.layout.bottom_sheet_log, container, false);
    }

    @NonNull
    @Override
    public android.app.Dialog onCreateDialog(@Nullable Bundle savedInstanceState) {
        android.app.Dialog dialog = super.onCreateDialog(savedInstanceState);
        // Ensure keyboard pushes up the bottom sheet instead of covering the input
        if (dialog.getWindow() != null) {
            dialog.getWindow().setSoftInputMode(android.view.WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE);
        }
        return dialog;
    }

    @Override
    public void onViewCreated(@NonNull View view, @Nullable Bundle savedInstanceState) {
        super.onViewCreated(view, savedInstanceState);

        recyclerView = view.findViewById(R.id.logRecyclerView);
        emptyView = view.findViewById(R.id.logEmptyView);
        messageInput = view.findViewById(R.id.messageInput);

        adapter = new LogAdapter();
        LinearLayoutManager layoutManager = new LinearLayoutManager(requireContext());
        recyclerView.setLayoutManager(layoutManager);
        recyclerView.setAdapter(adapter);

        // Clear input button
        ImageView btnClearInput = view.findViewById(R.id.btnClearInput);
        btnClearInput.setOnClickListener(v -> messageInput.setText(""));
        messageInput.addTextChangedListener(new TextWatcher() {
            @Override public void beforeTextChanged(CharSequence s, int start, int count, int after) {}
            @Override public void onTextChanged(CharSequence s, int start, int before, int count) {}
            @Override public void afterTextChanged(Editable s) {
                btnClearInput.setVisibility(s.length() > 0 ? View.VISIBLE : View.GONE);
            }
        });

        // Filter chips with visual selection
        TextView filterAll = view.findViewById(R.id.filterAll);
        TextView filterError = view.findViewById(R.id.filterError);
        TextView filterWarn = view.findViewById(R.id.filterWarn);
        TextView filterLog = view.findViewById(R.id.filterLog);

        View.OnClickListener filterClick = v -> {
            if (v.getId() == R.id.filterAll) currentFilter = '\0';
            else if (v.getId() == R.id.filterError) currentFilter = 'E';
            else if (v.getId() == R.id.filterWarn) currentFilter = 'W';
            else if (v.getId() == R.id.filterLog) currentFilter = 'D';
            selectedSeqs.clear(); // Clear selections when filter changes to avoid stale phantom selections
            updateFilterChips(filterAll, filterError, filterWarn, filterLog);
            refreshList();
        };
        filterAll.setOnClickListener(filterClick);
        filterError.setOnClickListener(filterClick);
        filterWarn.setOnClickListener(filterClick);
        filterLog.setOnClickListener(filterClick);
        // Set initial selection
        updateFilterChips(filterAll, filterError, filterWarn, filterLog);

        // Clear
        view.findViewById(R.id.btnClearLog).setOnClickListener(v -> {
            getLogBuffer().clear();
            selectedSeqs.clear();
            refreshList();
        });

        // Send selected
        view.findViewById(R.id.btnSendSelected).setOnClickListener(v -> {
            List<BrowserLogBuffer.Entry> selected = getSelectedEntries();
            if (selected.isEmpty()) {
                Toast.makeText(requireContext(), R.string.browser_log_no_selection, Toast.LENGTH_SHORT).show();
                return;
            }
            String desc = messageInput.getText().toString().trim();
            String content = formatEntriesWithDescription(desc, selected);
            showSessionPicker(content);
        });

        // Send all
        view.findViewById(R.id.btnSendAll).setOnClickListener(v -> {
            List<BrowserLogBuffer.Entry> all = getFilteredEntries();
            if (all.isEmpty()) return;
            String desc = messageInput.getText().toString().trim();
            String content = formatEntriesWithDescription(desc, all);
            showSessionPicker(content);
        });

        refreshList();

        // Auto-refresh every 500ms
        refreshRunnable = () -> {
            refreshList();
            uiHandler.postDelayed(refreshRunnable, REFRESH_INTERVAL_MS);
        };
        uiHandler.postDelayed(refreshRunnable, REFRESH_INTERVAL_MS);
    }

    @Override
    public void onStart() {
        super.onStart();
        // Expand bottom sheet to full screen
        if (getDialog() != null) {
            com.google.android.material.bottomsheet.BottomSheetDialog dialog =
                    (com.google.android.material.bottomsheet.BottomSheetDialog) getDialog();
            android.view.View bottomSheet = dialog.findViewById(com.google.android.material.R.id.design_bottom_sheet);
            if (bottomSheet != null) {
                com.google.android.material.bottomsheet.BottomSheetBehavior<?> behavior =
                        com.google.android.material.bottomsheet.BottomSheetBehavior.from(bottomSheet);
                behavior.setState(com.google.android.material.bottomsheet.BottomSheetBehavior.STATE_EXPANDED);
                behavior.setSkipCollapsed(true);
                // Make bottom sheet full screen: expanded offset = 0 means top aligns with parent top
                behavior.setExpandedOffset(0);
            }
        }
    }

    @Override
    public void onDestroyView() {
        super.onDestroyView();
        if (refreshRunnable != null) {
            uiHandler.removeCallbacks(refreshRunnable);
        }
        if (loadingToast != null) {
            loadingToast.cancel();
            loadingToast = null;
        }
        // Use shutdown() instead of shutdownNow() to allow in-flight HTTP requests to complete
        networkExecutor.shutdown();
    }

    private void updateFilterChips(TextView all, TextView error, TextView warn, TextView log) {
        all.setActivated(currentFilter == '\0');
        error.setActivated(currentFilter == 'E');
        warn.setActivated(currentFilter == 'W');
        log.setActivated(currentFilter == 'D');
    }

    private BrowserLogBuffer getLogBuffer() {
        if (getActivity() instanceof BrowserActivity) {
            return ((BrowserActivity) getActivity()).getLogBuffer();
        }
        // Should not happen — LogBottomSheet is only used within BrowserActivity
        throw new IllegalStateException("LogBottomSheet requires BrowserActivity host");
    }

    private List<BrowserLogBuffer.Entry> getFilteredEntries() {
        List<BrowserLogBuffer.Entry> all = getLogBuffer().getEntries();
        List<BrowserLogBuffer.Entry> filtered = new ArrayList<>();
        for (BrowserLogBuffer.Entry e : all) {
            if (currentFilter != '\0' && e.level != currentFilter) continue;
            filtered.add(e);
        }
        return filtered;
    }

    private List<BrowserLogBuffer.Entry> getSelectedEntries() {
        List<BrowserLogBuffer.Entry> filtered = getFilteredEntries();
        List<BrowserLogBuffer.Entry> selected = new ArrayList<>();
        for (BrowserLogBuffer.Entry e : filtered) {
            if (selectedSeqs.contains(e.seq)) selected.add(e);
        }
        return selected;
    }

    private String formatEntries(List<BrowserLogBuffer.Entry> entries) {
        StringBuilder sb = new StringBuilder();
        sb.append("```log\n");
        for (BrowserLogBuffer.Entry e : entries) {
            sb.append(String.format(Locale.ROOT, "%s %c/%s: %s\n",
                    TIME_FORMAT_MILLIS.format(new Date(e.ts)), e.level, e.tag, e.msg));
        }
        sb.append("```");
        // Truncate if exceeding max length
        if (sb.length() > MAX_SEND_LENGTH) {
            sb.setLength(MAX_SEND_LENGTH);
            sb.append("\n... (truncated)");
        }
        return sb.toString();
    }

    private String formatEntriesWithDescription(String description, List<BrowserLogBuffer.Entry> entries) {
        StringBuilder sb = new StringBuilder();
        if (!description.isEmpty()) {
            sb.append(description).append("\n\n");
        }
        sb.append(formatEntries(entries));
        return sb.toString();
    }

    /**
     * Fetch session list from server and show a picker dialog.
     * Default selection is the current session.
     */
    private void showSessionPicker(String content) {
        if (serverUrl == null || serverUrl.isEmpty() || allCookies.isEmpty()) {
            AppLog.w("LogBottomSheet", "No session credentials: url=" + serverUrl);
            Toast.makeText(requireContext(), R.string.browser_log_no_session, Toast.LENGTH_SHORT).show();
            return;
        }

        // Show loading toast while fetching
        if (loadingToast != null) loadingToast.cancel();
        loadingToast = Toast.makeText(requireContext(), R.string.browser_log_loading_sessions, Toast.LENGTH_SHORT);
        loadingToast.show();

        try {
        networkExecutor.execute(() -> {
            try {
                String url = serverUrl + "/api/ai/sessions?limit=50";
                URL urlObj = new URL(url);
                HttpURLConnection conn = (HttpURLConnection) urlObj.openConnection();
                try {
                    conn.setRequestMethod("GET");
                    conn.setRequestProperty("Cookie", allCookies);
                    conn.setConnectTimeout(5000);
                    conn.setReadTimeout(5000);

                    // Only trust self-signed certs for localhost
                    if (conn instanceof javax.net.ssl.HttpsURLConnection) {
                        String host = urlObj.getHost();
                        SSLHelper.setupTrustAll((javax.net.ssl.HttpsURLConnection) conn, host);
                    }

                    int code = conn.getResponseCode();
                    if (code != 200) {
                        AppLog.w("LogBottomSheet", "Fetch sessions failed: HTTP " + code);
                        uiHandler.post(() -> {
                            if (!isAdded()) return;
                            if (loadingToast != null) loadingToast.cancel();
                            Toast.makeText(requireContext(), R.string.browser_log_send_failed, Toast.LENGTH_SHORT).show();
                        });
                        return;
                    }

                    // Read response
                    java.io.BufferedReader reader = new java.io.BufferedReader(
                            new java.io.InputStreamReader(conn.getInputStream(), "UTF-8"));
                    StringBuilder sb = new StringBuilder();
                    String line;
                    while ((line = reader.readLine()) != null) {
                        sb.append(line);
                    }

                    org.json.JSONObject json = new org.json.JSONObject(sb.toString());
                    org.json.JSONArray arr = json.optJSONArray("sessions");
                    if (arr == null || arr.length() == 0) {
                        uiHandler.post(() -> {
                            if (!isAdded()) return;
                            Toast.makeText(requireContext(), R.string.browser_log_no_session, Toast.LENGTH_SHORT).show();
                        });
                        return;
                    }

                    // Parse sessions
                    List<String> ids = new ArrayList<>();
                    List<String> titles = new ArrayList<>();
                    List<Boolean> running = new ArrayList<>();
                    int defaultIndex = 0;
                    for (int i = 0; i < arr.length(); i++) {
                        org.json.JSONObject s = arr.getJSONObject(i);
                        String id = s.optString("id", "");
                        String title = s.optString("title", "");
                        boolean isRunning = s.optBoolean("running", false);
                        if (id.isEmpty()) continue;

                        ids.add(id);
                        titles.add(title.isEmpty() ? id : title);
                        running.add(isRunning);

                        if (id.equals(currentSessionId)) {
                            defaultIndex = ids.size() - 1;
                        }
                    }

                    if (ids.isEmpty()) {
                        uiHandler.post(() -> {
                            if (!isAdded()) return;
                            Toast.makeText(requireContext(), R.string.browser_log_no_session, Toast.LENGTH_SHORT).show();
                        });
                        return;
                    }

                    int finalDefaultIndex = defaultIndex;
                    List<String> finalIds = ids;
                    List<String> finalTitles = titles;
                    List<Boolean> finalRunning = running;

                    uiHandler.post(() -> {
                        if (!isAdded()) return;
                        if (loadingToast != null) loadingToast.cancel();
                        showSessionPickerDialog(finalIds, finalTitles, finalRunning, finalDefaultIndex, content);
                    });

                } finally {
                    conn.disconnect();
                }
            } catch (Exception e) {
                AppLog.e("LogBottomSheet", "Failed to fetch sessions", e);
                uiHandler.post(() -> {
                    if (!isAdded()) return;
                    Toast.makeText(requireContext(), R.string.browser_log_send_failed, Toast.LENGTH_SHORT).show();
                });
            }
        });
        } catch (java.util.concurrent.RejectedExecutionException e) {
            AppLog.w("LogBottomSheet", "Executor shut down, send cancelled");
        }
    }

    /**
     * Show custom dark-themed session picker dialog with RecyclerView.
     */
    private void showSessionPickerDialog(List<String> ids, List<String> titles, List<Boolean> running, int defaultIndex, String content) {
        Context ctx = getContext();
        if (ctx == null) return;

        // Use AlertDialog with custom view for reliable dark theme
        android.app.AlertDialog.Builder builder = new android.app.AlertDialog.Builder(ctx, R.style.Theme_ClawBench_Browser);
        View dialogView = LayoutInflater.from(ctx).inflate(R.layout.dialog_session_picker, null);
        builder.setView(dialogView);

        android.app.AlertDialog dialog = builder.create();
        // Set a dim background behind the dialog to separate it from the BottomSheet
        if (dialog.getWindow() != null) {
            dialog.getWindow().setBackgroundDrawable(new android.graphics.drawable.ColorDrawable(0xCC000000));
        }

        int[] selectedIdx = {defaultIndex};

        RecyclerView sessionList = dialogView.findViewById(R.id.sessionList);
        sessionList.setLayoutManager(new LinearLayoutManager(ctx));
        sessionList.setAdapter(new SessionAdapter(ids, titles, running, defaultIndex, idx -> selectedIdx[0] = idx));

        // Constrain RecyclerView height to prevent dialog from growing too tall
        int maxListHeight = (int) (ctx.getResources().getDisplayMetrics().heightPixels * 0.4);
        sessionList.post(() -> {
            if (sessionList.getHeight() > maxListHeight) {
                ViewGroup.LayoutParams clp = sessionList.getLayoutParams();
                clp.height = maxListHeight;
                sessionList.setLayoutParams(clp);
            }
        });

        // Cancel button
        dialogView.findViewById(R.id.btnCancel).setOnClickListener(v -> dialog.dismiss());

        // Confirm button with loading state
        View btnConfirm = dialogView.findViewById(R.id.btnConfirm);
        btnConfirm.setOnClickListener(v -> {
            // Disable button to prevent double-send
            btnConfirm.setEnabled(false);
            btnConfirm.setAlpha(0.5f);
            String targetId = ids.get(selectedIdx[0]);
            sendToSession(targetId, content, () -> {
                // Re-enable on failure (success dismisses the dialog)
                if (dialog.isShowing()) {
                    btnConfirm.setEnabled(true);
                    btnConfirm.setAlpha(1f);
                }
            });
            messageInput.setText("");
            // Dismiss dialog after initiating send (async, can't wait for result)
            dialog.dismiss();
        });

        dialog.show();

        // Set width after show() so window is available
        if (dialog.getWindow() != null) {
            dialog.getWindow().setLayout(
                    (int) (ctx.getResources().getDisplayMetrics().widthPixels * 0.88),
                    android.view.ViewGroup.LayoutParams.WRAP_CONTENT);
        }
    }

    private void sendToSession(String targetSessionId, String content, Runnable onFailure) {
        try {
        networkExecutor.execute(() -> {
            try {
                String url = serverUrl + "/api/ai/chat?session_id=" + URLEncoder.encode(targetSessionId, "UTF-8");
                org.json.JSONObject body = new org.json.JSONObject();
                body.put("message", content);
                byte[] data = body.toString().getBytes("UTF-8");

                URL urlObj = new URL(url);
                HttpURLConnection conn = (HttpURLConnection) urlObj.openConnection();
                try {
                    conn.setRequestMethod("POST");
                    conn.setRequestProperty("Content-Type", "application/json");
                    conn.setRequestProperty("Cookie", allCookies);
                    conn.setConnectTimeout(5000);
                    conn.setReadTimeout(5000);
                    conn.setDoOutput(true);

                    // Only trust self-signed certs for localhost
                    if (conn instanceof javax.net.ssl.HttpsURLConnection) {
                        String host = urlObj.getHost();
                        SSLHelper.setupTrustAll((javax.net.ssl.HttpsURLConnection) conn, host);
                    }

                    java.io.OutputStream os = conn.getOutputStream();
                    os.write(data);
                    os.flush();
                    os.close();

                    int code = conn.getResponseCode();
                    // Drain response body to avoid connection pool leaks
                    try (java.io.InputStream is = code >= 400 ? conn.getErrorStream() : conn.getInputStream()) {
                        if (is != null) { byte[] drain = new byte[1024]; while (is.read(drain) != -1) {} }
                    } catch (java.io.IOException ignored) {}
                    if (code == 200) {
                        uiHandler.post(() -> {
                            if (!isAdded()) return;
                            Toast.makeText(requireContext(), R.string.browser_log_send_success, Toast.LENGTH_SHORT).show();
                            // Hide BrowserActivity (not close) and navigate to the target session
                            if (getActivity() instanceof BrowserActivity) {
                                ((BrowserActivity) getActivity()).navigateBackToMain(targetSessionId);
                            }
                        });
                    } else if (code == 401 || code == 403) {
                        AppLog.w("LogBottomSheet", "Auth expired: HTTP " + code);
                        uiHandler.post(() -> {
                            if (!isAdded()) return;
                            Toast.makeText(requireContext(), R.string.browser_log_auth_expired, Toast.LENGTH_SHORT).show();
                            if (onFailure != null) onFailure.run();
                        });
                    } else {
                        AppLog.w("LogBottomSheet", "Send failed: HTTP " + code);
                        uiHandler.post(() -> {
                            if (!isAdded()) return;
                            Toast.makeText(requireContext(), R.string.browser_log_send_failed, Toast.LENGTH_SHORT).show();
                            if (onFailure != null) onFailure.run();
                        });
                    }
                } finally {
                    conn.disconnect();
                }
            } catch (Exception e) {
                AppLog.e("LogBottomSheet", "Send to session failed", e);
                uiHandler.post(() -> {
                    if (!isAdded()) return;
                    Toast.makeText(requireContext(), R.string.browser_log_send_failed, Toast.LENGTH_SHORT).show();
                    if (onFailure != null) onFailure.run();
                });
            }
        });
        } catch (java.util.concurrent.RejectedExecutionException e) {
            AppLog.w("LogBottomSheet", "Executor shut down, send cancelled");
            if (onFailure != null) onFailure.run();
        }
    }

    private void refreshList() {
        List<BrowserLogBuffer.Entry> filtered = getFilteredEntries();
        int oldCount = adapter.getItemCount();
        adapter.setEntries(filtered);
        recyclerView.setVisibility(filtered.isEmpty() ? View.GONE : View.VISIBLE);
        emptyView.setVisibility(filtered.isEmpty() ? View.VISIBLE : View.GONE);

        // Auto-scroll to bottom when new entries arrive and user is already at the bottom
        if (filtered.size() > oldCount && recyclerView.getLayoutManager() instanceof LinearLayoutManager) {
            LinearLayoutManager lm = (LinearLayoutManager) recyclerView.getLayoutManager();
            int lastVisible = lm.findLastCompletelyVisibleItemPosition();
            if (lastVisible >= oldCount - 1 || oldCount == 0) {
                recyclerView.scrollToPosition(filtered.size() - 1);
            }
        }
    }

    // --- RecyclerView Adapter ---

    private class LogAdapter extends RecyclerView.Adapter<LogAdapter.ViewHolder> {

        private List<BrowserLogBuffer.Entry> entries = Collections.emptyList();

        void setEntries(List<BrowserLogBuffer.Entry> entries) {
            int oldSize = this.entries.size();
            int newSize = entries.size();
            // Skip update if nothing changed (avoids unnecessary rebinds on 500ms auto-refresh)
            // Check both first and last entry seq to catch circular buffer head eviction
            if (oldSize == newSize && !entries.isEmpty() && !this.entries.isEmpty()) {
                if (this.entries.get(0).seq == entries.get(0).seq
                        && this.entries.get(oldSize - 1).seq == entries.get(newSize - 1).seq) return;
            }
            this.entries = entries;
            if (oldSize == 0) {
                notifyDataSetChanged();
            } else if (newSize > oldSize && newSize <= oldSize + 20) {
                // Incremental insert for typical streaming case
                notifyItemRangeInserted(oldSize, newSize - oldSize);
            } else {
                notifyDataSetChanged();
            }
        }

        @NonNull
        @Override
        public ViewHolder onCreateViewHolder(@NonNull ViewGroup parent, int viewType) {
            TextView tv = new TextView(parent.getContext());
            tv.setPadding(8, 3, 8, 3);
            tv.setTextSize(12);
            tv.setTypeface(android.graphics.Typeface.MONOSPACE);
            if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.M) {
                tv.setBreakStrategy(android.text.Layout.BREAK_STRATEGY_SIMPLE);
            }
            RecyclerView.LayoutParams lp = new RecyclerView.LayoutParams(
                    RecyclerView.LayoutParams.MATCH_PARENT, RecyclerView.LayoutParams.WRAP_CONTENT);
            tv.setLayoutParams(lp);
            return new ViewHolder(tv);
        }

        @Override
        public void onBindViewHolder(@NonNull ViewHolder holder, int position) {
            BrowserLogBuffer.Entry entry = entries.get(position);
            String time = TIME_FORMAT.format(new Date(entry.ts));
            String levelLabel;
            switch (entry.level) {
                case 'E': levelLabel = "ERR"; break;
                case 'W': levelLabel = "WRN"; break;
                default:  levelLabel = "LOG"; break;
            }
            String text = time + " " + levelLabel + " " + entry.tag + ": " + entry.msg;

            TextView tv = (TextView) holder.itemView;
            tv.setText(text);

            boolean isSelected = selectedSeqs.contains(entry.seq);
            tv.setBackgroundColor(isSelected ? COLOR_SELECTION : 0x00000000);

            switch (entry.level) {
                case 'E': tv.setTextColor(ContextCompat.getColor(requireContext(), R.color.browser_log_error)); break;
                case 'W': tv.setTextColor(ContextCompat.getColor(requireContext(), R.color.browser_log_warn)); break;
                default:  tv.setTextColor(ContextCompat.getColor(requireContext(), R.color.browser_icon_tint)); break;
            }

            tv.setOnClickListener(v -> {
                if (selectedSeqs.contains(entry.seq)) {
                    selectedSeqs.remove(entry.seq);
                } else {
                    selectedSeqs.add(entry.seq);
                }
                int pos = holder.getAdapterPosition();
                if (pos != RecyclerView.NO_POSITION) {
                    notifyItemChanged(pos);
                }
            });

            tv.setOnLongClickListener(v -> {
                ClipboardManager clipboard = (ClipboardManager) requireContext().getSystemService(Context.CLIPBOARD_SERVICE);
                clipboard.setPrimaryClip(ClipData.newPlainText("log", text));
                Toast.makeText(requireContext(), R.string.browser_log_copied, Toast.LENGTH_SHORT).show();
                return true;
            });
        }

        @Override
        public int getItemCount() {
            return entries.size();
        }

        class ViewHolder extends RecyclerView.ViewHolder {
            ViewHolder(TextView itemView) {
                super(itemView);
            }
        }
    }

    // --- Session Picker Adapter ---

    private class SessionAdapter extends RecyclerView.Adapter<SessionAdapter.ViewHolder> {

        private final List<String> ids;
        private final List<String> titles;
        private final List<Boolean> running;
        private int selectedPos;
        private final java.util.function.IntConsumer onSelect;

        SessionAdapter(List<String> ids, List<String> titles, List<Boolean> running, int defaultPos, java.util.function.IntConsumer onSelect) {
            this.ids = ids;
            this.titles = titles;
            this.running = running;
            this.selectedPos = defaultPos;
            this.onSelect = onSelect;
        }

        @NonNull
        @Override
        public ViewHolder onCreateViewHolder(@NonNull ViewGroup parent, int viewType) {
            android.widget.LinearLayout row = new android.widget.LinearLayout(parent.getContext());
            row.setOrientation(android.widget.LinearLayout.HORIZONTAL);
            row.setGravity(android.view.Gravity.CENTER_VERTICAL);
            row.setPadding(24, 16, 24, 16);

            // Running indicator: proper circle with GradientDrawable
            View dot = new View(parent.getContext());
            int dotSize = (int) (10 * parent.getContext().getResources().getDisplayMetrics().density);
            android.widget.LinearLayout.LayoutParams dotLp = new android.widget.LinearLayout.LayoutParams(dotSize, dotSize);
            dotLp.setMarginEnd(14);
            dot.setLayoutParams(dotLp);
            // Make dot circular via shape drawable
            android.graphics.drawable.GradientDrawable dotShape = new android.graphics.drawable.GradientDrawable();
            dotShape.setShape(android.graphics.drawable.GradientDrawable.OVAL);
            dotShape.setColor(ContextCompat.getColor(parent.getContext(), R.color.browser_running_dot));
            dot.setBackground(dotShape);

            // Title text
            TextView tv = new TextView(parent.getContext());
            tv.setTextSize(14);
            tv.setTextColor(ContextCompat.getColor(parent.getContext(), R.color.browser_url_text));
            tv.setEllipsize(android.text.TextUtils.TruncateAt.END);
            tv.setSingleLine(true);
            android.widget.LinearLayout.LayoutParams tvLp = new android.widget.LinearLayout.LayoutParams(0, android.widget.LinearLayout.LayoutParams.WRAP_CONTENT, 1f);
            tv.setLayoutParams(tvLp);

            // Selected indicator: radio-style circle instead of text checkmark
            View radio = new View(parent.getContext());
            int radioOuter = (int) (18 * parent.getContext().getResources().getDisplayMetrics().density);
            android.widget.LinearLayout.LayoutParams radioLp = new android.widget.LinearLayout.LayoutParams(radioOuter, radioOuter);
            radioLp.setMarginStart(12);
            radio.setLayoutParams(radioLp);

            row.addView(dot);
            row.addView(tv);
            row.addView(radio);

            RecyclerView.LayoutParams lp = new RecyclerView.LayoutParams(
                    RecyclerView.LayoutParams.MATCH_PARENT, RecyclerView.LayoutParams.WRAP_CONTENT);
            row.setLayoutParams(lp);

            return new ViewHolder(row, dot, tv, radio);
        }

        @Override
        public void onBindViewHolder(@NonNull ViewHolder holder, int position) {
            holder.title.setText(titles.get(position));
            boolean isRunning = running.get(position);

            // Running dot: green circle for active, invisible for inactive (preserves spacing)
            holder.dot.setVisibility(isRunning ? View.VISIBLE : View.INVISIBLE);

            // Selection indicator: filled circle for selected, empty ring for unselected
            boolean isSelected = position == selectedPos;
            android.graphics.drawable.GradientDrawable radioDrawable = new android.graphics.drawable.GradientDrawable();
            radioDrawable.setShape(android.graphics.drawable.GradientDrawable.OVAL);
            if (isSelected) {
                radioDrawable.setColor(ContextCompat.getColor(holder.row.getContext(), R.color.browser_accent_blue));
            } else {
                radioDrawable.setColor(0x00000000);
                radioDrawable.setStroke(2, ContextCompat.getColor(holder.row.getContext(), R.color.browser_icon_tint));
            }
            holder.radio.setBackground(radioDrawable);

            // Selection background: subtle highlight
            holder.row.setBackgroundColor(isSelected ? ContextCompat.getColor(holder.row.getContext(), R.color.browser_dialog_item_selected) : 0x00000000);

            holder.row.setOnClickListener(v -> {
                int pos = holder.getAdapterPosition();
                if (pos == RecyclerView.NO_POSITION) return;
                int oldPos = selectedPos;
                selectedPos = pos;
                notifyItemChanged(oldPos);
                notifyItemChanged(selectedPos);
                onSelect.accept(selectedPos);
            });
        }

        @Override
        public int getItemCount() {
            return ids.size();
        }

        class ViewHolder extends RecyclerView.ViewHolder {
            final android.widget.LinearLayout row;
            final View dot;
            final TextView title;
            final View radio;

            ViewHolder(android.widget.LinearLayout row, View dot, TextView title, View radio) {
                super(row);
                this.row = row;
                this.dot = dot;
                this.title = title;
                this.radio = radio;
            }
        }
    }
}
