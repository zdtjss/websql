/**
 * 编辑器命中判定：事件目标是否位于代码编辑器内部。
 *
 * 背景（计划 3.1）：`PageFindBar` 在 capture 阶段全局接管 Ctrl+F，
 * 导致编辑器内无法使用编辑器自带的查找/替换。现在编辑器内的事件交还给 CodeMirror。
 * 该判定被抽成独立函数，便于单测覆盖「祖先节点」逻辑（不依赖真实 DOM）。
 */
export function isInsideCodeEditor(target: EventTarget | null): boolean {
  const el = target as unknown as { closest?: (selector: string) => unknown } | null
  if (!el || typeof el.closest !== 'function') return false
  // CodeMirror 6 根节点带 .cm-editor；本项目编辑器容器另有 .codemirror 类
  return !!el.closest('.cm-editor, .codemirror')
}
