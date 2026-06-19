import { ref, type Ref } from 'vue'
import { ElMessage } from 'element-plus'
import { client, server } from '@passwordless-id/webauthn'
import { type AxiosHeaders } from 'axios'
import {
  loginByPassword,
  loginByToken as loginByTokenApi,
  loginByBio as loginByBioApi,
  type CurrentUser,
} from '@/api/auth'
import { useAuthStore } from '@/stores/auth'

const bioLocalStorageKey = 'nway_websql_bio_credential_id'

export interface UseLoginResult {
  login: (name: string, password: string) => Promise<CurrentUser | null>
  loginByToken: (token: string) => Promise<CurrentUser | null>
  loginByBio: () => Promise<CurrentUser | null>
  loading: Ref<boolean>
  error: Ref<string | null>
}

/**
 * 集中处理登录逻辑的组合式函数。
 * - 密码登录：token 从响应头 authentication 获取
 * - Token 登录：authentication 字段在响应体 data 中
 * - 生物识别登录：依赖 @passwordless-id/webauthn，token 从响应头 authentication 获取
 *
 * 登录成功后会通过 auth store 写入 sessionStorage 并更新状态，
 * 调用方拿到非 null 返回值即代表登录成功。
 */
export function useLogin(): UseLoginResult {
  const authStore = useAuthStore()
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function login(name: string, password: string): Promise<CurrentUser | null> {
    loading.value = true
    error.value = null
    try {
      const resp = await loginByPassword({ name, password })
      if (resp.data.code !== 200) {
        error.value = resp.data.msg || '登录失败'
        ElMessage.error(error.value)
        return null
      }
      const token = (resp.headers as AxiosHeaders).get('authentication') as string
      authStore.setToken(token)
      authStore.setUser(resp.data.data)
      return resp.data.data
    } catch {
      error.value = '登录失败'
      return null
    } finally {
      loading.value = false
    }
  }

  async function loginByToken(token: string): Promise<CurrentUser | null> {
    loading.value = true
    error.value = null
    try {
      const resp = await loginByTokenApi(token)
      if (resp.data.code !== 200) {
        error.value = resp.data.msg || '登录失败'
        ElMessage.error(error.value)
        return null
      }
      const data = resp.data.data
      authStore.setToken(data.authentication)
      authStore.setUser(data)
      return data
    } catch {
      error.value = '登录失败'
      ElMessage.error('登录失败')
      return null
    } finally {
      loading.value = false
    }
  }

  async function loginByBio(): Promise<CurrentUser | null> {
    loading.value = true
    error.value = null
    try {
      const credential = window.localStorage.getItem(bioLocalStorageKey)
      const authentication = await client.authenticate({
        allowCredentials: credential == null ? [] : [JSON.parse(credential)],
        challenge: server.randomChallenge(),
      })
      const resp = await loginByBioApi(authentication.id)
      if (resp.data.code !== 200) {
        error.value = resp.data.msg || '登录失败'
        ElMessage.error(error.value)
        return null
      }
      const token = (resp.headers as AxiosHeaders).get('authentication') as string
      authStore.setToken(token)
      authStore.setUser(resp.data.data)
      return resp.data.data
    } catch {
      error.value = '登录失败'
      ElMessage.error('登录失败')
      return null
    } finally {
      loading.value = false
    }
  }

  return {
    login,
    loginByToken,
    loginByBio,
    loading,
    error,
  }
}
