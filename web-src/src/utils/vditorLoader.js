/**
 * Vditor 懒加载器
 * 将 Vditor 的加载逻辑抽离为独立模块，支持：
 * 1. 动态 import 按需加载（不打入主 chunk）
 * 2. 预加载（idle 时提前下载，打开编辑器时零等待）
 * 3. CSS 延迟注入
 */

let vditorCssLoaded = false
let VditorClass = null
let vditorModulePromise = null

/**
 * 预加载 Vditor 模块（可在 requestIdleCallback 中调用）
 * 多次调用安全，只会触发一次实际加载
 */
export function preloadVditor() {
  ensureVditorCss()
  if (!vditorModulePromise) {
    vditorModulePromise = import('vditor').then(m => {
      VditorClass = m.default || m
      return VditorClass
    })
  }
  return vditorModulePromise
}

/**
 * 获取 Vditor 构造函数（如果已预加载则立即返回）
 */
export async function loadVditorModule() {
  if (VditorClass) return VditorClass
  VditorClass = await preloadVditor()
  return VditorClass
}

/**
 * 确保 Vditor CSS 已注入（仅注入一次）
 */
export function ensureVditorCss() {
  if (vditorCssLoaded) return
  vditorCssLoaded = true
  import('vditor/dist/index.css')
}
