import DOMPurify from 'dompurify'
import type { Config } from 'dompurify'

const SANITIZE_CONFIG: Config = {
  ADD_ATTR: ['target', 'rel', 'data-export-link', 'class', 'id'],
  ADD_TAGS: ['iframe'],
  FORBID_TAGS: ['script', 'style'],
  FORBID_ATTR: ['onerror', 'onload', 'onclick', 'onmouseover', 'onmouseout', 'onfocus', 'onblur', 'onchange', 'onsubmit', 'onkeydown', 'onkeyup', 'onkeypress'],
  ALLOW_DATA_ATTR: true,
}

export function sanitizeHtml(html: string): string {
  if (!html) return ''
  return DOMPurify.sanitize(html, SANITIZE_CONFIG) as string
}
