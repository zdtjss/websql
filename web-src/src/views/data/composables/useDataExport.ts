import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import http from '@/api/index'
import { buildSelectSQL, quoteId } from '@/utils/sqlHelper.ts'
import type { Ref } from 'vue'

export type ExportFormat = 'xlsx' | 'csv' | 'json' | 'sql'

export interface DataExportParams {
  connId: () => string | undefined
  schema: () => string | undefined
  tableName: () => string | undefined
  effectiveDbType: () => string
  filterExpr: () => string
  sortColumn: () => string
  sortOrder: () => 'ascending' | 'descending' | null
}

/**
 * DataBrowser 数据导出。
 *
 * 四种格式全部走服务端流式导出：
 *   - xlsx → /exportXlsxBySql（excelize StreamWriter）
 *   - csv/json/sql → /exportText（逐行读、逐行写响应）
 * 前端只接收文件流，不再把最多 10 万行拉进浏览器再拼文件（计划 2.4）。
 */
export function useDataExport(params: DataExportParams) {
  const exporting = ref(false)

  /** 按当前筛选与排序生成导出 SQL */
  function buildExportSql(): string | null {
    const table = params.tableName() || ''
    if (!table) {
      ElMessage({ message: '请先选择要导出的表', type: 'warning' })
      return null
    }
    let orderBy = ''
    if (params.sortColumn() && params.sortOrder()) {
      const dir = params.sortOrder() === 'descending' ? 'DESC' : 'ASC'
      orderBy = quoteId(params.sortColumn(), params.effectiveDbType()) + ' ' + dir
    }
    return buildSelectSQL(table, params.effectiveDbType(), {
      where: params.filterExpr().trim() || undefined,
      orderBy: orderBy || undefined,
    })
  }

  function buildForm(sql: string): URLSearchParams {
    const body = new URLSearchParams()
    body.append('connId', params.connId() || '')
    body.append('schema', params.schema() || '')
    body.append('sql', sql)
    body.append('filename', params.tableName() || 'export')
    return body
  }

  function saveBlob(data: BlobPart, filename: string, mime: string) {
    const blob = new Blob([data], { type: mime })
    const url = window.URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    window.URL.revokeObjectURL(url)
  }

  async function runExport(format: ExportFormat, loadingRef?: Ref<boolean>) {
    const sql = buildExportSql()
    if (!sql) return

    const body = buildForm(sql)
    const isXlsx = format === 'xlsx'
    if (!isXlsx) body.append('format', format)

    exporting.value = true
    if (loadingRef) loadingRef.value = true
    try {
      const res = await http.post(isXlsx ? '/exportXlsxBySql' : '/exportText', body, {
        responseType: 'blob',
      })
      const contentType = (res.headers['content-type'] as string) || ''
      // 后端出错时会退化为 JSON 错误体，需拦截而不是下载一个坏文件
      if (contentType.includes('application/json')) {
        ElMessage({ message: '导出失败，请稍后重试', type: 'error' })
        return
      }
      const name = params.tableName() || 'export'
      const ext = isXlsx ? 'xlsx' : format
      const mime = isXlsx
        ? 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
        : contentType || 'application/octet-stream'
      saveBlob(res.data, `${name}.${ext}`, mime)
      ElMessage({ message: '导出成功', type: 'success' })
    } catch (err) {
      console.error('[DataBrowser] 导出失败:', err)
      ElMessage({ message: '导出失败', type: 'error' })
    } finally {
      exporting.value = false
      if (loadingRef) loadingRef.value = false
    }
  }

  function exportToExcel() {
    return runExport('xlsx')
  }

  function handleExportCommand(format: ExportFormat, loadingRef?: Ref<boolean>) {
    return runExport(format, loadingRef)
  }

  return {
    exporting,
    handleExportCommand,
    exportToExcel,
  }
}
