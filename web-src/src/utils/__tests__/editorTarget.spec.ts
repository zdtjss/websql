import { describe, expect, it } from 'vitest'
import { isInsideCodeEditor } from '../editorTarget'

// 用最小的假元素模拟 closest(selector) 的祖先查找：
// 只要能命中 .cm-editor 或 .codemirror 就视为在编辑器内。
function fakeElement(hit: boolean) {
  return {
    closest: (selector: string) => (hit && /\.cm-editor|\.codemirror/.test(selector) ? {} : null),
  }
}

describe('isInsideCodeEditor（PageFindBar 的 ancestor 判定）', () => {
  it('事件目标在编辑器内 → true，Ctrl+F 应交还 CodeMirror', () => {
    expect(isInsideCodeEditor(fakeElement(true) as unknown as EventTarget)).toBe(true)
  })

  it('事件目标不在编辑器内 → false，Ctrl+F 仍走页内查找', () => {
    expect(isInsideCodeEditor(fakeElement(false) as unknown as EventTarget)).toBe(false)
  })

  it('null / 无 closest 的对象按“不在编辑器内”处理，不得抛错', () => {
    expect(isInsideCodeEditor(null)).toBe(false)
    expect(isInsideCodeEditor({} as EventTarget)).toBe(false)
    expect(isInsideCodeEditor('plain-string' as unknown as EventTarget)).toBe(false)
  })

  it('选择器需同时覆盖 .cm-editor 与 .codemirror 两个根节点', () => {
    // 模拟只命中 .codemirror（本项目容器类名）的情况
    const onlyContainer = { closest: (s: string) => (s.includes('.codemirror') ? {} : null) }
    expect(isInsideCodeEditor(onlyContainer as unknown as EventTarget)).toBe(true)
  })
})
