import { ref, computed } from 'vue'
import { listBackupData, showBackupData as showBackupDataApi } from '@/api/sql'
import excel from '@/utils/excel'

export interface UseSqlHistoryOptions {
    connId: string
    schema: string
}

/**
 * SQL 执行历史管理：
 * - showSqlHistory: 从后端分页加载执行历史
 * - filteredSqlHistory: 按关键字过滤的历史列表
 * - 抽屉宽度拖拽（onDrawerDragStart / Move / End）
 * - 备份数据查看与导出（showBackupData / exportBackupData）
 *
 * applySqlFromHistory 需要编辑器实例，由调用方通过 emit 处理。
 */
export function useSqlHistory(options: UseSqlHistoryOptions) {
    const { connId, schema } = options

    // ── 历史抽屉状态 ──
    const sqlHistoryDrawerShow = ref(false)
    const sqlHistoryList = ref<any[]>([])
    const sqlHistorySearch = ref('')
    const sqlHistoryTotal = ref(0)
    const sqlHistoryCurrent = ref(1)
    const sqlHistoryPageSize = ref(35)
    const sqlDrawerWidth = ref(600)
    const isDraggingDrawer = ref(false)

    // ── 备份数据状态 ──
    const backupData = ref('')
    const backupDataDrawerShow = ref(false)

    const filteredSqlHistory = computed(() => {
        const kw = sqlHistorySearch.value.trim().toLowerCase()
        if (!kw) return sqlHistoryList.value
        return sqlHistoryList.value.filter((item: any) =>
            (item.exec_sql || '').toLowerCase().includes(kw)
        )
    })

    function showSqlHistory() {
        listBackupData({
            connId,
            schema,
            current: sqlHistoryCurrent.value,
            pageSize: sqlHistoryPageSize.value,
        }).then((resp: any) => {
            sqlHistoryList.value = resp.data.data.data || []
            sqlHistoryTotal.value = resp.data.data.total || 0
            sqlHistoryDrawerShow.value = true
        })
    }

    // ── 抽屉宽度拖拽 ──

    function onDrawerDragStart(e: MouseEvent) {
        isDraggingDrawer.value = true
        document.addEventListener('mousemove', onDrawerDragMove)
        document.addEventListener('mouseup', onDrawerDragEnd)
        e.preventDefault()
    }

    function onDrawerDragMove(e: MouseEvent) {
        if (!isDraggingDrawer.value) return
        const newWidth = window.innerWidth - e.clientX
        if (newWidth >= 300 && newWidth <= 1200) {
            sqlDrawerWidth.value = newWidth
        }
    }

    function onDrawerDragEnd() {
        isDraggingDrawer.value = false
        document.removeEventListener('mousemove', onDrawerDragMove)
        document.removeEventListener('mouseup', onDrawerDragEnd)
    }

    // ── 备份数据 ──

    function showBackupData(backupId: any) {
        showBackupDataApi(backupId).then((resp) => {
            backupData.value = JSON.stringify(JSON.parse(resp.data.data), null, 4)
            backupDataDrawerShow.value = true
        })
    }

    function exportBackupData() {
        let header: any = {}
        let keys: any = []

        const jsonObj = JSON.parse(backupData.value)
        if (Array.isArray(jsonObj)) {
            keys = Object.keys(jsonObj[0])
        } else {
            keys = Object.keys(jsonObj)
        }

        keys.forEach((key: any) => {
            header[key] = key
        })

        const obj = {
            header,
            title: '',
            key: keys,
            data: Array.isArray(jsonObj) ? jsonObj : [jsonObj],
            filename: '导出的备份数据',
            autoWidth: false,
        }
        excel.exportJsonToExcel(obj)
    }

    return {
        // history state
        sqlHistoryDrawerShow,
        sqlHistoryList,
        sqlHistorySearch,
        sqlHistoryTotal,
        sqlHistoryCurrent,
        sqlHistoryPageSize,
        sqlDrawerWidth,
        isDraggingDrawer,
        filteredSqlHistory,
        // backup state
        backupData,
        backupDataDrawerShow,
        // methods
        showSqlHistory,
        onDrawerDragStart,
        onDrawerDragMove,
        onDrawerDragEnd,
        showBackupData,
        exportBackupData,
    }
}
