<template>
    <div>

    </div>
</template>
<script setup>
import { ref, onMounted } from 'vue'
import http from '@/js/utils/httpProxy.js'

const props = defineProps({
    tableMeta: Object,
})

onMounted(() => {

    console.log(props.tableMeta)
})

</script>

<style lang="less" scoped></style>