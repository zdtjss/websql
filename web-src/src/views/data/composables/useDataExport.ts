import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import http from '@/api/index'
import { quoteId } from '@/utils/sqlHelper.ts'
import { exportToCsv, exportToJson, exportToSql, downloadBlob } from '@/utils/exportHelper.ts'
import { buildSelectSQL } from '@/utils/sqlHelper.ts'
import type { Ref } from 'vue'
import type { DataColumn } from './useDataQuery'

export type ExportFormat = 'xlsx' | 'csv' | 'json' | 'sql'

export interface DataExportParams {
  connId: () => string | undefined
  schema: () => string | undefined
  tableName: () => string | undefined
  effectiveDbType: () => string
  filterExpr: () => string
  sortColumn: () => string
  sortOrder: () => 'ascending' | 'descending' | null
  dataColumns: () => DataColumn[]
  fetchFullData: () => Promise<any>
}

/**
 * Encapsulates data-export logic for the DataBrowser.
 *
 * Supports four formats: xlsx (server-side via /exportXlsxBySql),
 * csv / json / sql (client-side from a full data fetch).
 *
 * The composable keeps an `exporting` flag for button loading state and
 * exposes a single `handleExportCommand(format)` entry point.
 */
export function useDataExport(params: DataExportParams) {
  const exporting = ref(false)

  async function fetchExportFullData() {
    let orderBy = ''
    if (params.sortColumn() && params.sortOrder()) {
      const dir = params.sortOrder() === 'descending' ? 'DESC' : 'ASC'
      orderBy = quoteId(params.sortColumn(), params.effectiveDbType()) + ' ' + dir
    }
    const sql = buildSelectSQL(params.tableName() || '', params.effectiveDbType(), {
      where: params.filterExpr().trim() || undefined,
      orderBy: orderBy || undefined,
    })
    const urlParams = new URLSearchParams()
    urlParams.append('connId', params.connId() || '')
    urlParams.append('schema', params.schema() || '')
    urlParams.append('tableName', params.tableName() || '')
    urlParams.append('sql', sql)
    urlParams.append('maxLine', '100000')
    return await http.post('/execSQL', urlParams)
  }

  function exportToExcel() {
    let orderBy = ''
    if (params.sortColumn() && params.sortOrder()) {
      const dir = params.sortOrder() === 'descending' ? 'DESC' : 'ASC'
      orderBy = quoteId(params.sortColumn(), params.effectiveDbType()) + ' ' + dir
    }
    const sql = buildSelectSQL(params.tableName() || '', params.effectiveDbType(), {
      where: params.filterExpr().trim() || undefined,
      orderBy: orderBy || undefined,
    })
    const urlParams = new URLSearchParams()
    urlParams.append('connId', params.connId() || '')
    urlParams.append('schema', params.schema() || '')
    urlParams.append('filename', params.tableName() || '')
    urlParams.append('sql', sql)
    exporting.value = true
    http
      .post('/exportXlsxBySql', urlParams, { responseType: 'blob' })
      .then((res) => {
        const contentType = (res.headers['content-type'] as string) || ''
        if (contentType.includes('application/json')) {
          ElMessage({ message: '导出失败', type: 'error' })
          return
        }
        const blob = new Blob([res.data], {
          type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
        })
        const url = window.URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = (params.tableName() || 'export') + '.xlsx'
        document.body.appendChild(a)
        a.click()
        document.body.removeChild(a)
        window.URL.revokeObjectURL(url)
      })
      .catch(() => ElMessage({ message: '导出失败', type: 'error' }))
      .finally(() => (exporting.value = false))
  }

  async function handleExportCommand(format: ExportFormat, loadingRef?: Ref<boolean>) {
    if (format === 'xlsx') {
      exportToExcel()
      return
    }

    if (loadingRef) loadingRef.value = true
    try {
      const resp = await fetchExportFullData()
      if (!resp) return

      const exportRows = resp.data?.data?.data ?? []
      const cols = params.dataColumns().map((c) => c.name)
      const comments = params.dataColumns().map((c) => c.comment)
      const tableName = params.tableName() || 'export'

      if (format === 'csv') {
        exportToCsv(cols, exportRows, tableName, comments)
      } else if (format === 'json') {
        exportToJson(exportRows, tableName)
      } else if (format === 'sql') {
        const sqlText = exportToSql(cols, exportRows, tableName, params.effectiveDbType())
        downloadBlob(sqlText, tableName + '.sql', 'text/plain')
      }
      ElMessage({ message: '导出成功', type: 'success' })
    } catch (err) {
      console.error('[DataBrowser] 导出失败:', err)
      ElMessage({ message: '导出失败', type: 'error' })
    } finally {
      if (loadingRef) loadingRef.value = false
    }
  }

  return {
    exporting,
    handleExportCommand,
    exportToExcel,
  }
}
