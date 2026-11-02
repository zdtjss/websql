/**
 * Vditor 懒加载器
 * 将 Vditor 的加载逻辑抽离为独立模块，支持：
 * 1. 动态 import 按需加载（不打入主 chunk）
 * 2. 预加载（idle 时提前下载，打开编辑器时零等待）
 * 3. CSS 延迟注入
 */

/** Vditor 构造函数类型（通过动态 import 的 default 导出推导，避免静态引入影响分包） */
type VditorConstructor = typeof import('vditor').default

let vditorCssLoaded = false
let VditorClass: VditorConstructor | null = null
let vditorModulePromise: Promise<VditorConstructor> | null = null
// Vditor 默认从 unpkg 拉取约 4MB 的 Lute 渲染引擎，内网首次打开会明显卡顿，
// 这里改用随前端一起构建的本地资源（由 Vite 以 ?url 方式输出到 /assets）。
let vditorLutePath = ''
// 语言包同样本地加载，避免 CDN 不可用时编辑器无法初始化
let vditorI18n: Record<string, string> | null = null

/**
 * 获取本地 Lute 引擎地址（需在 loadVditorModule 完成后调用）
 */
export function getVditorLutePath(): string {
  return vditorLutePath
}

/**
 * 获取本地语言包（需在 loadVditorModule 完成后调用，未加载成功时返回 null）
 */
export function getVditorI18n(): Record<string, string> | null {
  return vditorI18n
}

/**
 * 加载 Vditor 运行时附属脚本（图标 sprite / 语言包）。
 * 加载失败时静默放行，让 Vditor 自行回退到默认 CDN 地址。
 */
function loadScriptOnce(path: string, id: string): Promise<void> {
  return new Promise((resolve) => {
    if (typeof document === 'undefined' || document.getElementById(id)) {
      resolve()
      return
    }
    const script = document.createElement('script')
    script.src = path
    script.async = true
    script.onload = () => { script.id = id; resolve() }
    script.onerror = () => resolve()
    document.head.appendChild(script)
  })
}

/**
 * 预加载 Vditor 模块（可在 requestIdleCallback 中调用）
 * 多次调用安全，只会触发一次实际加载
 */
export function preloadVditor(): Promise<VditorConstructor> {
  ensureVditorCss()
  if (!vditorModulePromise) {
    vditorModulePromise = Promise.all([
      import('vditor'),
      import('vditor/dist/js/lute/lute.min.js?url'),
      // 语言包体积很小，直接随 vditor 分包执行（脚本内会赋值 window.VditorI18n）
      import('vditor/dist/js/i18n/zh_CN.js'),
      import('vditor/dist/js/icons/ant.js?url'),
    ]).then(async ([m, luteModule, , iconModule]) => {
      VditorClass = (m.default || m) as VditorConstructor
      vditorLutePath = luteModule.default
      // 图标脚本沿用 Vditor 自身的 id，加载完成后编辑器初始化时不会再走同步 XHR 拉 CDN
      await loadScriptOnce(iconModule.default, 'vditorIconScript')
      vditorI18n = (window as any).VditorI18n || null
      return VditorClass
    })
  }
  return vditorModulePromise
}

/**
 * 获取 Vditor 构造函数（如果已预加载则立即返回）
 */
export async function loadVditorModule(): Promise<VditorConstructor> {
  if (VditorClass) return VditorClass
  VditorClass = await preloadVditor()
  return VditorClass
}

/**
 * 浏览器空闲时预加载，避免用户打开编辑器时才下载模块
 */
export function preloadVditorWhenIdle(): void {
  const run = () => { void preloadVditor() }
  if (typeof window === 'undefined') return
  if (typeof window.requestIdleCallback === 'function') {
    window.requestIdleCallback(run, { timeout: 3000 })
  } else {
    window.setTimeout(run, 800)
  }
}

/**
 * 确保 Vditor CSS 已注入（仅注入一次）
 */
export function ensureVditorCss(): void {
  if (vditorCssLoaded) return
  vditorCssLoaded = true
  void import('vditor/dist/index.css')
}
