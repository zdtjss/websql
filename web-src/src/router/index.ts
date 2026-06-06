import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'AIChat',
    component: () => import('@/views/ai/ChatView.vue')
  },
  {
    path: '/ai',
    name: 'ChatExplicit',
    component: () => import('@/views/ai/ChatView.vue')
  },
  {
    path: '/system-management',
    name: 'SystemManagement',
    component: () => import('@/views/system/SystemManagement.vue'),
    meta: { title: '系统管理' }
  },
  {
    path: '/classical',
    name: 'ClassicalView',
    component: () => import('@/views/classical/ClassicalView.vue'),
    meta: { title: '经典视图', requiresClassicView: true }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

let cachedDefaultHomepage: string | null = null

export function resetDefaultHomepageCache() {
  cachedDefaultHomepage = null
}

async function getDefaultHomepage(): Promise<string> {
  if (cachedDefaultHomepage !== null) return cachedDefaultHomepage
  const stored = localStorage.getItem('defaultHomepage')
  if (stored) {
    cachedDefaultHomepage = stored
    return stored
  }
  try {
    const auth = sessionStorage.getItem('authentication') || ''
    const apiBase = import.meta.env.VITE_API_URL || ''
    const resp = await fetch(apiBase + '/system/config/all/get', {
      headers: { 'Authorization': auth }
    })
    if (resp.ok) {
      const data = await resp.json()
      const homepage = data.data?.defaultHomepage || 'ai'
      cachedDefaultHomepage = homepage
      localStorage.setItem('defaultHomepage', homepage)
      return homepage
    }
  } catch { /* ignore */ }
  cachedDefaultHomepage = 'ai'
  localStorage.setItem('defaultHomepage', 'ai')
  return 'ai'
}

async function checkClassicViewPermission(): Promise<boolean> {
  try {
    const auth = sessionStorage.getItem('authentication') || ''
    const apiBase = import.meta.env.VITE_API_URL || ''
    const resp = await fetch(apiBase + '/canUseClassicView', {
      headers: { 'Authorization': auth }
    })
    if (resp.ok) {
      const data = await resp.json()
      return !!(data.data && data.data.allowed)
    }
  } catch { /* ignore */ }
  return false
}

router.beforeEach(async (to, _from) => {
  if (to.meta.requiresClassicView) {
    const allowed = await checkClassicViewPermission()
    if (allowed) return true
    return '/'
  }

  if (to.path === '/') {
    const homepage = await getDefaultHomepage()
    if (homepage === 'classical') {
      const allowed = await checkClassicViewPermission()
      if (allowed) return '/classical'
    }
  }

  return true
})

export default router
