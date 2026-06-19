import { defineStore } from 'pinia'
import { ref } from 'vue'

const SIDEBAR_COLLAPSED_KEY = 'websql_sidebar_collapsed'

export const useUiStore = defineStore('ui', () => {
  const sidebarCollapsed = ref<boolean>(
    localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === 'true'
  )
  const globalLoading = ref<boolean>(false)
  const loginDialogVisible = ref<boolean>(false)

  function toggleSidebar() {
    sidebarCollapsed.value = !sidebarCollapsed.value
    localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(sidebarCollapsed.value))
  }

  function setSidebarCollapsed(collapsed: boolean) {
    sidebarCollapsed.value = collapsed
    localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(collapsed))
  }

  function setGlobalLoading(loading: boolean) {
    globalLoading.value = loading
  }

  function openLoginDialog() {
    loginDialogVisible.value = true
  }

  function closeLoginDialog() {
    loginDialogVisible.value = false
  }

  function toggleLoginDialog() {
    loginDialogVisible.value = !loginDialogVisible.value
  }

  return {
    sidebarCollapsed,
    globalLoading,
    loginDialogVisible,
    toggleSidebar,
    setSidebarCollapsed,
    setGlobalLoading,
    openLoginDialog,
    closeLoginDialog,
    toggleLoginDialog,
  }
})