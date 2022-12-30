import { defineStore } from 'pinia'

export const useDBStore = defineStore('db', {
    state: () => ({ schema: {}, schemaNames: [] }),
    actions: {
        addTable(schema, names) {
            const tables = names.map(n => {
                return {
                    label: n
                }
            })
            this.schema[schema] = tables
        },
        getTable(schema) {
            let schemas = this.schema[schema]
            const schemaNames = Object.keys(this.schema).map(n => {
                return {
                    label: n
                }
            })
            if (schemas) {
                schemas.push(...schemaNames)
            }
            return schemas
        },
        getAll() {
            return this.schema
        }
    }
});
