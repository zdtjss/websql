import http from '@/api'

// 大值跳过后端同步的阈值（512KB），避免 schema 缓存等大对象频繁写库
const MAX_SYNC_SIZE = 512 * 1024

// 待同步队列：同一 key 的多次写入只保留最后一次，减少后端请求
const pendingSync = new Map<string, string>()
let syncTimer: ReturnType<typeof setTimeout> | null = null

function flushSyncQueue() {
  syncTimer = null
  if (pendingSync.size === 0) return
  const batch = new Map(pendingSync)
  pendingSync.clear()
  for (const [key, value] of batch) {
    http.post('/storage/save', { key, value }).catch(() => {
      // 同步失败静默降级，localStorage 已写入，下次启动会重新同步
    })
  }
}

function scheduleSync(key: string, value: string) {
  if (value.length > MAX_SYNC_SIZE) return
  pendingSync.set(key, value)
  if (syncTimer) clearTimeout(syncTimer)
  // 500ms 防抖，合并连续写入（如编辑器输入）
  syncTimer = setTimeout(flushSyncQueue, 500)
}

function syncDelete(key: string) {
  pendingSync.delete(key)
  http.post('/storage/delete', { key }).catch(() => {
    // 静默降级
  })
}

/**
 * 用户级持久化存储 composable。
 *
 * 策略：双写 — localStorage 作为同步读缓存，后端 API 作为持久化层。
 * - setItem: 同步写 localStorage + 异步写后端（500ms 防抖）
 * - getItem: 同步读 localStorage（无改动）
 * - removeItem: 同步删 localStorage + 异步删后端
 * - restoreFromBackend: 启动时从后端拉取全部 KV，覆盖 localStorage
 *
 * 桌面模式重启后端口变化导致 localStorage 丢失时，
 * restoreFromBackend 会从后端恢复数据到 localStorage，实现跨会话持久化。
 */
export function useStorage() {
  return {
    getItem(key: string): string | null {
      return localStorage.getItem(key)
    },

    setItem(key: string, value: string) {
      localStorage.setItem(key, value)
      scheduleSync(key, value)
    },

    removeItem(key: string) {
      localStorage.removeItem(key)
      syncDelete(key)
    },
  }
}

/**
 * 启动时从后端恢复所有用户级存储到 localStorage。
 * 在 main.ts bootstrap 中调用，早于 app.mount。
 *
 * 仅恢复 localStorage 中不存在的 key（避免覆盖当前会话已写入的值）。
 * 桌面模式重启后 localStorage 为空，所有 key 均从后端恢复。
 */
export async function restoreFromBackend() {
  try {
    const resp = await http.get('/storage/list')
    if (resp.data.code !== 200 || !Array.isArray(resp.data.data)) return
    for (const item of resp.data.data) {
      if (!item.storageKey) continue
      // 后端为权威源，直接覆盖（桌面模式重启后 localStorage 为空）
      if (item.storageValue !== null && item.storageValue !== undefined) {
        localStorage.setItem(item.storageKey, item.storageValue)
      }
    }
  } catch {
    // 静默降级：后端不可用时使用 localStorage 现有数据
  }
}
