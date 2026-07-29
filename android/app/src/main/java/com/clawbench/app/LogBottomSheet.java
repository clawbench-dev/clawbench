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
import android.view.inputmethod.EditorInfo;
import android.widget.EditText;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
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


    private RecyclerView recyclerView;
    private TextView emptyView;
    private EditText searchInput;
    private EditText messageInput;
    private LogAdapter adapter;
    private final Handler uiHandler = new Handler(Looper.getMainLooper());
    private final ExecutorService networkExecutor = Executors.newSingleThreadExecutor();

    private char currentFilter = '\0'; // '\0' = all
    private String currentSearch = "";
    private String sessionId;
    private String serverUrl;
    private String sessionCookie;

    // Selected log entry sequence numbers (unique identity, unlike timestamps which can collide)
    private final Set<Long> selectedSeqs = new HashSet<>();

    private static final SimpleDateFormat SDF_FULL = new SimpleDateFormat("HH:mm:ss.SSS", Locale.ROOT);
    private static final SimpleDateFormat SDF_SHORT = new SimpleDateFormat("HH:mm:ss", Locale.ROOT);
    private static final int COLOR_SELECTION = 0x1F58a6ff;
    private Runnable refreshRunnable;
    private static final long REFRESH_INTERVAL_MS = 500;

    public static LogBottomSheet newInstance() {
        return new LogBottomSheet();
    }

    @Override
    public void onCreate(@Nullable Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        // Read credentials securely from BrowserSessionCredentials instead of Bundle args.
        // This avoids exposing the session cookie in Fragment arguments (which can appear in logcat).
        BrowserSessionCredentials.Creds creds = BrowserSessionCredentials.get(requireContext());
        if (creds != null) {
            sessionId = creds.sessionId;
            serverUrl = creds.serverUrl;
            sessionCookie = creds.sessionCookie;
        }
    }

    @Nullable
    @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container, @Nullable Bundle savedInstanceState) {
        return inflater.inflate(R.layout.bottom_sheet_log, container, false);
    }

    @Override
    public void onViewCreated(@NonNull View view, @Nullable Bundle savedInstanceState) {
        super.onViewCreated(view, savedInstanceState);

        recyclerView = view.findViewById(R.id.logRecyclerView);
        emptyView = view.findViewById(R.id.logEmptyView);
        searchInput = view.findViewById(R.id.logSearchInput);
        messageInput = view.findViewById(R.id.messageInput);

        adapter = new LogAdapter();
        recyclerView.setLayoutManager(new LinearLayoutManager(requireContext()));
        recyclerView.setAdapter(adapter);

        // Filter chips
        view.findViewById(R.id.filterAll).setOnClickListener(v -> { currentFilter = '\0'; refreshList(); });
        view.findViewById(R.id.filterError).setOnClickListener(v -> { currentFilter = 'E'; refreshList(); });
        view.findViewById(R.id.filterWarn).setOnClickListener(v -> { currentFilter = 'W'; refreshList(); });
        view.findViewById(R.id.filterLog).setOnClickListener(v -> { currentFilter = 'D'; refreshList(); });

        // Search
        searchInput.addTextChangedListener(new TextWatcher() {
            @Override public void beforeTextChanged(CharSequence s, int start, int count, int after) {}
            @Override public void onTextChanged(CharSequence s, int start, int before, int count) {}
            @Override public void afterTextChanged(Editable s) {
                currentSearch = s.toString().toLowerCase(Locale.ROOT);
                refreshList();
            }
        });

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
            sendToSession(formatEntries(selected));
        });

        // Send all
        view.findViewById(R.id.btnSendAll).setOnClickListener(v -> {
            List<BrowserLogBuffer.Entry> all = getFilteredEntries();
            if (all.isEmpty()) return;
            sendToSession(formatEntries(all));
        });

        // Send message (text only, no implicit log attachment)
        view.findViewById(R.id.btnSendMessage).setOnClickListener(v -> sendMessage());
        messageInput.setOnEditorActionListener((v, actionId, event) -> {
            if (actionId == EditorInfo.IME_ACTION_SEND) {
                sendMessage();
                return true;
            }
            return false;
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
    public void onDestroyView() {
        super.onDestroyView();
        if (refreshRunnable != null) {
            uiHandler.removeCallbacks(refreshRunnable);
        }
        networkExecutor.shutdownNow();
    }

    private BrowserLogBuffer getLogBuffer() {
        if (getActivity() instanceof BrowserActivity) {
            return ((BrowserActivity) getActivity()).getLogBuffer();
        }
        // Should not happen — LogBottomSheet is only used within BrowserActivity
        throw new IllegalStateException("LogBottomSheet requires BrowserActivity host");
    }

    private void sendMessage() {
        String text = messageInput.getText().toString().trim();
        if (text.isEmpty()) return;
        sendToSession(text);
        messageInput.setText("");
    }

    private List<BrowserLogBuffer.Entry> getFilteredEntries() {
        List<BrowserLogBuffer.Entry> all = getLogBuffer().getEntries();
        List<BrowserLogBuffer.Entry> filtered = new ArrayList<>();
        for (BrowserLogBuffer.Entry e : all) {
            if (currentFilter != '\0' && e.level != currentFilter) continue;
            if (!currentSearch.isEmpty() && !e.msg.toLowerCase(Locale.ROOT).contains(currentSearch)
                    && !e.tag.toLowerCase(Locale.ROOT).contains(currentSearch)) continue;
            filtered.add(e);
        }
        return filtered;
    }

    private List<BrowserLogBuffer.Entry> getSelectedEntries() {
        List<BrowserLogBuffer.Entry> filtered = getFilteredEntries();
        List<BrowserLogBuffer.Entry> selected = new ArrayList<>();
        for (BrowserLogBuffer.Entry e : filtered) {
            if (selectedSeqs.contains(e.ts)) selected.add(e);
        }
        return selected;
    }

    private String formatEntries(List<BrowserLogBuffer.Entry> entries) {
        SimpleDateFormat sdf = SDF_FULL;
        StringBuilder sb = new StringBuilder();
        for (BrowserLogBuffer.Entry e : entries) {
            sb.append(String.format(Locale.ROOT, "%s %c/%s: %s\n",
                    sdf.format(new Date(e.ts)), e.level, e.tag, e.msg));
        }
        return sb.toString();
    }

    private void sendToSession(String content) {
        if (sessionId == null || sessionId.isEmpty() || serverUrl == null || serverUrl.isEmpty()) {
            Toast.makeText(requireContext(), R.string.browser_log_no_session, Toast.LENGTH_SHORT).show();
            return;
        }
        networkExecutor.execute(() -> {
            try {
                String url = serverUrl + "/api/ai/chat?session_id=" + URLEncoder.encode(sessionId, "UTF-8");
                org.json.JSONObject body = new org.json.JSONObject();
                body.put("message", content);
                byte[] data = body.toString().getBytes("UTF-8");

                URL urlObj = new URL(url);
                HttpURLConnection conn = (HttpURLConnection) urlObj.openConnection();
                try {
                    conn.setRequestMethod("POST");
                    conn.setRequestProperty("Content-Type", "application/json");
                    conn.setRequestProperty("Cookie", sessionCookie);
                    conn.setConnectTimeout(5000);
                    conn.setReadTimeout(5000);
                    conn.setDoOutput(true);

                    // Trust self-signed certs using BrowserActivity's own SSL logic
                    if (conn instanceof javax.net.ssl.HttpsURLConnection) {
                        SSLHelper.setupTrustAll((javax.net.ssl.HttpsURLConnection) conn);
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
                        });
                    } else if (code == 401 || code == 403) {
                        AppLog.w("LogBottomSheet", "Auth expired: HTTP " + code);
                        uiHandler.post(() -> {
                            if (!isAdded()) return;
                            Toast.makeText(requireContext(), R.string.browser_log_auth_expired, Toast.LENGTH_SHORT).show();
                        });
                    } else {
                        AppLog.w("LogBottomSheet", "Send failed: HTTP " + code);
                        uiHandler.post(() -> {
                            if (!isAdded()) return;
                            Toast.makeText(requireContext(), R.string.browser_log_send_failed, Toast.LENGTH_SHORT).show();
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
                });
            }
        });
    }

    private void refreshList() {
        List<BrowserLogBuffer.Entry> filtered = getFilteredEntries();
        adapter.setEntries(filtered);
        recyclerView.setVisibility(filtered.isEmpty() ? View.GONE : View.VISIBLE);
        emptyView.setVisibility(filtered.isEmpty() ? View.VISIBLE : View.GONE);
    }

    // --- RecyclerView Adapter ---

    private class LogAdapter extends RecyclerView.Adapter<LogAdapter.ViewHolder> {

        private List<BrowserLogBuffer.Entry> entries = Collections.emptyList();

        void setEntries(List<BrowserLogBuffer.Entry> entries) {
            // Skip update if nothing changed (avoids unnecessary rebinds on 500ms auto-refresh)
            if (this.entries.size() == entries.size() && !this.entries.isEmpty()) {
                BrowserLogBuffer.Entry lastOld = this.entries.get(this.entries.size() - 1);
                BrowserLogBuffer.Entry lastNew = entries.get(entries.size() - 1);
                if (lastOld.seq == lastNew.seq) return;
            }
            this.entries = entries;
            notifyDataSetChanged();
        }

        @NonNull
        @Override
        public ViewHolder onCreateViewHolder(@NonNull ViewGroup parent, int viewType) {
            TextView tv = new TextView(parent.getContext());
            tv.setPadding(8, 4, 8, 4);
            tv.setTextSize(11);
            if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.M) {
                tv.setBreakStrategy(0); // BREAK_STRATEGY_SIMPLE
            }
            RecyclerView.LayoutParams lp = new RecyclerView.LayoutParams(
                    RecyclerView.LayoutParams.MATCH_PARENT, RecyclerView.LayoutParams.WRAP_CONTENT);
            tv.setLayoutParams(lp);
            return new ViewHolder(tv);
        }

        @Override
        public void onBindViewHolder(@NonNull ViewHolder holder, int position) {
            BrowserLogBuffer.Entry entry = entries.get(position);
            SimpleDateFormat sdf = SDF_SHORT;
            String time = sdf.format(new Date(entry.seq));
            String text = time + " " + entry.level + "/" + entry.tag + ": " + entry.msg;

            TextView tv = (TextView) holder.itemView;
            tv.setText(text);

            boolean isSelected = selectedSeqs.contains(entry.seq);
            tv.setBackgroundColor(isSelected ? COLOR_SELECTION : 0x00000000);

            switch (entry.level) {
                case 'E': tv.setTextColor(0xFFF85149); break;
                case 'W': tv.setTextColor(0xFFD29922); break;
                default:  tv.setTextColor(0xFF8B949E); break;
            }

            tv.setOnClickListener(v -> {
                if (selectedSeqs.contains(entry.seq)) {
                    selectedSeqs.remove(entry.seq);
                } else {
                    selectedSeqs.add(entry.seq);
                }
                notifyItemChanged(holder.getAdapterPosition());
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
}
