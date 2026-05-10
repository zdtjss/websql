import { ref } from 'vue'

const THEME_KEY = 'websql_theme'
type Theme = 'light' | 'dark'

const currentTheme = ref<Theme>((localStorage.getItem(THEME_KEY) as Theme) || 'light')

function applyTheme(theme: Theme) {
  document.documentElement.setAttribute('data-theme', theme)
  currentTheme.value = theme
  localStorage.setItem(THEME_KEY, theme)
}

function toggleTheme() {
  const next = currentTheme.value === 'light' ? 'dark' : 'light'
  applyTheme(next)
}

function initTheme() {
  applyTheme(currentTheme.value)
}

export function useTheme() {
  return {
    currentTheme,
    toggleTheme,
    initTheme,
    applyTheme,
  }
}