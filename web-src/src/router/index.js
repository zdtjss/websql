import { createRouter, createWebHistory } from 'vue-router'
import App from '@/App.vue'

const routes = [
  {
    path: '/',
    name: 'App',
    component: App
  },
  {
    path: '/system-management',
    name: 'SystemManagement',
    component: () => import('@/views/SystemManagement.vue'),
    meta: { title: '系统管理' }
  },
  {
    path: '/classical',
    name: 'ClassicalView',
    component: () => import('@/views/ClassicalView.vue'),
    meta: { title: '经典视图', requiresClassicView: true }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach(async (to, from, next) => {
  if (to.meta.requiresClassicView) {
    try {
      const auth = sessionStorage.getItem('authentication') || ''
      const apiBase = import.meta.env.VITE_API_URL || ''
      const resp = await fetch(apiBase + '/canUseClassicView', {
        headers: { 'Authorization': auth }
      })
      if (resp.ok) {
        const data = await resp.json()
        if (data.data && data.data.allowed) {
          next()
        } else {
          next('/')
        }
      } else {
        next('/')
      }
    } catch {
      next('/')
    }
  } else {
    next()
  }
})

export default router
