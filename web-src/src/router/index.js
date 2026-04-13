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
    meta: { title: '经典视图' }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

export default router
