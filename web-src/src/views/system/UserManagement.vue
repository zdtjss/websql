<template>
  <div class="user-management">
    <div class="table-toolbar">
      <el-form :inline="true" :model="userQuery">
        <el-form-item label="姓名">
          <el-input v-model="userQuery.name" placeholder="请输入姓名" clearable />
        </el-form-item>
        <el-form-item label="登录名">
          <el-input v-model="userQuery.loginName" placeholder="请输入登录名" clearable />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="userQuery.roleId" placeholder="请选择角色" style="width: 180px" filterable clearable>
            <el-option v-for="item in roleList" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="findUserQuery">查询</el-button>
          <el-button type="primary" @click="addUser">添加用户</el-button>
        </el-form-item>
      </el-form>
    </div>
    
    <el-table :data="userList" v-loading="loading" style="width: 100%">
      <el-table-column type="index" width="50" />
      <el-table-column prop="name" label="姓名" width="150">
        <template #default="scope">
          <el-input v-if="scope.row.editing" v-model="scope.row.name" />
          <span v-else>{{ scope.row.name }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="loginName" label="登录名" width="150">
        <template #default="scope">
          <el-input v-if="scope.row.editing" v-model="scope.row.loginName" />
          <span v-else>{{ scope.row.loginName }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="pwd" label="密码" width="150">
        <template #default="scope">
          <el-input v-if="scope.row.editing" v-model="scope.row.pwd" type="password" placeholder="不修改请留空" />
          <span v-else>******</span>
        </template>
      </el-table-column>
      <el-table-column label="角色" width="200">
        <template #default="scope">
          <span v-if="!scope.row.editing">{{ scope.row.roleName.join(",") }}</span>
          <el-select 
            v-else
            v-model="scope.row.roleId" 
            multiple 
            filterable 
            collapse-tags 
            collapse-tags-tooltip 
            placeholder="请选择角色"
          >
            <el-option v-for="item in roleList" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="280" fixed="right">
        <template #default="scope">
          <el-button 
            v-if="!scope.row.editing" 
            type="primary" 
            size="small"
            @click="scope.row.editing = true"
          >
            编辑
          </el-button>
          <el-button 
            v-else 
            type="success" 
            size="small"
            @click="saveUserRow(scope.row)"
          >
            保存
          </el-button>
          <el-button
            v-if="scope.row.id"
            type="primary"
            link
            size="small"
            title="查看合并后的有效数据权限"
            @click="showEffectivePermissions(scope.row)"
          >
            有效权限
          </el-button>
          <el-popconfirm title="确定要删除这个用户吗？" @confirm="delUserRow(scope.row)">
            <template #reference>
              <el-button type="danger" size="small">删除</el-button>
            </template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      v-if="total > 0"
      v-model:current-page="page"
      v-model:page-size="pageSize"
      class="user-pagination"
      :total="total"
      :page-sizes="[10, 20, 50, 100]"
      layout="total, sizes, prev, pager, next"
      background
      @current-change="findUserQuery"
      @size-change="handlePageSizeChange"
    />

    <el-dialog v-model="effectiveDialogVisible" title="合并后的有效数据权限" width="720px" top="8vh">
      <div v-loading="effectiveLoading" class="effective-body">
        <template v-if="effectiveData">
          <div class="effective-row">
            <span class="effective-label">用户</span>
            <span>{{ effectiveData.userName }}（{{ effectiveData.loginName }}）</span>
            <el-tag v-if="effectiveData.isAdmin" size="small" type="warning">内置管理员：不受数据权限限制</el-tag>
          </div>
          <div class="effective-row">
            <span class="effective-label">角色</span>
            <el-tag v-for="role in effectiveData.roles" :key="role.id" size="small" class="effective-tag">
              {{ role.name }}
            </el-tag>
            <span v-if="effectiveData.roles.length === 0" class="effective-empty">未绑定角色</span>
          </div>
          <div class="effective-row">
            <span class="effective-label">写能力</span>
            <el-tag size="small" :type="effectiveData.allowModify ? 'success' : 'info'">
              修改数据：{{ effectiveData.allowModify ? '允许' : '禁止' }}
            </el-tag>
            <el-tag size="small" :type="effectiveData.aiModifyEffective ? 'success' : 'info'" style="margin-left: 8px;">
              AI 修改数据：{{ effectiveData.aiModifyEffective ? '允许' : '禁止' }}
            </el-tag>
            <span class="effective-hint">
              {{ effectiveData.aiPolicyEnforce
                ? 'AI 写需同一角色同时允许「修改数据」与「AI 修改数据」'
                : '兼容期：AI 写能力当前等同于「修改数据」' }}
            </span>
          </div>
          <el-divider content-position="left">合并后的数据访问范围</el-divider>
          <el-empty v-if="effectiveGroups.length === 0" description="当前用户没有任何数据授权" :image-size="60" />
          <div v-for="group in effectiveGroups" :key="group.connId" class="effective-conn">
            <div class="effective-conn-head">
              <span class="effective-conn-name">{{ group.connName || group.connId }}</span>
              <el-tag size="small" :type="group.fullAccess ? 'success' : 'warning'">{{ group.status }}</el-tag>
            </div>
            <div v-if="group.schemas.length > 0" class="effective-grants">
              <span class="effective-grant-label">Schema</span>
              <el-tag v-for="s in group.schemas" :key="'s-' + s" size="small" effect="plain" class="effective-tag">{{ s }}</el-tag>
            </div>
            <div v-if="group.tables.length > 0" class="effective-grants">
              <span class="effective-grant-label">表</span>
              <el-tag v-for="t in group.tables" :key="'t-' + t" size="small" effect="plain" class="effective-tag">{{ t }}</el-tag>
            </div>
            <div v-if="group.columns.length > 0" class="effective-grants">
              <span class="effective-grant-label">列</span>
              <el-tag v-for="c in group.columns" :key="'c-' + c" size="small" effect="plain" class="effective-tag">{{ c }}</el-tag>
            </div>
          </div>
        </template>
      </div>
      <template #footer>
        <el-button @click="effectiveDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { getRoleList, findUserPage, saveUser, delUser, getUserEffectivePermissions } from '@/api/system'
import { unwrap } from '@/utils/apiResult'
import { ElMessage } from 'element-plus'

const emit = defineEmits(['user-saved', 'user-deleted'])

const userList = ref([])
const roleList = ref([])
const userQuery = ref({ name: "", loginName: "", roleId: "" })
const loading = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)

const effectiveDialogVisible = ref(false)
const effectiveLoading = ref(false)
const effectiveData = ref(null)

const loadRoles = () => {
  getRoleList().then((resp) => {
    roleList.value = resp.data.data || []
  })
}

const findUserQuery = () => {
  loading.value = true
  findUserPage({
    ...userQuery.value,
    page: page.value,
    pageSize: pageSize.value,
  }).then((resp) => {
    const data = unwrap(resp) || {}
    userList.value = (data.list || []).map(e => Object.assign({ editing: false }, e))
    total.value = data.total || 0
  }).catch((e) => {
    ElMessage.error(e?.message || '查询用户失败')
  }).finally(() => {
    loading.value = false
  })
}

const handlePageSizeChange = () => {
  page.value = 1
  findUserQuery()
}

const addUser = () => {
  userList.value.push({ 
    roleId: [], 
    roleName: [], 
    loginName: "", 
    name: "", 
    pwd: "", 
    editing: true 
  })
}

const saveUserRow = async (row) => {
  try {
    unwrap(await saveUser(row))
    ElMessage.success("保存成功")
    row.editing = false
    emit('user-saved', row)
    findUserQuery()
  } catch (e) {
    // 保存失败时保持编辑态，便于修正重复登录名/空密码等问题
    ElMessage.error(e?.message || '保存失败')
  }
}

const delUserRow = async (row) => {
  if (row.id) {
    try {
      unwrap(await delUser(row.id))
      ElMessage.success("删除成功")
      if (userList.value.length === 1 && page.value > 1) page.value--
      findUserQuery()
      emit('user-deleted', row)
    } catch (e) {
      ElMessage.error(e?.message || '删除失败')
    }
  } else {
    userList.value = userList.value.filter(item => item != row)
    emit('user-deleted', row)
  }
}

/**
 * 合并后的有效权限分组：一个连接一行，区分「连接级全量」与「存在下级限制」。
 * 展示口径与 AI 链路一致：多角色并集，连接级授权会放大到无限制的 schema。
 */
const effectiveGroups = computed(() => {
  const powers = effectiveData.value?.powers || []
  const groups = new Map()
  for (const p of powers) {
    let group = groups.get(p.connId)
    if (!group) {
      group = {
        connId: p.connId,
        connName: p.connName || '',
        connLevel: false,
        schemas: new Set(),
        tables: new Set(),
        columns: new Set(),
      }
      groups.set(p.connId, group)
    }
    if (p.level === 'conn') {
      group.connLevel = true
    } else if (p.level === 'schema') {
      group.schemas.add(p.schemaName || '')
    } else if (p.level === 'table') {
      group.tables.add(`${p.schemaName || ''}.${p.tableName || ''}`)
    } else if (p.level === 'column') {
      group.columns.add(`${p.schemaName || ''}.${p.tableName || ''}.${p.columnName || ''}`)
    }
  }
  return [...groups.values()].map((g) => {
    const hasRestriction = g.schemas.size > 0 || g.tables.size > 0 || g.columns.size > 0
    const fullAccess = !!effectiveData.value?.isAdmin || (g.connLevel && !hasRestriction)
    let status = '局部授权'
    if (fullAccess) status = '全部授权（连接级）'
    else if (g.connLevel) status = '连接级 + 下级限制'
    return {
      ...g,
      fullAccess,
      status,
      schemas: [...g.schemas].filter(Boolean),
      tables: [...g.tables].filter(Boolean),
      columns: [...g.columns].filter(Boolean),
    }
  })
})

const showEffectivePermissions = async (row) => {
  effectiveDialogVisible.value = true
  effectiveLoading.value = true
  effectiveData.value = null
  try {
    effectiveData.value = unwrap(await getUserEffectivePermissions(row.id))
  } catch (e) {
    ElMessage.error(e?.message || '查询有效权限失败')
    effectiveDialogVisible.value = false
  } finally {
    effectiveLoading.value = false
  }
}

onMounted(() => {
  loadRoles()
  findUserQuery()
})
</script>

<style scoped>
.user-management {
  padding: 20px;
}

.table-toolbar {
  margin-bottom: 16px;
}

:deep(.el-table) {
  font-size: 14px;
}

:deep(.el-table th) {
  background-color: var(--bg-secondary);
  color: var(--text-secondary);
  font-weight: 600;
}

.user-pagination {
  margin-top: 16px;
  justify-content: flex-end;
}

.effective-body {
  min-height: 120px;
  max-height: 60vh;
  overflow: auto;
}

.effective-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}

.effective-label {
  width: 64px;
  color: var(--text-secondary);
  flex-shrink: 0;
}

.effective-hint {
  color: var(--text-tertiary);
  font-size: 12px;
}

.effective-tag {
  margin-right: 6px;
  margin-bottom: 4px;
}

.effective-empty {
  color: var(--text-tertiary);
}

.effective-conn {
  border: 1px solid var(--border-primary);
  border-radius: 6px;
  padding: 10px 12px;
  margin-bottom: 10px;
}

.effective-conn-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.effective-conn-name {
  font-weight: 600;
}

.effective-grants {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-top: 6px;
  flex-wrap: wrap;
}

.effective-grant-label {
  width: 56px;
  flex-shrink: 0;
  color: var(--text-tertiary);
  font-size: 12px;
  line-height: 24px;
}
</style>
