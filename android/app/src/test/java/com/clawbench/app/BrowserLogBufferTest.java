package com.clawbench.app;

import org.junit.Before;
import org.junit.Test;

import java.util.List;

import static org.junit.Assert.*;

/**
 * Tests for BrowserLogBuffer: circular buffer with level-based filtering.
 */
public class BrowserLogBufferTest {

    private BrowserLogBuffer buffer;

    @Before
    public void setUp() {
        buffer = new BrowserLogBuffer(5);
    }

    // --- add / retrieve ---

    @Test
    public void add_andRetrieve_singleEntry() {
        buffer.add('E', "Tag1", "msg1");
        List<BrowserLogBuffer.Entry> entries = buffer.getEntries();
        assertEquals(1, entries.size());
        assertEquals('E', entries.get(0).level);
        assertEquals("Tag1", entries.get(0).tag);
        assertEquals("msg1", entries.get(0).msg);
        assertTrue(entries.get(0).ts > 0);
    }

    @Test
    public void add_multipleEntries_preservesOrder() {
        buffer.add('D', "T1", "first");
        buffer.add('W', "T2", "second");
        buffer.add('E', "T3", "third");
        List<BrowserLogBuffer.Entry> entries = buffer.getEntries();
        assertEquals(3, entries.size());
        assertEquals("first", entries.get(0).msg);
        assertEquals("second", entries.get(1).msg);
        assertEquals("third", entries.get(2).msg);
    }

    // --- capacity eviction ---

    @Test
    public void add_exceedsCapacity_evictsOldest() {
        buffer.add('D', "T", "msg0");
        buffer.add('D', "T", "msg1");
        buffer.add('D', "T", "msg2");
        buffer.add('D', "T", "msg3");
        buffer.add('D', "T", "msg4");
        // At capacity now, next add evicts msg0
        buffer.add('D', "T", "msg5");
        List<BrowserLogBuffer.Entry> entries = buffer.getEntries();
        assertEquals(5, entries.size());
        assertEquals("msg1", entries.get(0).msg);
        assertEquals("msg5", entries.get(4).msg);
    }

    @Test
    public void add_farExceedsCapacity_keepsOnlyLatest() {
        for (int i = 0; i < 20; i++) {
            buffer.add('D', "T", "msg" + i);
        }
        List<BrowserLogBuffer.Entry> entries = buffer.getEntries();
        assertEquals(5, entries.size());
        assertEquals("msg15", entries.get(0).msg);
        assertEquals("msg19", entries.get(4).msg);
    }

    // --- clear ---

    @Test
    public void clear_emptiesBuffer() {
        buffer.add('D', "T", "msg");
        buffer.clear();
        assertTrue(buffer.getEntries().isEmpty());
    }

    @Test
    public void clear_emptyBuffer_noException() {
        buffer.clear();
        assertTrue(buffer.getEntries().isEmpty());
    }

    // --- filterByLevel ---

    @Test
    public void getFiltered_returnsOnlyMatchingLevel() {
        buffer.add('E', "T1", "error1");
        buffer.add('W', "T2", "warn1");
        buffer.add('E', "T3", "error2");
        buffer.add('D', "T4", "debug1");

        List<BrowserLogBuffer.Entry> errors = buffer.getFiltered('E');
        assertEquals(2, errors.size());
        assertEquals("error1", errors.get(0).msg);
        assertEquals("error2", errors.get(1).msg);
    }

    @Test
    public void getFiltered_noMatch_returnsEmpty() {
        buffer.add('D', "T", "debug");
        List<BrowserLogBuffer.Entry> errors = buffer.getFiltered('E');
        assertTrue(errors.isEmpty());
    }

    @Test
    public void getFiltered_emptyBuffer_returnsEmpty() {
        assertTrue(buffer.getFiltered('E').isEmpty());
    }

    // --- Entry equals/hashCode ---

    @Test
    public void entry_equals_sameFields() {
        long ts = System.currentTimeMillis();
        BrowserLogBuffer.Entry a = new BrowserLogBuffer.Entry('E', "Tag", "msg", ts);
        BrowserLogBuffer.Entry b = new BrowserLogBuffer.Entry('E', "Tag", "msg", ts);
        assertEquals(a, b);
        assertEquals(a.hashCode(), b.hashCode());
    }

    @Test
    public void entry_equals_differentLevel_notEqual() {
        long ts = System.currentTimeMillis();
        BrowserLogBuffer.Entry a = new BrowserLogBuffer.Entry('E', "Tag", "msg", ts);
        BrowserLogBuffer.Entry b = new BrowserLogBuffer.Entry('W', "Tag", "msg", ts);
        assertNotEquals(a, b);
    }

    @Test
    public void entry_equals_differentMsg_notEqual() {
        long ts = System.currentTimeMillis();
        BrowserLogBuffer.Entry a = new BrowserLogBuffer.Entry('E', "Tag", "msg1", ts);
        BrowserLogBuffer.Entry b = new BrowserLogBuffer.Entry('E', "Tag", "msg2", ts);
        assertNotEquals(a, b);
    }

    @Test
    public void entry_equals_differentTs_notEqual() {
        BrowserLogBuffer.Entry a = new BrowserLogBuffer.Entry('E', "Tag", "msg", 1000);
        BrowserLogBuffer.Entry b = new BrowserLogBuffer.Entry('E', "Tag", "msg", 2000);
        assertNotEquals(a, b);
    }

    @Test
    public void entry_equals_null_notEqual() {
        BrowserLogBuffer.Entry a = new BrowserLogBuffer.Entry('E', "Tag", "msg", 1000);
        assertNotEquals(a, null);
    }

    @Test
    public void entry_equals_differentType_notEqual() {
        BrowserLogBuffer.Entry a = new BrowserLogBuffer.Entry('E', "Tag", "msg", 1000);
        assertNotEquals(a, "string");
    }

    @Test
    public void entry_equals_self() {
        BrowserLogBuffer.Entry a = new BrowserLogBuffer.Entry('E', "Tag", "msg", 1000);
        assertEquals(a, a);
    }

    @Test
    public void entry_equals_differentTag_notEqual() {
        long ts = System.currentTimeMillis();
        BrowserLogBuffer.Entry a = new BrowserLogBuffer.Entry('E', "Tag1", "msg", ts);
        BrowserLogBuffer.Entry b = new BrowserLogBuffer.Entry('E', "Tag2", "msg", ts);
        assertNotEquals(a, b);
    }
}
