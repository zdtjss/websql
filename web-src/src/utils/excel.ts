/* 导出excel文件 */

/**
 * 导出excel文件实现思路分析
 *
 * 1.通过XLSX插件的 XLSX.utils.book_new()方法，创建excel工作蒲对象wb。
 * 2.按需插入第一行数据，通过数组的unshift()方法。
 * 3.通过XLSX.utils.json_to_sheet(),创建excel表格对象ws。
 * 4.通过json_to_array(key，data),结合自定义的字段名key，和数据记录data，生成新数组。
 * 5.通过auto_width(),对ws和新生成的数组，自动计算各列col宽。
 * 6.通过XLSX.utils.book_append_sheet(),生成实际excel工作蒲，并使用XLSX.writeFile()生成excel文件。
 */

import * as XLSX from 'xlsx'

/** exportJsonToExcel 的参数选项 */
export interface ExportJsonToExcelOptions {
  /** 表头（对象或数组，会作为第一行数据插入） */
  header?: Record<string, unknown> | string[]
  /** 标题（会居中显示），即excel表格第一行 */
  title?: string
  /** 字段名列表 */
  key: string[]
  /** 表体数据 */
  data: any[]
  /** 文件名 */
  filename: string
  /** 是否自动根据key自定义列宽度 */
  autoWidth: boolean
}

// 自动计算col列宽
function auto_width(ws: XLSX.WorkSheet, data: any[][]): void {
  /*set worksheet max width per col*/
  const colWidth = data.map(row => row.map(val => {
    /*if null/undefined*/
    if (val == null) {
      return { 'wch': 10 }
    }
    const s = String(val)
    /*if chinese*/
    if (s.charCodeAt(0) > 255) {
      return { 'wch': s.length * 2 }
    } else {
      return { 'wch': s.length }
    }
  }))
  /*start in the first row*/
  let result = colWidth[0]
  for (let i = 1; i < colWidth.length; i++) {
    for (let j = 0; j < colWidth[i].length; j++) {
      if (result[j]['wch'] < colWidth[i][j]['wch']) {
        result[j]['wch'] = colWidth[i][j]['wch']
      }
    }
  }
  ws['!cols'] = result as XLSX.ColInfo[]
}

// 将json数据转换成数组
function json_to_array(key: string[], jsonData: any[]): any[][] {
  return jsonData.map(v => key.map(j => {
    return v[j]
  }))
}

/**
 * 导出 JSON 数据到 Excel 文件
 * @param options 导出选项
 */
export const exportJsonToExcel = ({ header, title, key, data, filename, autoWidth }: ExportJsonToExcelOptions): void => {
  const wb = XLSX.utils.book_new()
  if (header) {
    data.unshift(header)
  }
  if (title) {
    data.unshift(title)
  }
  const ws = XLSX.utils.json_to_sheet(data, {
    header: key,
    skipHeader: true
  })
  if (autoWidth) {
    const arr = json_to_array(key, data)
    auto_width(ws, arr)
  }
  XLSX.utils.book_append_sheet(wb, ws, "Sheet1")
  XLSX.writeFile(wb, filename + '.xlsx')
}

export default {
  exportJsonToExcel
}
