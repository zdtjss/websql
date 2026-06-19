import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { type AxiosHeaders } from 'axios'
import { ElMessage } from 'element-plus'
import {
  loginByPassword,
  loginByToken as loginByTokenApi,
  logout as logoutApi,
} from '@/api/auth'

export interface CurrentUser {
  id: string
  name: string
  isAdmin: boolean
}

export const useAuthStore = defineStore('auth', () => {
  // State
  const token = ref<string>(sessionStorage.getItem('authentication') || '')
  const currentUser = ref<CurrentUser | null>(parseCurrentUser())
  const isRemote = ref<boolean>(sessionStorage.getItem('isRemote') === 'true')

  // Getters
  const isLoggedIn = computed(() => !!token.value)
  const isAdmin = computed(() => currentUser.value?.isAdmin ?? false)

  // Actions
  function parseCurrentUser(): CurrentUser | null {
    try {
      const stored = sessionStorage.getItem('currentUser')
      return stored ? JSON.parse(stored) : null
    } catch {
      return null
    }
  }

  function setToken(t: string) {
    token.value = t
    sessionStorage.setItem('authentication', t)
  }

  function setUser(user: CurrentUser) {
    currentUser.value = user
    sessionStorage.setItem('currentUser', JSON.stringify(user))
  }

  function setRemote(remote: boolean) {
    isRemote.value = remote
    sessionStorage.setItem('isRemote', String(remote))
  }

  function clearAuth() {
    token.value = ''
    currentUser.value = null
    sessionStorage.removeItem('authentication')
    sessionStorage.removeItem('currentUser')
    sessionStorage.removeItem('isRemote')
  }

  async function login(loginName: string, password: string): Promise<boolean> {
    try {
      const resp = await loginByPassword({ name: loginName, password })
      if (resp.data.code !== 200) {
        ElMessage.error(resp.data.msg || '登录失败')
        return false
      }
      // 密码登录的 token 从响应头 authentication 获取
      setToken((resp.headers as AxiosHeaders).get('authentication') as string)
      setUser(resp.data.data)
      return true
    } catch {
      return false
    }
  }

  async function loginByToken(token: string): Promise<boolean> {
    try {
      const resp = await loginByTokenApi(token)
      if (resp.data.code !== 200) {
        ElMessage.error(resp.data.msg || '登录失败')
        return false
      }
      // token 登录的 authentication 字段在响应体 data 中
      setToken(resp.data.data.authentication)
      setUser(resp.data.data)
      return true
    } catch {
      return false
    }
  }

  async function logout(): Promise<void> {
    const resp = await logoutApi()
    ElMessage(resp.data.data)
    clearAuth()
  }

  async function refreshUser(): Promise<void> {
    currentUser.value = parseCurrentUser()
  }

  return {
    token, currentUser, isRemote,
    isLoggedIn, isAdmin,
    setToken, setUser, setRemote, clearAuth,
    login, loginByToken, logout, refreshUser,
  }
})
