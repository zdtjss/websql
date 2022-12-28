import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      component: () => import('../views/SQLEditor2.vue')
    },
    {
      path: '/export',
      component: () => import('../views/DBExport.vue'),
    }
  ]
})

export default router
