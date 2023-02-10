import { MySQL, PLSQL, StandardSQL } from '@codemirror/lang-sql';

const schemaStore = JSON.parse(localStorage.getItem("go-web-sql-dbSchemaProxy") || "{}")

export const dbSchemaProxy = {
    callback: [],
    registLsn(callback) {
        this.callback.push(callback)
    },
    addTable(schema, dbType, names) {
        const tables = names.map(n => {
            return {
                label: n.label,
                info: n.data.text
            }
        })
        if (schema.endsWith("_col")) {
            const currentSchema = this.schemaProxy[schema.substring(0, schema.length - 4)]
            currentSchema.push(...tables)
            this.schemaProxy[schema] = { tables: currentSchema, dbType: dbType }
        } else {
            this.schemaProxy[schema] = { tables: tables, dbType: dbType }
        }
        localStorage.setItem("go-web-sql-dbSchemaProxy", JSON.stringify(this.schemaProxy))
    },
    getTable(schema) {
        let schemas = this.schemaProxy[schema]['tables']
        const schemaNames = Object.keys(this.schemaProxy).map(n => {
            return {
                label: n,
            }
        })
        if (schemas) {
            schemas.push(...schemaNames)
        }
        return schemas
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
    getAll() {
        const schema = {}
        Object.keys(this.schemaProxy).forEach(key => schema[key] = this.schemaProxy[key]["tables"])
        return schema
    },
    cleanCache() {
        localStorage.removeItem("go-web-sql-dbSchemaProxy")
    },
    schemaProxy: new Proxy(schemaStore, {
        set: (target, property, value) => {
            Reflect.set(target, property, value);
            for (const callback of dbSchemaProxy.callback) {
                if (property.endsWith("_col")) {
                    callback(property.substring(0, property.length - 4))
                } else {
                    callback(property)
                }
            }
            return true
        }
    })
}
