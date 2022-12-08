<template>
  <div class="about">
    <div style="display:block;">
      <div>This is an test content {{ data.data }}</div>
      <div v-if="data.ssv">{{ data.ssv }}</div>
      <div v-if="data.lsv">{{ data.lsv }}</div>
      <div v-if="data.pssv">{{ data.pssv }}</div>
      <div v-if="data.plsv">{{ data.plsv }}</div>
      <div v-if="data.pvar">{{ data.pvar }}</div>
      <div v-if="data.token">{{ data.token }}</div>
      <input type="button" @click="show" value="查看" />
    </div>
  </div>
</template>

<script setup>

import { reactive, onMounted } from 'vue'

const data = reactive({
  data: "哈哈",
  ssv: "",
  lsv: "",
  pssv: "",
  plsv: "",
  pvar: "",
  token: ""
})


onMounted(() => {
  sessionStorage.setItem("ssk", "sessionStorage 值")
  localStorage.setItem("lsk", "localStorage 值")
}
)

function show() {
  debugger
  data.ssv = sessionStorage.getItem("ssk")
  data.lsv = localStorage.getItem("lsk")
  data.pvar = location.search
  data.token = sessionStorage.getItem("token")
  data.pssv = parent.sessionStorage.getItem("parent_ssk")
  data.plsv = parent.localStorage.getItem("parent_lsk")
}
</script>
<style>
@media (min-width: 1024px) {
  .about {
    min-height: 100vh;
    display: flex;
    align-items: center;
  }
}
</style>
