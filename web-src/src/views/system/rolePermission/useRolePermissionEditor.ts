/**
 * 角色权限编辑状态机（唯一 baseline / working / dirty / saving 来源）。
 *
 * 设计要点：
 * - baseline 是服务端加载的权限快照；working 是用户编辑中的权限集合；
 * - 显式取消的 key 记录在 explicitlyUnchecked 中，用于区分「从未拥有」与「刚被取消」；
 * - 保存成功后用服务端返回的新 digest 刷新 baseline；409 冲突只标记状态，绝不自动覆盖重试。
 */

import { reactive } from 'vue'
import {
  type PowerLevel,
  type RolePower,
  buildDiff,
  computeImplicitKeys,
  displayCheckedKeys,
  isDescendantOf,
  keyLevel,
  parseKey,
  permToKey,
} from './permissionPolicy'

/** 角色列表项（与 /roleList 对齐）。 */
export interface EditorRole {
  id: string
  name: string
  viewClassic: number
  allowModify: number
  aiAllowModify?: number
  permissionDigest?: string
  powerList?: RolePower[]
}

/** /saveRole 请求体。 */
export interface SaveRoleParams {
  id: string
  name: string
  addPowers: RolePower[]
  delPowers: RolePower[]
  viewClassic: number
  allowModify: number
  aiAllowModify?: number
  expectedDigest?: string
}

export interface SaveOptions {
  viewClassic: number
  allowModify: number
  aiAllowModify?: number
  force?: boolean
}

function cloneMap(source: Map<string, RolePower>): Map<string, RolePower> {
  return new Map(source)
}

export function useRolePermissionEditor() {
  const state = reactive({
    baseline: new Map<string, RolePower>(),
    working: new Map<string, RolePower>(),
    baselineDigest: '',
    explicitlyUnchecked: new Set<string>(),
    saving: false,
    conflict: false,
  })

  /** 载入角色：baseline/working 都来自服务端快照。 */
  function load(role: EditorRole) {
    const map = new Map<string, RolePower>()
    for (const power of role.powerList || []) {
      map.set(permToKey(power), { ...power })
    }
    state.baseline = map
    state.working = cloneMap(map)
    state.baselineDigest = role.permissionDigest || ''
    state.explicitlyUnchecked.clear()
    state.conflict = false
  }

  /** 取消编辑：回到 baseline。 */
  function reset() {
    state.working = cloneMap(state.baseline)
    state.explicitlyUnchecked.clear()
    state.conflict = false
  }

  function isWorking(key: string): boolean {
    return state.working.has(key)
  }

  function isImplicit(key: string, treeKeys: Set<string>): boolean {
    return computeImplicitKeys(state.working.keys(), treeKeys).has(key)
  }

  /** 树展示的勾选 key（working + 隐式父级）。 */
  function checkedKeysFor(treeKeys: Set<string>): string[] {
    return displayCheckedKeys(state.working.keys(), treeKeys)
  }

  /** 勾选/取消单个节点；取消时级联删除全部下级。 */
  function toggle(key: string, checked: boolean) {
    if (key.startsWith('dir::')) return
    if (checked) {
      state.working.set(key, parseKey(key))
      state.explicitlyUnchecked.delete(key)
      return
    }
    for (const existing of [...state.working.keys()]) {
      if (existing === key || isDescendantOf(existing, key)) {
        state.working.delete(existing)
      }
    }
    state.explicitlyUnchecked.add(key)
  }

  /** 批量勾选（目录节点全选/全不选）。 */
  function toggleMany(keys: string[], checked: boolean) {
    for (const key of keys) toggle(key, checked)
  }

  function hasPermissionChanges(): boolean {
    const { addPowers, delPowers } = diff()
    return addPowers.length > 0 || delPowers.length > 0
  }

  function diff(): { addPowers: RolePower[]; delPowers: RolePower[] } {
    return buildDiff(
      [...state.baseline.values()],
      [...state.working.values()],
    )
  }

  /** 当前 working 中指定层级的 key。 */
  function keysOfLevel(level: PowerLevel): string[] {
    return [...state.working.keys()].filter((key) => keyLevel(key) === level)
  }

  /**
   * 生成保存请求。无变更且能力开关未变时返回 null（前端不发空请求）。
   */
  function buildPayload(role: EditorRole, options: SaveOptions): SaveRoleParams | null {
    const { addPowers, delPowers } = diff()
    const capabilitiesChanged =
      role.viewClassic !== options.viewClassic ||
      role.allowModify !== options.allowModify ||
      (options.aiAllowModify !== undefined && (role.aiAllowModify ?? 0) !== options.aiAllowModify)
    if (addPowers.length === 0 && delPowers.length === 0 && !capabilitiesChanged && !options.force) {
      return null
    }
    return {
      id: role.id,
      name: role.name,
      addPowers: addPowers.map((p) => ({ ...p, id: '', roleId: role.id })),
      delPowers: delPowers.map((p) => ({ ...p, id: '', roleId: role.id })),
      viewClassic: options.viewClassic ? 1 : 0,
      allowModify: options.allowModify ? 1 : 0,
      aiAllowModify: options.aiAllowModify,
      expectedDigest: state.baselineDigest || undefined,
    }
  }

  /** 保存成功：以 working 作为新 baseline，digest 由服务端返回。 */
  function markSaved(newDigest?: string) {
    state.baseline = cloneMap(state.working)
    if (newDigest) state.baselineDigest = newDigest
    state.explicitlyUnchecked.clear()
    state.conflict = false
    state.saving = false
  }

  /** 409 冲突：保留 working 但标记冲突，由界面提示重新加载。 */
  function markConflict() {
    state.conflict = true
    state.saving = false
  }

  return {
    state,
    load,
    reset,
    isWorking,
    isImplicit,
    checkedKeysFor,
    toggle,
    toggleMany,
    hasPermissionChanges,
    diff,
    keysOfLevel,
    buildPayload,
    markSaved,
    markConflict,
  }
}
