import DOMPurify from 'dompurify'
import type { Config } from 'dompurify'

const SANITIZE_CONFIG: Config = {
  ADD_ATTR: ['target', 'rel', 'data-export-link', 'class', 'id'],
  // 注意：不放开 iframe（DOMPurify 默认白名单本身不含 iframe）。
  // iframe 会显著放宽 XSS 攻击面（可加载任意外部页面 / 触发同源脚本执行），
  // 若后续产品确需内嵌页面，应改为独立的受控预览组件 + 域名白名单，
  // 而不是在此处放宽全局净化规则。
  FORBID_TAGS: ['script', 'style', 'iframe'],
  FORBID_ATTR: ['onerror', 'onload', 'onclick', 'onmouseover', 'onmouseout', 'onfocus', 'onblur', 'onchange', 'onsubmit', 'onkeydown', 'onkeyup', 'onkeypress'],
  ALLOW_DATA_ATTR: true,
}

export function sanitizeHtml(html: string): string {
  if (!html) return ''
  return DOMPurify.sanitize(html, SANITIZE_CONFIG) as string
}
