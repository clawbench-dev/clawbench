package com.clawbench.app;

import org.json.JSONArray;
import org.json.JSONObject;

import java.net.URLDecoder;
import java.util.List;

/**
 * Pure rules for "share into ClawBench" (ACTION_SEND / ACTION_SEND_MULTIPLE).
 *
 * Kept out of MainActivity so they can be unit-tested: the Intent, the upload
 * and the WebView hand-off all need a live Activity, but the decisions that
 * actually break (what counts as importable, what the file is called, which
 * project the upload landed in, what the page receives) do not.
 */
final class ShareInPolicy {

    /** Directory the shell uploads into; the app reads it back as "recent shares". */
    static final String SHARE_DIR = ".clawbench/share-in";

    /** The frontend rejects a batch larger than this (see nativeShareTargetResult). */
    static final int MAX_RESULT_PATHS = 20;

    private ShareInPolicy() {}

    /**
     * Text carried by a share, or null when there is nothing usable.
     *
     * Sharing a link or a snippet is an ordinary ACTION_SEND with EXTRA_TEXT
     * and no file URI at all. The shell used to ignore those completely, so
     * the app opened and nothing happened. The text is imported as a .txt
     * file instead of a separate "text attachment" path: the composer, the
     * file viewer and the AI already understand files, and a second kind of
     * attachment would have to duplicate all three.
     */
    static String textPayload(CharSequence text) {
        if (text == null) return null;
        String value = text.toString().trim();
        return value.isEmpty() ? null : value;
    }

    /**
     * File name for an imported text share: the subject when it has anything
     * usable in it, otherwise a timestamped name.
     *
     * Path separators and control characters are dropped rather than replaced,
     * because this name is also the file's identity in the composer.
     */
    static String textFileName(CharSequence subject, long timestamp) {
        StringBuilder stem = new StringBuilder();
        boolean usable = false;
        if (subject != null) {
            String value = subject.toString().trim();
            for (int i = 0; i < value.length() && stem.length() < 60; i++) {
                char c = value.charAt(i);
                if (Character.isLetterOrDigit(c) || c == '-' || c == '_') {
                    stem.append(c);
                    usable = true;
                } else if (c == ' ' || c == '.') {
                    stem.append('_');
                }
            }
        }
        while (stem.length() > 0 && stem.charAt(stem.length() - 1) == '_') {
            stem.setLength(stem.length() - 1);
        }
        // A subject of pure punctuation sanitizes to separators only; that is
        // not a usable name, so fall back rather than produce "_.txt".
        if (!usable) {
            return "shared_text_" + timestamp + ".txt";
        }
        return stem + ".txt";
    }

    /**
     * Project the upload landed in, read from the session cookie the WebView
     * holds.
     *
     * The server may prefix the cookie with a port marker
     * (`cb20000_clawbench_project`) and the value is URL-encoded. Returns null
     * when the cookie is absent or ambiguous: the caller then falls back to a
     * native toast instead of handing the page a payload it would reject.
     */
    static String projectPathFromCookie(String cookieHeader) {
        if (cookieHeader == null || cookieHeader.isEmpty()) return null;
        String project = null;
        for (String item : cookieHeader.split(";")) {
            String pair = item.trim();
            int eq = pair.indexOf('=');
            if (eq <= 0) continue;
            String name = pair.substring(0, eq).trim();
            if (!name.equals("clawbench_project") && !name.matches("cb\\d{1,5}_clawbench_project")) {
                continue;
            }
            // Two project cookies would mean two answers; refuse rather than
            // pick one and attach the files to the wrong project.
            if (project != null) return null;
            try {
                project = URLDecoder.decode(pair.substring(eq + 1).trim(), "UTF-8");
            } catch (Exception e) {
                return null;
            }
        }
        return project == null || project.isEmpty() ? null : project;
    }

    /**
     * JSON handed to the page as `clawbench-share-imported` detail.
     *
     * Mirrors the shape `nativeShareTargetResult` validates: at most
     * {@link #MAX_RESULT_PATHS} paths, and `failed` counting everything that
     * did not make it (including the overflow, which is reported as failed
     * rather than silently truncated).
     */
    static String resultJson(List<String> paths, int failed, String projectPath) {
        int dropped = Math.max(0, paths.size() - MAX_RESULT_PATHS);
        int keep = Math.min(paths.size(), MAX_RESULT_PATHS);
        try {
            JSONArray array = new JSONArray();
            for (int i = 0; i < keep; i++) {
                array.put(paths.get(i));
            }
            JSONObject detail = new JSONObject();
            detail.put("paths", array);
            detail.put("failed", Math.min(MAX_RESULT_PATHS, Math.max(0, failed) + dropped));
            detail.put("cancelled", false);
            detail.put("projectPath", projectPath);
            return detail.toString();
        } catch (Exception e) {
            return null;
        }
    }
}
