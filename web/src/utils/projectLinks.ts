/**
 * Canonical project-identity links.
 *
 * Single source for the repository URL: the About page's homepage/feedback
 * entries and the upgrade dialog's release-notes link must never drift apart,
 * or one of them silently points at a fork or a dead path.
 */

/** Canonical GitHub repository URL. */
export const GITHUB_REPO_URL = 'https://github.com/xulongzhe/clawbench'

/** Project homepage — the marketing site (the About page's "项目主页"). */
export const PROJECT_HOMEPAGE_URL = 'https://clawbench.pages.dev'

/**
 * GitHub repository landing page (the About page's "GitHub 主页"). Kept
 * separate from the homepage: the site and the repository are different
 * destinations, and the homepage used to double as the repo URL.
 */
export const PROJECT_GITHUB_URL = GITHUB_REPO_URL

/**
 * Feedback entry: GitHub's issue template chooser rather than the bare issue
 * list, so the reporter lands on the bug / feature / question form directly.
 */
export const PROJECT_FEEDBACK_URL = `${GITHUB_REPO_URL}/issues/new/choose`
