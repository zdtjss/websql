import { ref } from 'vue'
import { defineStore } from 'pinia'
import { MySQL, PLSQL, StandardSQL } from '@codemirror/lang-sql'

interface ColumnInfo {
    label: string
    info: string
    type: string
}

interface TableEntry {
    self: { label: string; type: string; detail: string }
    children: ColumnInfo[]
}

interface SchemaEntry {
    tables: Record<string, TableEntry>
    dbType: string
    connId: string
}

type SchemaProxy = Record<string, SchemaEntry>

export const useDbSchemaStore = defineStore('dbSchema', () => {
    const callback = ref<((property: string) => void)[]>([])
    const schemaProxy = ref<SchemaProxy>(
        new Proxy(JSON.parse(localStorage.getItem("go-web-sql-dbSchemaProxy") || "{}") as SchemaProxy, {
            set: (target, property, value) => {
                Reflect.set(target, property, value)
                for (const cb of callback.value) {
                    cb(property as string)
                }
                return true
            }
        })
    )

    function registLsn(cb: (property: string) => void) {
        callback.value.push(cb)
    }

    function addTable(schema: string, dbType: string, names: any[], connId: string = '') {
        const tableObj: Record<string, TableEntry> = {}
        names.forEach(n => {
            if (!n || !n.data) return
            const columnsInfo: ColumnInfo[] = []
            const columns = n.data.columns || []
            for (let i = 0; i < columns.length; i++) {
                const col = columns[i]
                if (!col) continue
                columnsInfo.push({
                    label: col.name || '',
                    info: col.comment || '',
                    type: "property"
                })
            }
            tableObj[n.label] = { self: { label: n.label, type: n.type || "table", detail: n.data.text || '' }, children: columnsInfo }
        })
        schemaProxy.value[schema] = { tables: tableObj, dbType: dbType, connId: connId }
        try {
            localStorage.setItem("go-web-sql-dbSchemaProxy", JSON.stringify(schemaProxy.value))
        } catch (e) {
            console.warn('dbSchema cache save failed', e)
        }
    }

    function getTable(schema: string) {
        const entry = schemaProxy.value[schema]
        if (!entry) return []
        let schemas = entry['tables']
        if (!schemas) return []
        const schemaNames = Object.keys(schemas).map(n => {
            return {
                label: n,
            }
        })
        return schemaNames
    }

    function getDbType(schema: string) {
        const entry = schemaProxy.value[schema]
        return entry ? entry.dbType : null
    }

    function getConnId(schema: string) {
        const entry = schemaProxy.value[schema]
        return entry ? (entry.connId || '') : ''
    }

    function getDialect(schema: string) {
        const entry = schemaProxy.value[schema]
        if (!entry) return StandardSQL
        const dbType = entry["dbType"]
        if (dbType === "mysql" || dbType === "mariadb") {
            // MariaDB 与 MySQL 方言兼容，统一使用 MySQL 语法高亮
            return MySQL
        } else if (dbType === "oracle") {
            return PLSQL
        }
        return StandardSQL
    }

    function getAll(schemaName: string) {
        const entry = schemaProxy.value[schemaName]
        if (!entry) return {}
        return entry["tables"] || {}
    }

    function cleanCache() {
        localStorage.removeItem("go-web-sql-dbSchemaProxy")
    }

    return {
        callback,
        schemaProxy,
        registLsn,
        addTable,
        getTable,
        getDbType,
        getConnId,
        getDialect,
        getAll,
        cleanCache
    }
})
