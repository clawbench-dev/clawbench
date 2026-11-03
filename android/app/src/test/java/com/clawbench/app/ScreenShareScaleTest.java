package com.clawbench.app;

import static org.junit.Assert.assertArrayEquals;

import org.junit.Test;

/**
 * Frame downscaling for the bridge.
 *
 * The rule that matters is that the cap applies to the <em>longest</em> side: a
 * phone screen is 1080x2400, and capping the width alone would still encode a
 * 2400-pixel-tall frame that the viewer immediately downscales — several times
 * the bridge traffic for no visible gain.
 */
public class ScreenShareScaleTest {

    @Test
    public void leavesFramesThatAlreadyFit() {
        assertArrayEquals(new int[]{800, 600}, ScreenShareService.scaleTo(800, 600, 1280));
        assertArrayEquals(new int[]{1280, 720}, ScreenShareService.scaleTo(1280, 720, 1280));
    }

    @Test
    public void capsTheLongestSideInPortrait() {
        // 1080x2400 with a 1280 cap → height becomes 1280, width follows.
        int[] size = ScreenShareService.scaleTo(1080, 2400, 1280);
        assertArrayEquals(new int[]{576, 1280}, size);
    }

    @Test
    public void capsTheLongestSideInLandscape() {
        int[] size = ScreenShareService.scaleTo(2400, 1080, 1280);
        assertArrayEquals(new int[]{1280, 576}, size);
    }

    @Test
    public void neverProducesADegenerateSize() {
        // A 1-pixel-tall strip must not round down to zero.
        int[] size = ScreenShareService.scaleTo(4000, 1, 1280);
        assertArrayEquals(new int[]{1280, 2}, size);
    }
}
