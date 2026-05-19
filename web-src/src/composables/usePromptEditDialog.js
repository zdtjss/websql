import { ref } from 'vue'

const visible = ref(false)
const promptId = ref('')
const roleId = ref('')
const savedCallbacks = []
let sendToAIHandler = null

export function usePromptEditDialog() {
  function openDialog(options = {}) {
    promptId.value = options.promptId || ''
    roleId.value = options.roleId || ''
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
    openDialog,
    closeDialog,
    onSaved,
    offSaved,
    triggerSaved,
    setSendToAIHandler,
    handleSendToAI,
  }
}
