import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  {
    path: '/',
    name: 'App',
    component: () => import('@/App.vue')
  },
  {
    path: '/system-management',
    name: 'SystemManagement',
    component: () => import('@/views/SystemManagement.vue'),
    meta: { title: '系统管理' }
  },
  {
    path: '/role-permission',
    name: 'RolePermission',
    component: () => import('@/views/RolePermission.vue'),
    meta: { title: '角色权限管理' }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

export default router
