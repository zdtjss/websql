import { defineStore } from 'pinia'

export const useDBStore = defineStore('db', {
    state: () => ({ tableList: [], schema: [] }),
    actions: {
        addTable(names) {
            const tables = names.map(n => {
                return {
                    label: n
                }
            })
            this.tableList.push(...tables)
        }
    }
});
