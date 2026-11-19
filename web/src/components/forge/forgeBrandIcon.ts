import { Github, Gitlab, GitFork } from 'lucide-vue-next'
import type { Component } from 'vue'
import type { ForgeBrand } from '@/composables/useForgeBinding'

/**
 * The lucide component that stands in for a forge brand.
 *
 * Shared by the dock tab and the panel header so the two can never disagree
 * about which service the tab opens. Lucide ships GitHub and GitLab marks but no
 * Gitea one, so Gitea/Forgejo uses a neutral fork glyph — the label ("Gitea /
 * Forgejo integration") carries the name.
 */
export function forgeBrandIcon(brand: ForgeBrand): Component {
    switch (brand) {
        case 'gitlab':
            return Gitlab
        case 'gitea':
            return GitFork
        default:
            return Github
    }
}
