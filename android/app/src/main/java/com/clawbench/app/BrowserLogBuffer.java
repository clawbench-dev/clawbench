package com.clawbench.app;

import java.util.ArrayList;
import java.util.List;
import java.util.Objects;
import java.util.concurrent.atomic.AtomicLong;

/**
 * In-memory circular buffer for console log entries captured by BrowserActivity.
 * Independent from AppLog's static buffer to avoid cross-process conflicts
 * (BrowserActivity runs in :browser process).
 */
public class BrowserLogBuffer {

    public static class Entry {
        public final char level;
        public final String tag;
        public final String msg;
        public final long ts;
        /** Monotonically increasing sequence number for unique identity (unlike ts which can collide). */
        public final long seq;

        public Entry(char level, String tag, String msg, long ts, long seq) {
            this.level = level;
            this.tag = tag;
            this.msg = msg;
            this.ts = ts;
            this.seq = seq;
        }

        @Override
        public boolean equals(Object o) {
            if (this == o) return true;
            if (!(o instanceof Entry)) return false;
            Entry entry = (Entry) o;
            return seq == entry.seq;
        }

        @Override
        public int hashCode() {
            return Long.hashCode(seq);
        }
    }

    private final int capacity;
    private final List<Entry> entries;
    private final Object lock = new Object();
    private final AtomicLong seqCounter = new AtomicLong(0);

    public BrowserLogBuffer(int capacity) {
        this.capacity = capacity;
        this.entries = new ArrayList<>(capacity);
    }

    public void add(char level, String tag, String msg) {
        synchronized (lock) {
            if (entries.size() >= capacity) {
                entries.remove(0);
            }
            entries.add(new Entry(level, tag, msg, System.currentTimeMillis(), seqCounter.getAndIncrement()));
        }
    }

    public List<Entry> getEntries() {
        synchronized (lock) {
            return new ArrayList<>(entries);
        }
    }

    public List<Entry> getFiltered(char level) {
        synchronized (lock) {
            List<Entry> filtered = new ArrayList<>();
            for (Entry e : entries) {
                if (e.level == level) filtered.add(e);
            }
            return filtered;
        }
    }

    public void clear() {
        synchronized (lock) {
            entries.clear();
        }
    }
}
