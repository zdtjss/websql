import { defineStore } from 'pinia'

const state = defineStore('MYSQLContent', {
    state: () => (['user', 'app_user', 'app_user_user']),
    actions: {
        setMysqlTableContent(state: any, data: any) {
            state.MYSQLContent = { ...state.MYSQLContent, ...data };
        }
    }
});

export const useStore = defineStore('main', {
    // other options...
})