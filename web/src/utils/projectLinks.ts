/**
 * Canonical project-identity links.
 *
 * Single source for the repository URL: the About page's homepage/feedback
 * entries and the upgrade dialog's release-notes link must never drift apart,
 * or one of them silently points at a fork or a dead path.
 */

/** Canonical GitHub repository URL. */
export const GITHUB_REPO_URL = 'https://github.com/xulongzhe/clawbench'

/** Project homepage — the repository landing page (the About page's "官网"). */
export const PROJECT_HOMEPAGE_URL = GITHUB_REPO_URL

/**
 * Feedback entry: GitHub's issue template chooser rather than the bare issue
 * list, so the reporter lands on the bug / feature / question form directly.
 */
export const PROJECT_FEEDBACK_URL = `${GITHUB_REPO_URL}/issues/new/choose`
