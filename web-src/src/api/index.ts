import axios, { type AxiosInstance } from 'axios'
import { sanitizeError } from '@/utils/errorHandler'
import { ElMessage } from 'element-plus'

const env = import.meta.env

// 扩展 axios config，支持请求级别跳过全局错误弹窗（由调用方自行处理错误展示）
declare module 'axios' {
  interface AxiosRequestConfig {
    /** 为 true 时，code=500 业务错误不弹全局 ElMessage，由调用方 handleError 统一展示 */
    skipGlobalError?: boolean
  }
}

const http: AxiosInstance = axios.create({
  timeout: 1000 * 30 * 60
})

let sessionExpiredDispatched = false

function dispatchSessionExpired(detail: any) {
  if (sessionExpiredDispatched) {
    return
  }
  sessionExpiredDispatched = true
  window.dispatchEvent(new CustomEvent('session-expired', detail))
  setTimeout(() => {
    sessionExpiredDispatched = false
  }, 3000)
}

http.interceptors.request.use((config) => {
  config.url = env.VITE_API_URL + config.url
  config.headers['Authorization'] = sessionStorage.getItem('authentication') || ''
  return config
})

http.interceptors.response.use(
  (response) => {
    if (response.config.responseType === 'blob') {
      return response
    }
    const { code, msg } = response.data
    if (code === 401) {
      // 本地/桌面模式不触发登录过期
      if (sessionStorage.getItem("isRemote") !== "true") {
        return response
      }
      const isLoginExpired = !!sessionStorage.getItem('authentication')
      sessionStorage.removeItem('authentication')
      sessionStorage.removeItem('currentUser')
      // 部署模式事实（isRemote/isDesktop）不随会话清理，避免系统管理 Tab 消失或路由守卫放宽
      dispatchSessionExpired({
        detail: {
          message: isLoginExpired ? (msg || '登录已过期，请重新登录') : ''
        }
      })
      return Promise.reject(new Error(''))
    }
    // 业务层权限拒绝（HTTP 200 + code 403）：只提示，不清会话
    if (code === 403) {
      if (!response.config?.skipGlobalError) {
        ElMessage({ message: sanitizeError(msg) || '无权访问', type: 'warning' })
      }
      return Promise.reject(new Error(sanitizeError(msg) || '无权访问'))
    }
    if (code === 500) {
      // 调用方可通过 config.skipGlobalError 跳过全局弹窗，由调用方 handleError 统一展示
      if (!response.config?.skipGlobalError) {
        ElMessage({ message: sanitizeError(msg) || '系统错误', type: 'error' })
      }
      return Promise.reject(new Error(sanitizeError(msg) || '系统错误'))
    }
    return response
  },
  (error) => {
    if (axios.isCancel(error)) {
      return Promise.reject(error)
    }

    if (error.response) {
      const status = error.response.status

      if (status === 401) {
        // 本地/桌面模式不触发登录过期
        if (sessionStorage.getItem("isRemote") !== "true") {
          return Promise.reject(error)
        }
        const isLoginExpired = !!sessionStorage.getItem('authentication')
        const msg = error.response.data?.msg || '登录已过期，请重新登录'
        sessionStorage.removeItem('authentication')
        sessionStorage.removeItem('currentUser')
        // 部署模式事实不随会话清理
        dispatchSessionExpired({
          detail: {
            message: isLoginExpired ? msg : ''
          }
        })
        return Promise.reject(error)
      }

      if (status === 429) {
        ElMessage.warning('请求过于频繁，请稍后再试')
        return Promise.reject(error)
      }

      if (status === 503) {
        ElMessage.warning('服务暂时不可用，请稍后重试')
        return Promise.reject(error)
      }

      // 409：并发编辑冲突（如角色权限摘要不一致）。调用方可自行处理时跳过全局提示。
      if (status === 409) {
        const msg409 = error.response.data?.msg || '数据已被其他用户修改，请刷新后重试'
        if (!error.config?.skipGlobalError) {
          ElMessage.warning(sanitizeError(msg409))
        }
        return Promise.reject(error)
      }

      const rawMsg = error.response.data?.msg || error.response.statusText || '服务异常'
      if (!error.config?.skipGlobalError) {
        ElMessage.error(sanitizeError(rawMsg))
      }
    } else if (error.request) {
      ElMessage.error('网络异常，请检查网络连接')
    } else {
      ElMessage.error('请求失败，请稍后重试')
    }
    return Promise.reject(error)
  }
)

export default http
export { axios }
export function isCancel(error: any): boolean {
  return axios.isCancel(error)
}
