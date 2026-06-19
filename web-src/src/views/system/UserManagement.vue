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
    
    <el-table :data="userList" style="width: 100%">
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
      <el-table-column label="操作" width="150" fixed="right">
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
          <el-popconfirm title="确定要删除这个用户吗？" @confirm="delUserRow(scope.row)">
            <template #reference>
              <el-button type="danger" size="small">删除</el-button>
            </template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { getRoleList, findUser, saveUser, delUser } from '@/api/system'
import { ElMessage } from 'element-plus'

const emit = defineEmits(['user-saved', 'user-deleted'])

const userList = ref([])
const roleList = ref([])
const userQuery = ref({ name: "", loginName: "", roleId: "" })

const loadRoles = () => {
  getRoleList().then((resp) => {
    roleList.value = resp.data.data || []
  })
}

const findUserQuery = () => {
  if (!userQuery.value.name && !userQuery.value.loginName && !userQuery.value.roleId) {
    ElMessage.warning("请指定查询条件")
    return
  }
  findUser(userQuery.value).then((resp) => {
    userList.value = resp.data.data.map(e => Object.assign({ editing: false }, e))
  })
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

const saveUserRow = (row) => {
  saveUser(row).then(() => {
    ElMessage.success("保存成功")
    row.roleName = row.roleId.map((val) => {
      const role = roleList.value.find(item => item.id === val)
      return role ? role.name : ''
    }).filter(n => n)
    row.editing = false
    emit('user-saved', row)
  })
}

const delUserRow = (row) => {
  if (row.id) {
    delUser(row.id).then(() => {
      ElMessage.success("删除成功")
      findUserQuery()
      emit('user-deleted', row)
    })
  } else {
    userList.value = userList.value.filter(item => item != row)
    emit('user-deleted', row)
  }
}

loadRoles()
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
</style>
