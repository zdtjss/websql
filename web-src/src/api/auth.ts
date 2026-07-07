import { api } from './adapter'
import type { AxiosResponse } from 'axios'

/** 通用接口响应结构（从 adapter 重新导出，保持向后兼容） */
export type { ApiResponse } from './adapter'

/** 当前登录用户信息 */
export interface CurrentUser {
  id: string
  name: string
  isAdmin: boolean
}

/** 登录类型 */
export type LoginType = 'pwd' | 'token' | 'bio'

/** 密码登录参数 */
export interface LoginByPasswordParams {
  name: string
  password: string
}

/** 系统模式信息 */
export interface SysModeInfo {
  isRemote: boolean
}

/** 权限检查结果 */
export interface PermissionResult {
  allowed: boolean
}

/** 登录响应（密码/生物识别）：token 在响应头 authentication 中 */
export interface LoginResponse {
  user: CurrentUser
  authentication: string
}

/** Token 登录响应：authentication 字段在响应体 data 中 */
export interface TokenLoginResponse {
  user: CurrentUser
  authentication: string
}

/**
 * 密码登录
 * 对应 POST /login，Content-Type: application/x-www-form-urlencoded
 * token 从响应头 authentication 获取
 */
export function loginByPassword(params: LoginByPasswordParams): Promise<AxiosResponse<ApiResponse<CurrentUser>>> {
  const body = new URLSearchParams()
  body.append('name', params.name)
  body.append('password', params.password)
  body.append('loginType', 'pwd')
  return api.request<CurrentUser>({
    method: 'POST',
    url: '/login',
    body,
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
  })
}

/**
 * Token 登录
 * 对应 POST /login，token 从响应体 data.authentication 获取
 */
export function loginByToken(token: string): Promise<AxiosResponse<ApiResponse<CurrentUser & { authentication: string }>>> {
  const body = new URLSearchParams()
  body.append('key', token)
  body.append('loginType', 'token')
  return api.request<CurrentUser & { authentication: string }>({
    method: 'POST',
    url: '/login',
    body,
  })
}

/**
 * 生物识别登录
 * 对应 POST /login，token 从响应头 authentication 获取
 */
export function loginByBio(key: string): Promise<AxiosResponse<ApiResponse<CurrentUser>>> {
  const body = new URLSearchParams()
  body.append('key', key)
  body.append('loginType', 'bio')
  return api.request<CurrentUser>({
    method: 'POST',
    url: '/login',
    body,
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
  })
}

/** 登出，对应 POST /logout */
export function logout(): Promise<AxiosResponse<ApiResponse<string>>> {
  return api.request<string>({ method: 'POST', url: '/logout' })
}

/**
 * 修改密码
 * 对应 POST /changePassword，使用 URLSearchParams 传参
 */
export function changePassword(oldPassword: string, newPassword: string): Promise<AxiosResponse<ApiResponse<unknown>>> {
  const body = new URLSearchParams()
  body.append('oldPassword', oldPassword)
  body.append('newPassword', newPassword)
  return api.request({ method: 'POST', url: '/changePassword', body })
}

/**
 * 保存用户生物识别凭证
 * 对应 POST /saveUserBio，使用 URLSearchParams 传参
 */
export function saveUserBio(bioKey: string): Promise<AxiosResponse<ApiResponse<unknown>>> {
  const body = new URLSearchParams()
  body.append('bioKey', bioKey)
  return api.request({ method: 'POST', url: '/saveUserBio', body })
}

/** 获取系统模式（是否远程模式），对应 GET /sysMode */
export function getSysMode(): Promise<AxiosResponse<unknown>> {
  return api.request({ method: 'GET', url: '/sysMode' })
}

/** 检查是否可使用经典视图，对应 GET /canUseClassicView */
export function canUseClassicView(): Promise<AxiosResponse<ApiResponse<PermissionResult>>> {
  return api.request<PermissionResult>({ method: 'GET', url: '/canUseClassicView' })
}

/** 检查是否可修改数据，对应 GET /canModifyData */
export function canModifyData(): Promise<AxiosResponse<ApiResponse<PermissionResult>>> {
  return api.request<PermissionResult>({ method: 'GET', url: '/canModifyData' })
}
