import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

const THEME_KEY = 'websql_theme'
type Theme = 'light' | 'dark'

export const useThemeStore = defineStore('theme', () => {
  // State
  const isDark = ref<boolean>(localStorage.getItem(THEME_KEY) === 'dark')

  // Getter
  const currentTheme = computed<Theme>(() => (isDark.value ? 'dark' : 'light'))

  // Actions
  function applyTheme(theme: Theme): void {
    const dark = theme === 'dark'
    document.documentElement.setAttribute('data-theme', theme)
    if (dark) {
      document.documentElement.classList.add('dark')
    } else {
      document.documentElement.classList.remove('dark')
    }
    isDark.value = dark
    localStorage.setItem(THEME_KEY, theme)
  }

  function setDark(value: boolean): void {
    applyTheme(value ? 'dark' : 'light')
  }

  function toggleTheme(): void {
    setDark(!isDark.value)
  }

  function initTheme(): void {
    applyTheme(currentTheme.value)
  }

  return {
    isDark,
    currentTheme,
    toggleTheme,
    setDark,
    initTheme,
  }
})
