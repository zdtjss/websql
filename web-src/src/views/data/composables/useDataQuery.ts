import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import http from '@/api/index'
import { useDbSchemaStore } from '@/stores/dbSchema'
import { buildCountSQL, buildSelectSQL, quoteId } from '@/utils/sqlHelper.ts'
import type { Ref } from 'vue'

export interface DataColumn {
  name: string
  comment: string
  type: string
}

export interface DataQueryParams {
  connId: () => string | undefined
  schema: () => string | undefined
  tableName: () => string | undefined
  dbType: () => string | undefined
  schemaPath: () => string | undefined
}

/**
 * Encapsulates all data-query business logic for the DataBrowser:
 * - DB type resolution
 * - Pagination state
 * - Loading rows / total count
 * - Sorting
 * - Filter expression + per-column filter conditions
 * - Primary key inference / row key generation
 *
 * The composable owns the canonical `rows`, `dataColumns`, `pkColumns`,
 * `filterExpr`, `sortColumn`, `sortOrder` and pagination refs. Other
 * components (inline edit, range selection, export) consume these refs
 * and may mutate `rows` in place (e.g. for inline edits / new rows).
 */
export function useDataQuery(params: DataQueryParams) {
  const dbSchemaProxy = useDbSchemaStore()
  const resolvedDbType = ref('')
  const effectiveDbType = computed(
    () =>
      params.dbType() ||
      dbSchemaProxy.getDbType(params.schema() || '') ||
      resolvedDbType.value ||
      '',
  )

  const loading = ref(false)
  const currentPage = ref(1)
  const pageSize = ref(20)
  const total = ref(0)
  const dataColumns: Ref<DataColumn[]> = ref([])
  const rows: Ref<Record<string, any>[]> = ref([])

  // Filter & sort state
  const filterExpr = ref('')
  const sortColumn = ref('')
  const sortOrder = ref<'ascending' | 'descending' | null>(null)

  // Primary key columns (inferred from query result keys or fallback to first column)
  const pkColumns: Ref<string[]> = ref([])

  // Per-column filter conditions: { [colName]: { operator, value } }
  const columnFilterConditions = ref<Record<string, { operator: string; value: string }>>({})

  async function resolveDbType() {
    if (params.dbType() || dbSchemaProxy.getDbType(params.schema() || '')) return
    try {
      const resp = await http.get('/listConn2', { params: { pageSize: 1000 } })
      const result = (resp.data && resp.data.data ? resp.data.data : resp.data) || {}
      const connList = result.data || []
      const conn = connList.find(
        (c: any) => String(c.id) === String(params.connId()),
      )
      if (conn && conn.dbType) {
        resolvedDbType.value = conn.dbType
      }
    } catch {
      /* ignore */
    }
  }

  function getRowKey(row: Record<string, any>): string {
    if (row._rowUid) return '_new_' + row._rowUid
    if (pkColumns.value.length > 0) {
      return pkColumns.value.map((k) => row[k]).join('_')
    }
    return JSON.stringify(row)
  }

  function isDateColumn(colName: string): boolean {
    const col = dataColumns.value.find((c) => c.name === colName)
    if (!col || !col.type) return false
    const upper = col.type.toUpperCase()
    return (
      upper === 'DATETIME' ||
      upper === 'DATE' ||
      upper === 'TIMESTAMP' ||
      upper === 'TIMESTAMP(6)' ||
      upper.includes('TIMESTAMP') ||
      upper === 'TIMESTAMPTZ' ||
      upper === 'TIMESTAMPLTZ'
    )
  }

  async function fetchTotal() {
    const sql = buildCountSQL(
      params.tableName() || '',
      effectiveDbType.value,
      filterExpr.value.trim() || undefined,
    )
    const urlParams = new URLSearchParams()
    urlParams.append('connId', params.connId() || '')
    urlParams.append('schema', params.schema() || '')
    urlParams.append('tableName', params.tableName() || '')
    urlParams.append('sql', sql)
    urlParams.append('maxLine', '1')
    const resp = await http.post('/execSQL', urlParams)
    const data = resp.data.data
    if (data && data.data && data.data.length > 0) {
      const firstRow = data.data[0]
      const firstValue = Object.values(firstRow)[0]
      total.value = Number(firstValue ?? 0)
    }
  }

  async function fetchData() {
    const offset = (currentPage.value - 1) * pageSize.value
    let orderBy = ''
    if (sortColumn.value && sortOrder.value) {
      const dir = sortOrder.value === 'descending' ? 'DESC' : 'ASC'
      orderBy = quoteId(sortColumn.value, effectiveDbType.value) + ' ' + dir
    }
    const sql = buildSelectSQL(params.tableName() || '', effectiveDbType.value, {
      where: filterExpr.value.trim() || undefined,
      orderBy: orderBy || undefined,
      limit: pageSize.value,
      offset: offset,
    })
    const urlParams = new URLSearchParams()
    urlParams.append('connId', params.connId() || '')
    urlParams.append('schema', params.schema() || '')
    urlParams.append('tableName', params.tableName() || '')
    urlParams.append('sql', sql)
    const resp = await http.post('/execSQL', urlParams)
    const data = resp.data.data

    if (data && data.columns) {
      dataColumns.value = data.columns
        .filter((col: any) => col.name !== 'RN')
        .map((col: any) => ({
          name: col.name,
          comment: col.comment || '',
          type: col.type || '',
        }))
      if (data.keys && data.keys.length > 0) {
        pkColumns.value = data.keys
      } else {
        const colNames = dataColumns.value.map((c) => c.name)
        const idCol = colNames.find((n) => n.toLowerCase() === 'id')
        pkColumns.value = idCol ? [idCol] : colNames.slice(0, 1)
      }
    }

    const rawRows = data?.data ?? []
    rows.value = rawRows.map((row: Record<string, any>) => {
      const filtered = { ...row }
      delete filtered.RN
      return filtered
    })
  }

  async function loadData() {
    if (!params.connId() || !params.schema() || !params.tableName()) return
    await resolveDbType()
    loading.value = true
    try {
      await fetchTotal()
      await fetchData()
    } catch (err) {
      console.error('[DataBrowser] 加载数据失败:', err)
      ElMessage({ message: '加载数据失败', type: 'error' })
    } finally {
      loading.value = false
    }
  }

  function onPageChange() {
    loadData()
  }

  function onSizeChange() {
    currentPage.value = 1
    loadData()
  }

  function handleSort(colName: string) {
    if (sortColumn.value === colName) {
      if (sortOrder.value === null) {
        sortOrder.value = 'ascending'
      } else if (sortOrder.value === 'ascending') {
        sortOrder.value = 'descending'
      } else {
        sortColumn.value = ''
        sortOrder.value = null
      }
    } else {
      sortColumn.value = colName
      sortOrder.value = 'ascending'
    }
    loadData()
  }

  function getSortIcon(colName: string) {
    if (sortColumn.value !== colName) {
      return 'Sort'
    }
    return sortOrder.value === 'ascending' ? 'ArrowUp' : 'ArrowDown'
  }

  function isColumnFiltered(colName: string): boolean {
    if (!filterExpr.value.trim()) return false
    const escapedColName = colName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const q =
      effectiveDbType.value === 'mysql' || effectiveDbType.value === 'mariadb'
        ? '`'
        : '"'
    const escapedQ = q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const pattern = new RegExp(
      `${escapedQ}${escapedColName}${escapedQ}|(?<![a-zA-Z0-9_])${escapedColName}(?![a-zA-Z0-9_])`,
      'i',
    )
    return pattern.test(filterExpr.value)
  }

  async function fetchFullData() {
    let orderBy = ''
    if (sortColumn.value && sortOrder.value) {
      const dir = sortOrder.value === 'descending' ? 'DESC' : 'ASC'
      orderBy = quoteId(sortColumn.value, effectiveDbType.value) + ' ' + dir
    }
    const sql = buildSelectSQL(params.tableName() || '', effectiveDbType.value, {
      where: filterExpr.value.trim() || undefined,
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

  function clearAllFilters() {
    filterExpr.value = ''
    columnFilterConditions.value = {}
    currentPage.value = 1
    loadData()
  }

  const rowIndexOffset = computed(
    () => (currentPage.value - 1) * pageSize.value + 1,
  )

  // Reload when the target table changes
  watch(
    () => [params.connId(), params.schema(), params.tableName()],
    () => {
      currentPage.value = 1
      loadData()
    },
  )

  return {
    // db type
    effectiveDbType,
    resolveDbType,
    // query state
    loading,
    currentPage,
    pageSize,
    total,
    dataColumns,
    rows,
    filterExpr,
    sortColumn,
    sortOrder,
    pkColumns,
    columnFilterConditions,
    rowIndexOffset,
    // query methods
    loadData,
    fetchTotal,
    fetchData,
    fetchFullData,
    onPageChange,
    onSizeChange,
    handleSort,
    getSortIcon,
    isColumnFiltered,
    isDateColumn,
    getRowKey,
    clearAllFilters,
  }
}
