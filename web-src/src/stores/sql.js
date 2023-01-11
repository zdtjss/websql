const schemaStore = {}

export const dbSchemaProxy = {
    callback: [],
    registLsn(callback) {
        this.callback.push(callback)
    },
    addTable(schema, names) {
        const tables = names.map(n => {
            return {
                label: n.label,
                info: n.data.text
            }
        })
        debugger
        if (schema.endsWith("_col")) {
            const currentSchema = this.schemaProxy[schema.substring(0, schema.length - 4)]
            currentSchema.push(...tables)
            this.schemaProxy[schema] = currentSchema
        } else {
            this.schemaProxy[schema] = tables
        }
    },
    getTable(schema) {
        let schemas = this.schemaProxy[schema]
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
    getAll() {
        return this.schemaProxy
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
