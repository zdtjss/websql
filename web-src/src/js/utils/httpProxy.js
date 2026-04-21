import axios from 'axios';
import { sanitizeError } from '@/utils/errorHandler.js';
import { ElMessage } from 'element-plus';

const env = import.meta.env

const http = axios.create({
    timeout: 1000 * 30 * 60
});

http.interceptors.request.use((config) => {
    config.url = env.VITE_API_URL + config.url
    config.headers['Authorization'] = sessionStorage.getItem("authentication") || ""
    return config
});

http.interceptors.response.use(
    (response) => {
        if (response.config.responseType === 'blob') {
            return response;
        }
        const { code, msg } = response.data;
        if (code === 401) {
            const isLoginExpired = !!sessionStorage.getItem('authentication');
            sessionStorage.removeItem('authentication');
            sessionStorage.removeItem('currentUser');
            sessionStorage.removeItem('isRemote');
            window.dispatchEvent(new CustomEvent('session-expired', { 
                detail: { 
                    message: isLoginExpired ? (msg || '登录已过期，请重新登录') : '' 
                } 
            }));
            return Promise.reject(new Error(''));
        }
        if (code === 500) {
            ElMessage({ message: sanitizeError(msg) || '系统错误', type: 'error' });
            return Promise.reject(new Error(sanitizeError(msg) || '系统错误'));
        }
        return response;
    },
    (error) => {
        if (error.response && error.response.status === 401) {
            const isLoginExpired = !!sessionStorage.getItem('authentication');
            const msg = error.response.data?.msg || '登录已过期，请重新登录';
            sessionStorage.removeItem('authentication');
            sessionStorage.removeItem('currentUser');
            sessionStorage.removeItem('isRemote');
            window.dispatchEvent(new CustomEvent('session-expired', { 
                detail: { 
                    message: isLoginExpired ? msg : '' 
                } 
            }));
            return Promise.reject(error);
        }

        let message = '请求失败';
        if (error.response) {
            const rawMsg = error.response.data?.msg || error.response.statusText || '服务异常';
            message = sanitizeError(rawMsg);
        } else if (error.request) {
            message = '网络异常，请检查网络连接';
        } else {
            message = '请求失败，请稍后重试';
        }
        ElMessage.error(message);
        return Promise.reject(error);
    }
);

export default http
