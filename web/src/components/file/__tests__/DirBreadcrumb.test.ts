import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import DirBreadcrumb from '@/components/file/DirBreadcrumb.vue'

const LucideStub = { template: '<span class="lucide-stub" />' }

function mountBreadcrumb(props: Record<string, any> = {}) {
  return mount(DirBreadcrumb, {
    props: { path: '', ...props },
    global: {
      stubs: { 'lucide-vue-next': LucideStub },
    },
  })
}

describe('DirBreadcrumb', () => {
  // ── reconstructPath (exposed via navigate emission) ──

  describe('reconstructPath via navigate emission', () => {
    it('reconstructs Unix path from segments', async () => {
      const wrapper = mountBreadcrumb({ path: '/home/user/docs' })
      // Click the second crumb ("home") — index 0 in parts
      const crumbs = wrapper.findAll('.crumb')
      // crumbs[0] = root (CircleDot), crumbs[1] = "home", crumbs[2] = "user", crumbs[3] = "docs"
      // Clicking "home" (not last) should emit navigate with "home" (relative path, no leading slash)
      await crumbs[1].trigger('click')
      const emitted = wrapper.emitted('navigate')
      expect(emitted).toBeTruthy()
      expect(emitted![emitted!.length - 1][0]).toBe('home')
    })

    it('reconstructs Windows path from segments', async () => {
      const wrapper = mountBreadcrumb({ path: 'C:\\Users\\admin\\docs' })
      const crumbs = wrapper.findAll('.crumb')
      // parts: ["C:\", "Users", "admin", "docs"]
      // crumbs[0] = root, crumbs[1] = "C:\", crumbs[2] = "Users", crumbs[3] = "admin", crumbs[4] = "docs"
      // Click "Users" (not last) => navigate with "C:\Users"
      await crumbs[2].trigger('click')
      const emitted = wrapper.emitted('navigate')
      expect(emitted).toBeTruthy()
      expect(emitted![emitted!.length - 1][0]).toBe('C:\\Users')
    })

    it('reconstructs Windows path to drive root', async () => {
      const wrapper = mountBreadcrumb({ path: 'C:\\Users\\admin' })
      const crumbs = wrapper.findAll('.crumb')
      // Click "C:\" (not last) => navigate with "C:\"
      await crumbs[1].trigger('click')
      const emitted = wrapper.emitted('navigate')
      expect(emitted).toBeTruthy()
      expect(emitted![emitted!.length - 1][0]).toBe('C:\\')
    })
  })

  // ── parts computed ──

  describe('parts computed', () => {
    it('splits Unix path into segments', () => {
      const wrapper = mountBreadcrumb({ path: '/home/user/docs' })
      // crumbs: [root_icon, "home", "user", "docs"]
      const crumbs = wrapper.findAll('.crumb')
      expect(crumbs.length).toBe(4) // root + 3 segments
      expect(crumbs[1].text()).toBe('home')
      expect(crumbs[2].text()).toBe('user')
      expect(crumbs[3].text()).toBe('docs')
    })

    it('merges bare drive letter C: into C:\\', () => {
      const wrapper = mountBreadcrumb({ path: 'C:\\Users\\admin' })
      const crumbs = wrapper.findAll('.crumb')
      // splitPath("C:\Users\admin") => ["C:", "Users", "admin"]
      // parts merges "C:" => "C:\", so parts = ["C:\", "Users", "admin"]
      // crumbs: [root_icon, "C:\", "Users", "admin"]
      expect(crumbs[1].text()).toBe('C:\\')
    })

    it('merges bare drive letter D: into D:\\', () => {
      const wrapper = mountBreadcrumb({ path: 'D:\\Projects\\app' })
      const crumbs = wrapper.findAll('.crumb')
      expect(crumbs[1].text()).toBe('D:\\')
    })

    it('returns empty for empty path', () => {
      const wrapper = mountBreadcrumb({ path: '' })
      expect(wrapper.find('.dir-breadcrumb').exists()).toBe(false)
    })

    it('returns empty for dot path', () => {
      const wrapper = mountBreadcrumb({ path: '.' })
      expect(wrapper.find('.dir-breadcrumb').exists()).toBe(false)
    })

    it('marks last crumb as current', () => {
      const wrapper = mountBreadcrumb({ path: '/home/user' })
      const crumbs = wrapper.findAll('.crumb')
      // Last crumb should have .current class
      expect(crumbs[crumbs.length - 1].classes()).toContain('current')
    })

    it('does not navigate on last crumb click (current)', async () => {
      const wrapper = mountBreadcrumb({ path: '/home/user' })
      const crumbs = wrapper.findAll('.crumb')
      // Last crumb is "current" — clicking should not emit navigate
      await crumbs[crumbs.length - 1].trigger('click')
      // The template: i < parts.length - 1 condition prevents emission
      expect(wrapper.emitted('navigate')).toBeUndefined()
    })

    it('root crumb emits navigate with empty string', async () => {
      const wrapper = mountBreadcrumb({ path: '/home/user' })
      const crumbs = wrapper.findAll('.crumb')
      // First crumb is the root CircleDot icon — emits navigate('')
      await crumbs[0].trigger('click')
      const emitted = wrapper.emitted('navigate')
      expect(emitted).toBeTruthy()
      expect(emitted![0][0]).toBe('')
    })
  })

  // ── reconstructPath edge cases ──

  describe('reconstructPath edge cases', () => {
    it('handles single Unix root segment "/"', async () => {
      // Path "/" => splitPath("/") = ["", ""] => filter("") => []
      // No crumbs except root icon, so no non-root segment to click
      const wrapper = mountBreadcrumb({ path: '/' })
      expect(wrapper.find('.dir-breadcrumb').exists()).toBe(false)
    })

    it('handles single Windows drive root', async () => {
      // Path "C:\" => splitPath("C:\") = ["C:", ""] => filter empty => ["C:"]
      // parts merges "C:" => "C:\", so parts = ["C:\"]
      const wrapper = mountBreadcrumb({ path: 'C:\\' })
      const crumbs = wrapper.findAll('.crumb')
      // Only root icon + "C:\" (which is current/last, not clickable for navigate)
      expect(crumbs.length).toBe(2) // root + "C:\"
      expect(crumbs[1].text()).toBe('C:\\')
    })
  })
})
