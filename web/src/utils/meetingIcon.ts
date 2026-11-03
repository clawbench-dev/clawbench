import { MonitorUp } from 'lucide-vue-next'

/**
 * The one icon for 视频会议 (the live/share tab), shared by the dock tab, the
 * meeting list header/empty state and the meeting quote chip.
 *
 * A monitor with an outgoing arrow — the glyph this feature has had since it
 * was only screen sharing. It is the picture half of a meeting (the thing you
 * actually see on the other end), which is what distinguishes the tab from the
 * voice-only and archive readings a mic or a calendar would give.
 *
 * Exported as a constant so the three surfaces cannot drift apart; each of them
 * used to hard-code its own icon.
 */
export const MEETING_ICON = MonitorUp
