<template>
  <el-container class="layout-container-demo">
    <el-aside :width="treeDivWidth">
      <div style="text-align: right;margin-right: 10px;">
        <el-icon v-show="currentUser.isAdmin || !isRemote" color="#409EFC"
          @click="cfgDialogVisible = true; loadCfgData({ props: { name: defaultTabAdmin } })"
          style="cursor: pointer;margin-left: 8px;" title="配置">
          <Tools />
        </el-icon>
        <el-icon v-show="!loginSucc && isRemote" color="#409EFC" @click="toLogin"
          style="cursor: pointer;margin-left: 8px;" title="登录">
          <User />
        </el-icon>
        <el-icon v-show="loginSucc && isRemote" color="#409EFC" @click="register"
          style="cursor: pointer;margin-left: 8px;" title="注册指纹/面容">
          <svg t="1712659928093" class="icon" viewBox="0 0 1024 1024" version="1.1" xmlns="http://www.w3.org/2000/svg" p-id="7116" width="200" height="200"><path d="M218.763636 509.672727a346.763636 297.890909 90 1 0 595.781819 0 346.763636 297.890909 90 1 0-595.781819 0Z" fill="#EAF3FF" p-id="7117"></path><path d="M991.418182 539.927273c-13.963636 0-23.272727-9.309091-23.272727-23.272728 0-146.618182-72.145455-281.6-195.49091-365.381818-11.636364-6.981818-13.963636-20.945455-6.981818-32.581818 6.981818-11.636364 20.945455-13.963636 32.581818-6.981818 134.981818 90.763636 214.109091 242.036364 214.109091 402.618182 2.327273 16.290909-6.981818 25.6-20.945454 25.6zM200.145455 228.072727c83.781818-95.418182 204.8-148.945455 330.472727-148.945454 58.181818 0 116.363636 11.636364 169.890909 34.909091 11.636364 4.654545 25.6 0 30.254545-11.636364 4.654545-11.636364 0-25.6-11.636363-30.254545-60.509091-25.6-123.345455-37.236364-188.509091-37.236364-139.636364 0-272.290909 60.509091-365.381818 165.236364-9.309091 9.309091-6.981818 23.272727 2.327272 32.581818 4.654545 4.654545 9.309091 4.654545 16.290909 4.654545 4.654545-2.327273 11.636364-4.654545 16.29091-9.309091zM90.763636 516.654545c0-72.145455 18.618182-144.290909 53.527273-209.454545 6.981818-11.636364 2.327273-25.6-9.309091-32.581818-11.636364-6.981818-25.6-2.327273-32.581818 9.309091-39.563636 69.818182-58.181818 151.272727-58.181818 230.4 0 13.963636 9.309091 23.272727 23.272727 23.272727s23.272727-6.981818 23.272727-20.945455z m125.672728-79.127272c37.236364-144.290909 165.236364-242.036364 314.181818-242.036364 146.618182 0 274.618182 100.072727 311.854545 242.036364 2.327273 11.636364 16.290909 20.945455 27.927273 16.290909 11.636364-2.327273 20.945455-16.290909 16.290909-27.927273-41.890909-162.909091-190.836364-274.618182-358.4-274.618182-169.890909 0-316.509091 114.036364-358.4 279.272728-2.327273 11.636364 4.654545 25.6 16.290909 27.927272h4.654546c11.636364-4.654545 23.272727-11.636364 25.6-20.945454z m567.854545 79.127272c0-58.181818-20.945455-114.036364-55.854545-160.581818-6.981818-9.309091-23.272727-11.636364-32.581819-2.327272-9.309091 6.981818-11.636364 23.272727-2.327272 32.581818 30.254545 37.236364 46.545455 83.781818 46.545454 130.327272 0 13.963636 9.309091 23.272727 23.272728 23.272728s20.945455-9.309091 20.945454-23.272728z m-463.127273 0c0-114.036364 93.090909-207.127273 207.127273-207.127272 37.236364 0 72.145455 9.309091 104.727273 27.927272 11.636364 6.981818 25.6 2.327273 32.581818-9.30909 6.981818-11.636364 2.327273-25.6-9.309091-32.581819-39.563636-23.272727-83.781818-34.909091-128-34.909091-139.636364 0-253.672727 114.036364-253.672727 253.672728 0 13.963636 9.309091 23.272727 23.272727 23.272727s23.272727-6.981818 23.272727-20.945455z m346.763637 0c0-76.8-62.836364-139.636364-139.636364-139.636363s-139.636364 62.836364-139.636364 139.636363c0 13.963636 9.309091 23.272727 23.272728 23.272728s23.272727-9.309091 23.272727-23.272728c0-51.2 41.890909-93.090909 93.090909-93.090909s93.090909 41.890909 93.090909 93.090909c0 13.963636 9.309091 23.272727 23.272727 23.272728s23.272727-9.309091 23.272728-23.272728zM83.781818 549.236364c4.654545-13.963636 6.981818-27.927273 6.981818-44.218182 0-13.963636-9.309091-23.272727-23.272727-23.272727s-23.272727 9.309091-23.272727 23.272727c0 9.309091-2.327273 18.618182-4.654546 27.927273-4.654545 11.636364 2.327273 25.6 13.963637 30.254545h6.981818c9.309091 2.327273 18.618182-4.654545 23.272727-13.963636z m719.127273 372.363636c62.836364-130.327273 95.418182-269.963636 95.418182-416.581818 0-13.963636-9.309091-23.272727-23.272728-23.272727s-23.272727 9.309091-23.272727 23.272727c0 139.636364-30.254545 272.290909-90.763636 395.636363-4.654545 11.636364 0 25.6 11.636363 30.254546 2.327273 2.327273 6.981818 2.327273 9.309091 2.327273 9.309091 2.327273 16.290909-2.327273 20.945455-11.636364z m-176.872727 69.818182c102.4-141.963636 155.927273-309.527273 155.927272-486.4 0-13.963636-9.309091-23.272727-23.272727-23.272727s-23.272727 9.309091-23.272727 23.272727c0 165.236364-51.2 323.490909-148.945455 458.472727-6.981818 9.309091-4.654545 25.6 4.654546 32.581818 4.654545 2.327273 9.309091 4.654545 13.963636 4.654546 9.309091 0 16.290909-2.327273 20.945455-9.309091z m-151.272728 4.654545c102.4-109.381818 167.563636-246.690909 188.509091-395.636363 2.327273-11.636364-6.981818-23.272727-20.945454-25.6-11.636364-2.327273-23.272727 6.981818-25.6 20.945454-18.618182 139.636364-79.127273 267.636364-174.545455 370.036364-9.309091 9.309091-9.309091 23.272727 0 32.581818 4.654545 4.654545 9.309091 6.981818 16.290909 6.981818 4.654545-2.327273 11.636364-4.654545 16.290909-9.309091z m-128-37.236363c130.327273-114.036364 207.127273-279.272727 207.127273-453.818182 0-13.963636-9.309091-23.272727-23.272727-23.272727s-23.272727 9.309091-23.272727 23.272727c0 160.581818-69.818182 311.854545-190.836364 418.909091-9.309091 9.309091-11.636364 23.272727-2.327273 32.581818 4.654545 4.654545 11.636364 6.981818 18.618182 6.981818 2.327273 2.327273 9.309091 0 13.963636-4.654545z m-104.727272-65.163637c72.145455-53.527273 125.672727-125.672727 160.581818-207.127272 4.654545-11.636364 0-25.6-11.636364-30.254546-11.636364-4.654545-25.6 0-30.254545 13.963636-30.254545 74.472727-79.127273 139.636364-144.290909 186.181819-9.309091 6.981818-11.636364 23.272727-4.654546 32.581818 4.654545 6.981818 11.636364 9.309091 18.618182 9.309091 2.327273 0 6.981818 0 11.636364-4.654546z m186.181818-293.236363c6.981818-32.581818 9.309091-62.836364 9.309091-95.418182 0-13.963636-9.309091-23.272727-23.272728-23.272727s-23.272727 9.309091-23.272727 23.272727c0 30.254545-2.327273 58.181818-9.309091 86.109091-2.327273 11.636364 4.654545 25.6 18.618182 27.927272h4.654546c9.309091 2.327273 20.945455-6.981818 23.272727-18.618181z m-267.636364 209.454545c79.127273-53.527273 132.654545-134.981818 151.272727-228.072727 2.327273-11.636364-4.654545-25.6-18.618181-27.927273-11.636364-2.327273-25.6 4.654545-27.927273 18.618182-16.290909 81.454545-65.163636 151.272727-132.654546 197.818182-11.636364 6.981818-13.963636 20.945455-6.981818 32.581818 6.981818 6.981818 13.963636 11.636364 23.272728 11.636364 4.654545 0 9.309091-2.327273 11.636363-4.654546z m-53.527273-102.4c62.836364-48.872727 100.072727-121.018182 100.072728-202.472727 0-13.963636-9.309091-23.272727-23.272728-23.272727s-23.272727 9.309091-23.272727 23.272727c0 65.163636-30.254545 125.672727-81.454545 165.236363-9.309091 6.981818-11.636364 23.272727-4.654546 32.581819 4.654545 6.981818 11.636364 9.309091 18.618182 9.309091 4.654545 0 9.309091-2.327273 13.963636-4.654546z" fill="#2D85FF" p-id="7118"></path></svg>
        </el-icon>
        <el-icon v-show="loginSucc" color="#409EFC" @click="logout" style="cursor: pointer;margin-left: 8px;"
          title="退出">
          <SwitchButton />
        </el-icon>
      </div>
      <el-tree ref="connTree" :highlight-current="true" :load="loadTree" :lazy="true" :data="treeData" empty-text=""
        :props="{ isLeaf: 'isLeaf' }">
        <template #default="{ node, data }">
          <span>
            <a :title="data.data != null ? data.data.text : ''">{{ node.label }}</a>
          </span>
        </template>
      </el-tree>
    </el-aside>
    <div style="height: 100%; border: 1px solid #9e9e9e; cursor: col-resize;" @mousedown="resizeTreeArea"></div>
    <el-container>
      <el-main>
        <el-tabs v-model="editableTabsValue" type="card" class="demo-tabs" closable @tab-remove="removeTab">
          <el-tab-pane v-for="item in editableTabs" :key="item.tabId" :label="item.title" :name="item.tabId">
            <component :is="item.component" :tabId="item.tabId" :connId="item.connId" :schema="item.schema" />
          </el-tab-pane>
        </el-tabs>
      </el-main>
    </el-container>
    <el-dialog v-model="cfgDialogVisible" @close="cfgDialogVisible = false" :draggable="true" width="1000px"
      style="height:650px;">
      <el-tabs v-model="defaultTabAdmin" type="card" style="height:500px;" @tab-click="loadCfgData">
        <el-tab-pane v-if="isRemote" label="角色" name="role">
          <el-table :data="roleList" :max-height="450" style="width: 100%;overflow-y: auto;"
            @cell-dblclick="roleDblClick">
            <el-table-column prop="name" label="角色名" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.name" />
                <span v-show="!scope.row.editable">{{ scope.row.name }}</span>
              </template>
            </el-table-column>
            <el-table-column label="连接" :show-overflow-tooltip="true">
              <template #default="scope">
                  <span v-show="!scope.row.editable">{{ scope.row.connNameListStr }}</span>
                  <el-tree-select ref="roleConnTree" v-model="scope.row.connIdList" v-show="scope.row.editable" @check="checkPower"
                    style="width:100%" :data="connListSelect" node-key="id" multiple collapse-tags collapse-tags-tooltip
                    :check-on-click-node="true" show-checkbox placeholder="请选择" />
                </template>
            </el-table-column>
            <el-table-column style="text-align: center; " width="80">
              <template #header>
                <span>操作</span>
                <el-icon style="cursor: pointer;position: relative;left: 8px;top: -5px;" title="添加" @click="addRole">
                  <Plus />
                </el-icon>
              </template>
              <template #default="scope">
                <el-icon v-show="scope.row.editable" @click="saveRole(scope.row); scope.row.editable = false" title="保存"
                  style="margin-right:5px;cursor: pointer;">
                  <Select />
                </el-icon>
                <el-popconfirm title="确定要删除?" @confirm="delRole(scope.row)" confirm-button-text="是"
                  cancel-button-text="否">
                  <template #reference>
                    <el-icon style="cursor: pointer;" title="删除">
                      <Delete />
                    </el-icon>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane v-if="isRemote" label="用户" name="user">
          <el-form v-model="userQuery">
            <el-row>
              <el-form-item label="姓名" :label-width="formLabelWidth">
                <el-input v-model="userQuery.name" />
              </el-form-item>
              <el-form-item label="登录名" :label-width="formLabelWidth">
                <el-input v-model="userQuery.loginName" />
              </el-form-item>
              <el-form-item>
                <el-button @click="findUser" style="margin-left:12px;">查询</el-button>
              </el-form-item>
            </el-row>
          </el-form>
          <el-table :data="userList" :max-height="450" style="width: 100%;overflow-y: auto;" empty-text="请正确输入条件后查询"
            @cell-dblclick="(row) => row.editable = true">
            <el-table-column prop="name" label="姓名" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.name" />
                <span v-show="!scope.row.editable">{{ scope.row.name }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="loginName" label="登录名" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.loginName" />
                <span v-show="!scope.row.editable">{{ scope.row.loginName }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="pwd" label="密码" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.pwd" />
                <span v-show="!scope.row.editable">{{ scope.row.pwd }}</span>
              </template>
            </el-table-column>
            <el-table-column label="角色" :show-overflow-tooltip="true">
              <template #default="scope">
                <span v-show="!scope.row.editable">{{ scope.row.roleName.join("、") }}</span>
                <el-select v-show="scope.row.editable" v-model="scope.row.roleId" multiple filterable collapse-tags
                  collapse-tags-tooltip placeholder="请选择">
                  <el-option v-for="item in roleList" :key="item.id" :label="item.name" :value="item.id" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column style="text-align: center; " width="80">
              <template #header>
                <span>操作</span>
                <el-icon style="cursor: pointer;position: relative;left: 8px;top: -5px;" title="添加" @click="addUser">
                  <Plus />
                </el-icon>
              </template>
              <template #default="scope">
                <el-icon v-show="scope.row.editable" @click="saveUser(scope.row); scope.row.editable = false" title="保存"
                  style="margin-right:5px;cursor: pointer;">
                  <Select />
                </el-icon>
                <el-popconfirm title="确定要删除?" @confirm="delUser(scope.row)" confirm-button-text="是"
                  cancel-button-text="否">
                  <template #reference>
                    <el-icon style="cursor: pointer;" title="删除">
                      <Delete />
                    </el-icon>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="连接" name="conn">
          <el-table :data="connList" :max-height="450" style="width: 100%;overflow-y: auto;" empty-text="暂无连接"
            @cell-dblclick="(row) => row.editable = true" v-loading="loadingConn">
            <el-table-column prop="name" label="连接名称" width="150" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.name" />
                <span v-show="!scope.row.editable">{{ scope.row.name }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="dbType" label="数据库类型" width="100">
              <template #default="scope">
                <span v-show="!scope.row.editable">{{
                  dbTypeList.filter(t => t.value === scope.row.dbType)[0].label
                }}</span>
                <el-select v-show="scope.row.editable" v-model="scope.row.dbType" placeholder="请选择">
                  <el-option v-for="item in dbTypeList" :key="item.value" :label="item.label" :value="item.value" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column prop="parentId" label="所属层级" width="130">
              <template #default="scope">
                <span v-show="!scope.row.editable">{{ scope.row.parentName }}</span>
                <el-tree-select v-show="scope.row.editable" v-model="scope.row.parentId" :data="conCfgTreeData"
                  clearable value-key="id" placeholder="未指定" />
              </template>
            </el-table-column>
            <el-table-column prop="user" label="用户名" width="150" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.user" />
                <span v-show="!scope.row.editable">{{ scope.row.user }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="pwd" label="密码" width="120" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.pwd" />
                <span v-show="!scope.row.editable">{{ scope.row.pwd }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="url" label="连接信息" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.url" type="textarea" />
                <span v-show="!scope.row.editable">{{ scope.row.url }}</span>
              </template>
            </el-table-column>
            <el-table-column style="text-align: center; " width="80">
              <template #header>
                <span>操作</span>
                <el-icon style="cursor: pointer;position: relative;left: 8px;top: -5px;" title="添加" @click="addConn">
                  <Plus />
                </el-icon>
              </template>
              <template #default="scope">
                <el-icon v-show="scope.row.editable" @click="saveConnCfg(scope.row); scope.row.editable = false"
                  title="保存" style="margin-right:5px;cursor: pointer;">
                  <Select />
                </el-icon>
                <el-popconfirm title="确定要删除?" @confirm="delConnCfg(scope.row)" confirm-button-text="是"
                  cancel-button-text="否">
                  <template #reference>
                    <el-icon style="cursor: pointer;" title="删除">
                      <Delete />
                    </el-icon>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="目录" name="dir">
          <div style="padding: 65px 200px;">
            <el-tree :data="conCfgTreeData" draggable default-expand-all :expand-on-click-node="false">
              <template #default="{ node, data }">
                <div style="width:100%;">
                  <div style="display: inline-block;width: 100%;">
                    <el-input v-model="data.label"></el-input>
                  </div>
                  <div style="margin-left: 30px;display: inline-block;">
                    <a @click="appendTreeNode(data)">添加</a>
                    <el-popconfirm title="确定要删除?" @confirm="removeDir(node, data)" confirm-button-text="是"
                      cancel-button-text="否">
                      <template #reference>
                        <a style="margin-left: 8px">删除</a>
                      </template>
                    </el-popconfirm>
                  </div>
                </div>
              </template>
            </el-tree>
          </div>
          <div style="float: right; margin-right: 100px;">
              <el-button type="primary" @click="saveTree">保存</el-button>
            </div>
        </el-tab-pane>
      </el-tabs>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="cfgDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
    <!-- 登录对话框 -->
    <el-dialog v-model="loginDialogVisible" @close="loginDialogVisible = false" width="350px" @keyup.enter="login"
      @opened="loginName.focus()">
      <el-form ref="loginFormRef" :model="loginForm" :rules="loginRules" label-width="80px">
        <el-form-item label="用户名" prop="name">
          <el-input ref="loginName" v-model="loginForm.name" />
        </el-form-item>
        <el-form-item label="密&nbsp;&nbsp;&nbsp;码" prop="password">
          <el-input v-model="loginForm.password" type="password" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button type="primary" @click="login" :loading="logining">登录</el-button>
          <el-button @click="loginDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
  </el-container>
</template>

<script setup>
import { ref, reactive, shallowRef, onMounted, computed } from 'vue'
import { client, parsers } from '@passwordless-id/webauthn'
import SQLEditor2 from './views/SQLEditor2.vue'
import http from './js/utils/httpProxy.js'
import { dbSchemaProxy } from '@/stores/sql'

const sqlEditor = shallowRef(SQLEditor2)

const defaultTabAdmin = computed(() => {
  return isRemote.value ? "role" : "conn"
})

const editableTabsValue = ref('')
const editableTabs = ref([])

const connTree = ref()
const treeData = ref([])
const treeDivWidth = ref("260px")

const loginForm = ref({ name: "", password: "" })
const loginDialogVisible = ref(false)
const currentUser = ref({
  isAdmin: false,
  name: ""
})
const loginName = ref()
const loginFormRef = ref()
const loginSucc = ref(!!sessionStorage.getItem("authentication"))

const bioLocalStorageKey = "nway_websql_bio_credential_id"

const isRemote = ref(null)
  
const logining = ref(false)
const loginRules = reactive({
  name: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
  ],
})

const loadingConn = ref(false)

const formLabelWidth = '100px'
const cfgDialogVisible = ref(false)
const userList = ref([])
const connListSelect = ref([])
const roleList = ref([])
const userQuery = ref({
  name: "",
  loginName: ""
})
const connList = ref([])
const roleConnTree = ref([])
const powerListChecked = []
const conCfgTreeData = ref([])
const dbTypeList = ref([{ label: "MySQL", value: "mysql" }, { label: "Oracle", value: "oracle" }])

onMounted(() => {
  getSysModel()
  const storedTabs = JSON.parse(localStorage.getItem("editableTabs") || "[]")
  storedTabs.forEach(tab => tab.component = sqlEditor)
  editableTabs.value.push(...storedTabs)
  editableTabsValue.value = localStorage.getItem("editableTabsValue") || ""
})

const addTab = (node) => {
  if (node.data.type !== "schema") {
    return
  }
  const tabId = Date.now().toString(36)
  editableTabs.value.push({
    tabId: tabId,
    title: node.data.label,
    connId: findConn(node),
    schema: node.data.label,
    component: sqlEditor,
  })
  editableTabsValue.value = tabId
  restoreTab()
}
const removeTab = (targetName) => {
  const tabs = editableTabs.value
  let activeName = editableTabsValue.value
  if (activeName === targetName) {
    tabs.forEach((tab, index) => {
      if (tab.tabId === targetName) {
        const nextTab = tabs[index + 1] || tabs[index - 1]
        if (nextTab) {
          activeName = nextTab.tabId
        }
      }
    })
  }
  editableTabsValue.value = activeName
  editableTabs.value = tabs.filter((tab) => tab.tabId !== targetName)
  restoreTab()
}

function restoreTab() {
  const waitStoredTabs = JSON.parse(JSON.stringify(editableTabs.value))
  waitStoredTabs.forEach(tab => tab.component = null)
  localStorage.setItem("editableTabs", JSON.stringify(waitStoredTabs))
  localStorage.setItem("editableTabsValue", editableTabsValue.value)
  if (editableTabs.value.length == 0) {
    // 清空可能带来负面清理
    localStorage.clear()
    dbSchemaProxy.cleanCache()
  }
}

function resizeTreeArea(event) {
  const startX = event.clientX
  const ogiWidth = new Number(treeDivWidth.value.substring(0, treeDivWidth.value.length - 2))
  document.onmousemove = (e) => {
    treeDivWidth.value = (ogiWidth + e.clientX - startX) + "px"
  }
  document.onmouseup = () => {
    document.onmouseup = null
    document.onmousemove = null
  }
}

function loadTree(node, resolve) {

  if ((Object.keys(node.data).length === 0 && !loginSucc.value && isRemote.value) || node.data.type === 'column') {
    resolve([])
    return
  }
  http.get("/showTree", { params: { connId: findConn(node), key: node.data.type === 'dir' ? node.data.id : node.data.label, type: node.data.type, level: node.level } })
    .then((resp) => {
      if (node.data.type === "schema") {
        dbSchemaProxy.addTable(node.data.label, node.data.data.dbType, resp.data.data)

        addTab(node)

        /* http.get("/showTree", { params: { connId: findConn(node), key: node.data.label, type: "all_column" } })
          .then((resp2) => {
            dbSchemaProxy.addTable(node.data.label + "_col", resp2.data.data)
          }) */
      }
      if (resp.data.data) {
        resolve(resp.data.data.map(e => {
          if (e.type === "column") {
            return Object.assign({ isLeaf: true }, e)
          }
          return e
        }))
      }
      node.loaded = false
    })
    .catch((error) => {
      console.log(error);
      node.loading = false
    });
}

function findConn(node) {
  let connId = ""
  if (node.level === 0) {
    return connId
  } else if (node.data.type === "conn") {
    connId = node.data.id
  } else {
    connId = findConn(node.parent)
  }
  return connId
}

function addConn() {
  connList.value.unshift({ dbType: "mysql", editable: true })
}

function saveConnCfg(row) {
  http.post("/saveConn", row)
    .then((resp) => {
      row.editable = false
      ElMessage("保存成功")
    })
}

function listConnCfg() {
  loadingConn.value = true
  http.get("/listConn2")
    .then((resp) => {
      connList.value = resp.data.data.map(e => Object.assign({ editable: false }, e))
      setTimeout(listDirTree(), 1000)
    }).finally(() => loadingConn.value = false)
}

function delConnCfg(row) {
  if (row.id) {
    http.get("/delConn", { params: { id: row.id } })
      .then((resp) => {
        listConnCfg()
      })
  } else {
    connList.value = connList.value.filter(item => item != row)
  }
}

function login() {
  loginFormRef.value.validate(isValid => {
    if (isValid) {
      logining.value = true
      const params = new URLSearchParams();
      params.append("name", loginForm.value.name);
      params.append("password", loginForm.value.password);
      params.append("loginType", "pwd");
      http.post("/login", params)
        .then((resp) => {
          refreshTree()
          currentUser.value = resp.data.data
          sessionStorage.setItem("authentication", resp.headers.get("authentication"))
          loginForm.value = {}
          logining.value = false
          loginSucc.value = true
          loginDialogVisible.value = false
          ElMessage("登陆成功")
        }).finally(() => logining.value = false)
    }
  })
}


async function register() {

  if (!client.isAvailable()) {
    ElMessage({
      showClose: true,
      message: '您的设备不支持生物识别',
      type: 'error',
    })
    return;
  }
  let res = await client.register(currentUser.value.name, window.crypto.randomUUID())

  const parsed = parsers.parseRegistration(res)

  window.localStorage.setItem(bioLocalStorageKey, parsed.credential.id)
  
  const params = new URLSearchParams();
  params.append("bioKey", parsed.credential.id);
  http.post("/saveUserBio", params).then((resp) => {
    if (data.code == 200) {
      ElMessage("注册成功")
    } else {
      ElMessage(data.msg)
    }
  }).catch((error) => {
    ElMessage(error)
  });
}

async function loginBio() {
  const credentialId = window.localStorage.getItem(bioLocalStorageKey)
  // 第一个参数指定值，可以简化用户选择的操作
  let res = await client.authenticate(credentialId == null ? [] : [credentialId], window.crypto.randomUUID())
  const params = new URLSearchParams();
  params.append("key", res.credentialId);
  params.append("loginType", "bio");
  http.post("/login", params).then((resp) => {
    if (resp.data.code == 200) {
      if (!credentialId) {
        window.localStorage.setItem(bioLocalStorageKey, res.credentialId)
      }
      refreshTree()
      currentUser.value = resp.data.data
      sessionStorage.setItem("authentication", resp.headers.get("authentication"))
      loginForm.value = {}
      logining.value = false
      loginSucc.value = true
      loginDialogVisible.value = false
      ElMessage("登陆成功")
    } else {
      errMsg.value = data.msg
    }
  }).catch((error) => {
    errMsg.value = error
  });
}

function logout() {
  http.post("/logout")
    .then((resp) => {
      refreshTree()
      currentUser.value = {}
      loginSucc.value = false
      ElMessage(resp.data.data)
      sessionStorage.removeItem("authentication")
    })
}

function toLogin() {
  const credentialId = window.localStorage.getItem(bioLocalStorageKey)
  if (credentialId && client.isAvailable()) {
    loginBio()
  } else {
    loginDialogVisible.value = true
  }
}

function getSysModel() {
  http.get("/sysMode").then((resp) => {
    isRemote.value = resp.data.data.isRemote
    if (!loginSucc.value && isRemote.value) {
      toLogin()
    }
  })
}

function refreshTree() {
  http.get("/showTree", { params: { connId: "", key: "", type: "dir", level: 0 } })
    .then((resp) => {
      treeData.value = resp.data.data
    })
}

function addUser() {
  userList.value.push({ "roleId": [], "roleName": [], "loginName": "", "name": "", "pwd": "", editable: true })
}

function findUser() {
  if (!userQuery.value.name && !userQuery.value.loginName) {
    ElMessage("请指定查询条件")
    return
  }
  http.get("/findUser", { params: userQuery.value })
    .then((resp) => {
      userList.value = resp.data.data.map(e => Object.assign({ editable: false }, e))
    })
}

function saveUser(row) {
  http.post("/saveUser", row)
    .then((resp) => {
      row.editable = false
      ElMessage("保存成功")
      row.roleName = row.roleId.map((val) => roleList.value.filter(item => item.id === val)[0].name)
    })
}

function delUser(row) {
  if (row.id) {
    http.get("/delUser", { params: { id: row.id } })
      .then((resp) => {
        findUser()
      })
  } else {
    userList.value = userList.value.filter(item => item != row)
  }
}

function loadCfgData(pane) {
  if (pane.props.name === "role") {
    http.get("/roleList")
      .then((resp) => {
        roleList.value = resp.data.data.map(e => {
          const row = Object.assign({ editable: false }, e)
          if (row.powerList) {
            row.connNameListStr = row.powerList.map(r => r.connName).join("、")
          }
          return row
        })
      })
  } else if (pane.props.name === "user") {

  } else if (pane.props.name === "conn") {
    http.get("/listConn2")
      .then((resp) => {
        connList.value = resp.data.data.map(e => Object.assign({ editable: false }, e))
        setTimeout(listDirTree(), 1000)
      })
  } else if (pane.props.name === "dir") {
    listDirTree()
  }
}

function addRole() {
  roleList.value.push({ editable: true })
  roleDblClick({})
}

function roleDblClick(row) {
  row.editable = true
  powerListChecked.splice(0, powerListChecked.length)
  row.connIdList = row.powerList ? row.powerList.map(r => r.connId) : []
  http.get("/connBaseTree")
    .then((resp) => {
      connListSelect.value = resp.data.data
    })
}

function saveRole(row) {
  const param = Object.assign({}, row)
  param.connIdList = []
  param.connIdList.push(...powerListChecked)
  http.post("/saveRole", param)
    .then((resp) => {
      loadCfgData({ props: { name: 'role' } })
      ElMessage("保存成功")
    })
}

function checkPower(data, status) {
  powerListChecked.push(...status.checkedKeys)
}

function delRole(row) {
  if (row.id) {
    http.get("/delRole", { params: { id: row.id } })
      .then((resp) => {
        loadCfgData({ props: { name: 'role' } })
      })
  } else {
    roleList.value = roleList.value.filter(item => item != row)
  }
}

function saveTree() {
  http.post("/saveTree", conCfgTreeData.value)
    .then((resp) => {
      ElMessage("保存成功")
    })
}

function listDirTree() {
  http.get("/listDirTree")
    .then((resp) => {
      conCfgTreeData.value = resp.data.data.length === 0 ? [{ label: "", value: "" }] : resp.data.data
    })
}

const appendTreeNode = (data) => {
  const newChild = { label: "", value: "", children: [] }
  conCfgTreeData.value.push(newChild)
}

const removeDir = (node, data) => {
  conCfgTreeData.value = conCfgTreeData.value.filter(item => item != data)
  if (data.id) {
    http.get("/delTreeNode", { params: { id: data.id } })
      .then((resp) => {
        conCfgTreeData.value = resp.data.data.length === 0 ? [{ label: "", value: "" }] : resp.data.data
      })
  }
}

</script>

<style scoped>
.layout-container-demo {
  /* width: calc(100vw * 0.98); */
  height: calc(100vh * 0.97);
}

.layout-container-demo .el-header {
  position: relative;
  color: var(--el-text-color-primary);
}

.layout-container-demo .el-aside {
  color: var(--el-text-color-primary);
}

.layout-container-demo .el-menu {
  border-right: none;
}

.layout-container-demo .el-main {
  padding: 0;
}
</style>

<style lang="less" scoped>
.el-tree-node {
  overflow-x: auto;
}
</style>