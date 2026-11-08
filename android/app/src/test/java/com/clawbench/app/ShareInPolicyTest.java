package com.clawbench.app;

import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

import static org.junit.Assert.*;

/**
 * Unit tests for the rules behind "share into ClawBench".
 *
 * The Intent, the upload and the WebView hand-off need a live Activity; the
 * decisions that actually break (what counts as importable, the file name, the
 * destination project, the payload the page receives) are pure and covered
 * here.
 */
public class ShareInPolicyTest {

    // ── Text shares (links / snippets) ──

    @Test
    public void textPayload_trimsAndKeepsContent() {
        assertEquals("https://example.com/a?b=1", ShareInPolicy.textPayload("  https://example.com/a?b=1\n"));
    }

    @Test
    public void textPayload_isNullWhenThereIsNothingToImport() {
        assertNull(ShareInPolicy.textPayload(null));
        assertNull(ShareInPolicy.textPayload(""));
        assertNull(ShareInPolicy.textPayload("   \n\t "));
    }

    @Test
    public void textFileName_usesTheSubject() {
        assertEquals("Quarterly_report.txt", ShareInPolicy.textFileName("Quarterly report", 1234L));
    }

    @Test
    public void textFileName_fallsBackToTheTimestamp() {
        assertEquals("shared_text_1234.txt", ShareInPolicy.textFileName(null, 1234L));
        assertEquals("shared_text_1234.txt", ShareInPolicy.textFileName("  ", 1234L));
        // A subject made only of punctuation has nothing usable left in it.
        assertEquals("shared_text_1234.txt", ShareInPolicy.textFileName("/// ???", 1234L));
    }

    @Test
    public void textFileName_dropsPathSeparatorsAndControlCharacters() {
        // The name becomes the file's identity in the composer; a separator
        // would make it look like a path the app could be tricked into using.
        String name = ShareInPolicy.textFileName("../etc/passwd", 1L);
        assertFalse(name.contains("/"));
        assertFalse(name.contains("\\"));
        assertTrue(name.endsWith(".txt"));
    }

    @Test
    public void textFileName_keepsNonAsciiTitles() {
        assertEquals("会议纪要.txt", ShareInPolicy.textFileName("会议纪要", 1L));
    }

    @Test
    public void textFileName_isBounded() {
        String name = ShareInPolicy.textFileName(new String(new char[500]).replace('\0', 'a'), 1L);
        assertEquals(64, name.length()); // 60 characters + ".txt"
    }

    // ── Destination project ──

    @Test
    public void projectPathFromCookie_readsThePlainCookie() {
        assertEquals("/home/user/project",
                ShareInPolicy.projectPathFromCookie("a=1; clawbench_project=%2Fhome%2Fuser%2Fproject; b=2"));
    }

    @Test
    public void projectPathFromCookie_acceptsThePortPrefixedName() {
        // The server prefixes the cookie with the listening port on some
        // deployments; missing that prefix would make every share fall back to
        // the "go find it yourself" toast.
        assertEquals("/home/user/project",
                ShareInPolicy.projectPathFromCookie("cb20000_clawbench_project=%2Fhome%2Fuser%2Fproject"));
    }

    @Test
    public void projectPathFromCookie_decodesSpacesAndUnicode() {
        assertEquals("/home/张 三/project",
                ShareInPolicy.projectPathFromCookie("clawbench_project=%2Fhome%2F%E5%BC%A0+%E4%B8%89%2Fproject"));
    }

    @Test
    public void projectPathFromCookie_isNullWhenAbsentOrAmbiguous() {
        assertNull(ShareInPolicy.projectPathFromCookie(null));
        assertNull(ShareInPolicy.projectPathFromCookie(""));
        assertNull(ShareInPolicy.projectPathFromCookie("a=1; b=2"));
        // Two projects in one header: refuse rather than attach the files to a
        // project the user may not be looking at.
        assertNull(ShareInPolicy.projectPathFromCookie(
                "clawbench_project=%2Fa; clawbench_project=%2Fb"));
    }

    // ── Payload handed to the page ──

    @Test
    public void resultJson_matchesTheShapeTheFrontendValidates() throws Exception {
        JSONObject detail = new JSONObject(ShareInPolicy.resultJson(
                Arrays.asList(".clawbench/share-in/a.png", ".clawbench/share-in/b.txt"),
                1, "/project"));

        JSONArray paths = detail.getJSONArray("paths");
        assertEquals(2, paths.length());
        assertEquals(".clawbench/share-in/a.png", paths.getString(0));
        assertEquals(1, detail.getInt("failed"));
        assertFalse(detail.getBoolean("cancelled"));
        assertEquals("/project", detail.getString("projectPath"));
    }

    @Test
    public void resultJson_reportsOverflowAsFailuresInsteadOfDroppingIt() throws Exception {
        List<String> paths = new ArrayList<>();
        for (int i = 0; i < 25; i++) {
            paths.add(".clawbench/share-in/" + i + ".png");
        }

        JSONObject detail = new JSONObject(ShareInPolicy.resultJson(paths, 0, "/project"));

        // The frontend rejects a batch larger than 20 outright, which would
        // turn a big share into "nothing happened".
        assertEquals(ShareInPolicy.MAX_RESULT_PATHS, detail.getJSONArray("paths").length());
        assertEquals(5, detail.getInt("failed"));
    }

    @Test
    public void resultJson_keepsFailedWithinTheFrontendLimit() throws Exception {
        List<String> paths = new ArrayList<>();
        paths.add(".clawbench/share-in/a.png");

        JSONObject detail = new JSONObject(ShareInPolicy.resultJson(paths, 50, "/project"));

        assertEquals(ShareInPolicy.MAX_RESULT_PATHS, detail.getInt("failed"));
    }
}
