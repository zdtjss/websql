import { createApp } from 'vue'
import { createPinia } from 'pinia'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'

import App from './App.vue'
import router from './router'

import './assets/main.css'
import './styles/db-tools.css'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(router)

// 应用挂载前的静默登录引导：在任意组件初始化之前把系统模式与本地会话写入 sessionStorage，
// 彻底消除此前各视图 onMounted 异步 getSysModel 带来的时序竞态。
// - 本地/桌面模式：写入 localToken 与 local 用户，组件 setup 阶段即为已登录态
// - 远程模式：仅写入 isRemote/isDesktop，登录仍由各视图弹框处理
// 任何异常都静默降级，不阻塞挂载（各视图的 getSysModel 作为兜底）。
async function bootstrap() {
  try {
    const apiBase = import.meta.env.VITE_API_URL || ''
    const resp = await fetch(apiBase + '/sysMode')
    if (!resp.ok) return
    const json = await resp.json()
    const data = json?.data ?? json ?? {}
    const isRemote = !!data.isRemote
    const isDesktop = !!data.isDesktop
    sessionStorage.setItem('isRemote', String(isRemote))
    sessionStorage.setItem('isDesktop', String(isDesktop))
    // isDesktop 为权威判据：桌面模式即使 isRemote 误为 true 也静默登录
    if ((isDesktop || !isRemote) && data.localToken) {
      sessionStorage.setItem('authentication', data.localToken)
      sessionStorage.setItem('currentUser', JSON.stringify({ id: 'local', name: 'local', isAdmin: true }))
    }
  } catch {
    // 静默降级：交由各视图 getSysModel 兜底
  }
}

bootstrap().finally(() => {
  app.mount('#app')
  const splash = document.getElementById('boot-splash')
  if (splash) splash.remove()
})
