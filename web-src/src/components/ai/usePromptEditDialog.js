import { ref } from 'vue'

const visible = ref(false)
const promptId = ref('')
const roleId = ref('')
const initialTitle = ref('')
const initialContent = ref('')
const initialConnSchemas = ref([])
const initialTables = ref([])
const savedCallbacks = []
let sendToAIHandler = null

export function usePromptEditDialog() {
  function openDialog(options = {}) {
    promptId.value = options.promptId || ''
    roleId.value = options.roleId || ''
    // 提取场景：把 AI 生成的结果预填进新增提示词弹窗，由用户确认后保存
    initialTitle.value = options.initialTitle || ''
    initialContent.value = options.initialContent || ''
    // 提取场景：会话中实际使用的连接/Schema，打开弹窗时预填
    initialConnSchemas.value = options.initialConnSchemas || []
    initialTables.value = options.initialTables || []
    visible.value = true
  }

  function closeDialog() {
    visible.value = false
  }

  function onSaved(callback) {
    if (!savedCallbacks.includes(callback)) {
      savedCallbacks.push(callback)
    }
  }

  function offSaved(callback) {
    const idx = savedCallbacks.indexOf(callback)
    if (idx > -1) savedCallbacks.splice(idx, 1)
  }

  function triggerSaved() {
    for (const cb of savedCallbacks) cb()
  }

  function setSendToAIHandler(handler) {
    sendToAIHandler = handler
  }

  function handleSendToAI(content, connInfo) {
    if (sendToAIHandler) {
      sendToAIHandler(content, connInfo)
    }
  }

  return {
    visible,
    promptId,
    roleId,
    initialTitle,
    initialContent,
    initialConnSchemas,
    initialTables,
    openDialog,
    closeDialog,
    onSaved,
    offSaved,
    triggerSaved,
    setSendToAIHandler,
    handleSendToAI,
  }
}
