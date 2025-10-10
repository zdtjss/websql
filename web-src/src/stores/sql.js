import { MySQL, PLSQL, StandardSQL } from '@codemirror/lang-sql';

const schemaStore = JSON.parse(localStorage.getItem("go-web-sql-dbSchemaProxy") || "{}")

export const dbSchemaProxy = {
    callback: [],
    registLsn(callback) {
        this.callback.push(callback)
    },
    addTable(schema, dbType, names) {
        const tableObj = {}
        names.forEach(n => {
            const columnsInfo = []
            for (let i = 0; i < n.data.columns.length; i++) {
                const col = n.data.columns[i]
                columnsInfo.push({
                    label: col.name,
                    info: col.comment,
                    type: "property"
                })
            }
            tableObj[n.label] = { self: { label: n.label, type: "table", detail: n.data.text }, children: columnsInfo }
        })
        this.schemaProxy[schema] = { tables: tableObj, dbType: dbType }
        localStorage.setItem("go-web-sql-dbSchemaProxy", JSON.stringify(this.schemaProxy))
    },
    getTable(schema) {
        let schemas = this.schemaProxy[schema]['tables']
        const schemaNames = Object.keys(schemas).map(n => {
            return {
                label: n,
            }
        })
        return schemaNames
    },
    getDbType(schema) {
        return this.schemaProxy[schema]["dbType"]
    },
    getDialect(schema) {
        const dbType = this.schemaProxy[schema]["dbType"]
        if (dbType === "mysql") {
            return MySQL
        } else if (dbType === "oracle") {
            return PLSQL
        }
        return StandardSQL
    },
    getAll(schemaName) {
        return this.schemaProxy[schemaName]["tables"]
    },
    cleanCache() {
        localStorage.removeItem("go-web-sql-dbSchemaProxy")
    },
    schemaProxy: new Proxy(schemaStore, {
        set: (target, property, value) => {
            Reflect.set(target, property, value);
            for (const callback of dbSchemaProxy.callback) {
                callback(property)
            }
            return true
        }
    })
}
