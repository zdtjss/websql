<template>
  <el-container class="layout-container-demo">
    <el-aside>
      <el-tree :data="treeData" highlight-current="true" :load="loadTree" :lazy="true">
        <template #default="{ node, data }">
          <span class="custom-tree-node">
            <span>
              <a>{{ node.label }}</a>
            </span>
          </span>
        </template>
      </el-tree>
    </el-aside>

    <el-container>
      <el-main>
        <RouterView></RouterView>
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { RouterView, useRouter } from 'vue-router'
import { Menu as IconMenu, Message, Setting } from '@element-plus/icons-vue'

import http from './js/utils/httpProxy.js'

onMounted(() => {
  // loadTree()
  router.push("/")
})

const router = useRouter()

const treeData = ref([{
  label: "",
  type: "",
  data: {},
  children: []
}])

function queryNest(data) {
  console.log(data)
}

function loadTree(node, resolve) {
  http.get("/showTree", { params: { connId: findConn(node), key: node.data.label, type: node.data.type } })
    .then((resp) => {
      resolve(resp.data.data)
    })
    .catch(function (error) {
      console.log(error);
    });
}

function findConn(node) {
  let connId = ""
  if (node.level === 0) {
    return connId
  } else if (node.level === 1) {
    connId = node.data.data.id
  } else {
    connId = findConn(node.parent)
  }
  return connId
}

</script>

<style scoped>
.layout-container-demo {
  width: calc(100vw * 0.98);
  height: calc(100vh * 0.98);
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
