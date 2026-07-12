<template>
  <div class="session-history-body">
    <!-- 搜索框 -->
    <div class="session-search-box">
      <el-input
        v-model="searchKeywordRef"
        size="small"
        placeholder="搜索会话标题"
        clearable
        @input="emit('search-input')"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
    </div>

    <!-- 会话列表 -->
    <div class="session-list-scroll">
      <el-empty v-if="loading && sessionList.length === 0" :description="'加载中...'" />
      <el-empty v-else-if="sessionList.length === 0 && searchKeyword" description="没有匹配的会话" />
      <el-empty v-else-if="sessionList.length === 0" description="暂无历史会话" />
      <el-skeleton v-else-if="loading" :rows="4" animated />
      <div v-else style="display: flex; flex-direction: column; gap: 8px;">
        <div v-for="sess in sessionList" :key="sess.id" class="session-item">
          <div class="session-content" @click="emit('click-session', sess.id)">
            <div class="session-title">{{ sess.title || '未命名会话' }}</div>
            <div class="session-time">
              <el-icon>
                <Clock />
              </el-icon>
              {{ formatDate(sess.createdAt) }}
            </div>
          </div>
          <div class="session-actions">
            <el-popconfirm title="确定要删除这个会话吗？" @confirm="emit('delete-session', sess.id)">
              <template #reference>
                <el-button type="danger" size="small" text @click.stop>
                  <el-icon>
                    <Delete />
                  </el-icon>
                </el-button>
              </template>
            </el-popconfirm>
          </div>
        </div>
      </div>
    </div>

    <!-- 分页 -->
    <div v-if="total > pageSize" class="session-pagination">
      <el-pagination
        v-model:current-page="currentPageRef"
        v-model:page-size="pageSizeRef"
        :page-sizes="[5, 10, 20, 50]"
        :total="total"
        layout="prev, pager, next"
        small
        @current-change="(p: number) => emit('page-change', p)"
        @size-change="(s: number) => emit('size-change', s)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 历史会话侧边栏组件。
 * 在 popover 中展示会话列表，支持搜索、分页、删除、切换。
 */
import { computed } from 'vue'
import { Clock, Delete, Search } from '@element-plus/icons-vue'
import type { ChatSessionItem } from '../composables/useChatHistory'

const props = defineProps<{
  /** 会话列表 */
  sessionList: ChatSessionItem[]
  /** 总数 */
  total: number
  /** 是否加载中 */
  loading: boolean
  /** 搜索关键词 */
  searchKeyword: string
  /** 当前页码 */
  currentPage: number
  /** 每页条数 */
  pageSize: number
  /** 时间格式化函数 */
  formatDate: (isoString?: string) => string
}>()

const emit = defineEmits<{
  /** 更新搜索关键词 */
  (e: 'update:search-keyword', val: string): void
  /** 更新当前页码 */
  (e: 'update:current-page', val: number): void
  /** 更新每页条数 */
  (e: 'update:page-size', val: number): void
  /** 搜索输入（带防抖） */
  (e: 'search-input'): void
  /** 页码变化 */
  (e: 'page-change', page: number): void
  /** 每页条数变化 */
  (e: 'size-change', size: number): void
  /** 点击某条会话 */
  (e: 'click-session', id: string): void
  /** 删除某条会话 */
  (e: 'delete-session', id: string): void
}>()

const searchKeywordRef = computed({
  get: () => props.searchKeyword,
  set: (v: string) => emit('update:search-keyword', v),
})

const currentPageRef = computed({
  get: () => props.currentPage,
  set: (v: number) => emit('update:current-page', v),
})

const pageSizeRef = computed({
  get: () => props.pageSize,
  set: (v: number) => emit('update:page-size', v),
})
</script>
