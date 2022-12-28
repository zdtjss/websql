import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'home',
      component: () => import('../views/SQLEditor2.vue')
    },
    {
      path: '/export',
      name: 'export',
      component: () => import('../views/DBExport.vue')
    }
  ]
})

export default router
