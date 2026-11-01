import { Users } from 'lucide-vue-next'

/**
 * The one icon for 会议 (the live/share tab), shared by the dock tab, the
 * meeting list header/empty state and the meeting quote chip.
 *
 * A group of people, not a calendar: the tab's job is starting or joining a
 * meeting, and the archive is only its landing view. A camera would read as
 * "video call", which this product does not do — a meeting is screen sharing or
 * voice only.
 *
 * Exported as a constant so the three surfaces cannot drift apart; each of them
 * used to hard-code its own icon.
 */
export const MEETING_ICON = Users
