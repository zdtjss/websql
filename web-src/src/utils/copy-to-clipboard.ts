/**
 * 复制文本到剪贴板工具
 *
 * 优先使用现代 Clipboard API（navigator.clipboard.writeText），
 * 在不安全上下文或 API 不可用时，回退到 execCommand('copy') 方案。
 *
 * 兼容说明：为保持与既有调用方一致，保留 onSucc/onError 回调参数（可选）；
 * 同时返回 Promise<boolean>，便于以链式方式判断复制是否成功。
 */

/**
 * 复制文本到剪贴板
 * @param text 待复制的文本
 * @param onSucc 复制成功回调（可选）
 * @param onError 复制失败回调（可选）
 * @returns 是否复制成功
 */
export default function copyToClipboard(
  text: string,
  onSucc: () => void = () => {},
  onError: () => void = () => {}
): Promise<boolean> {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard
      .writeText(text)
      .then(() => {
        onSucc()
        return true
      })
      .catch(() => copyToClipboardFallback(text, onSucc, onError))
  }
  return copyToClipboardFallback(text, onSucc, onError)
}

/**
 * 回退方案：通过隐藏 textarea + execCommand('copy') 复制文本
 * @param text 待复制的文本
 * @param onSucc 复制成功回调
 * @param onError 复制失败回调
 * @returns 是否复制成功
 */
function copyToClipboardFallback(
  text: string,
  onSucc: () => void,
  onError: () => void
): Promise<boolean> {
  // 创建 text area
  const textArea = document.createElement('textarea')
  textArea.value = text
  // 使 text area 不在 viewport，同时设置不可见
  document.body.appendChild(textArea)
  textArea.focus()
  textArea.select()
  return new Promise<boolean>((resolve) => {
    // 执行复制命令并移除文本框
    const ok = document.execCommand('copy')
    textArea.remove()
    if (ok) {
      onSucc()
      resolve(true)
    } else {
      onError()
      resolve(false)
    }
  })
}
