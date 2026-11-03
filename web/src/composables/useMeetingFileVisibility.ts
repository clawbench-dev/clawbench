/**
 * Keeps the meeting's file timeline in step with what the viewer is showing.
 *
 * The timeline used to learn about files only through
 * `useFileNavStack.openFile()`. That is the right chokepoint for *opens*, but a
 * file that was already open when the meeting started never fires it: the host
 * switches to the file tab mid-meeting, reads it for ten minutes, and the
 * record claims nothing was ever opened. Visibility is the missing half —
 * entering the viewer with a file on screen starts an interval, leaving it ends
 * the one that is open — and it also keeps the interval honest when the host
 * only glances at a file and goes back to the meeting.
 *
 * A quick flip to another tab and back does not split the interval: the tracker
 * merges a reopen within its coalesce window, so the noise stays out of the
 * timeline while a real absence still reads as two visits.
 */
import { watch } from 'vue'
import { noteMeetingFileClosed, noteMeetingFileOpened } from '@/utils/meetingFileTracker'

/**
 * @param visiblePath The file path on screen right now, or null/empty when no
 *   file is being looked at (viewer closed, another tab active, the left column
 *   collapsed). The tracker itself ignores calls outside a running meeting, so
 *   callers do not need to know whether one is being recorded.
 */
export function useMeetingFileVisibility(visiblePath: () => string | null | undefined): void {
  watch(
    () => visiblePath() ?? '',
    (path) => {
      if (path) noteMeetingFileOpened(path, 'preview')
      else noteMeetingFileClosed()
    },
  )
}
