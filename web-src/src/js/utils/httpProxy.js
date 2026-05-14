import axios from 'axios';
import { sanitizeError } from '@/utils/errorHandler.js';
import { ElMessage } from 'element-plus';

const env = import.meta.env

const http = axios.create({
    timeout: 1000 * 30 * 60
});

let sessionExpiredDispatched = false;

function dispatchSessionExpired(detail) {
    if (sessionExpiredDispatched) {
        return;
    }
    sessionExpiredDispatched = true;
    window.dispatchEvent(new CustomEvent('session-expired', detail));
    setTimeout(() => {
        sessionExpiredDispatched = false;
    }, 3000);
}

const pendingRequests = new Map();

function generateRequestKey(config) {
    const { method, url, params, data } = config;
    return [method, url, JSON.stringify(params), JSON.stringify(data)].join('&');
}

function removePendingRequest(config) {
    const key = generateRequestKey(config);
    if (pendingRequests.has(key)) {
        const cancel = pendingRequests.get(key);
        cancel('canceled_by_duplicate');
        pendingRequests.delete(key);
    }
}

function addPendingRequest(config) {
    const key = generateRequestKey(config);
    if (pendingRequests.has(key)) {
        const cancel = pendingRequests.get(key);
        cancel('canceled_by_duplicate');
        pendingRequests.delete(key);
    }
    config.cancelToken = new axios.CancelToken((cancel) => {
        pendingRequests.set(key, cancel);
    });
}

const throttleMap = new Map();

function throttleRequest(config, minInterval = 300) {
    const key = generateRequestKey(config);
    const now = Date.now();
    const lastTime = throttleMap.get(key) || 0;
    if (now - lastTime < minInterval) {
        return false;
    }
    throttleMap.set(key, now);
    return true;
}

http.interceptors.request.use((config) => {
    config.url = env.VITE_API_URL + config.url
    config.headers['Authorization'] = sessionStorage.getItem("authentication") || ""

    if (config.method === 'post' && config.url.includes('/execSQL')) {
        if (!throttleRequest(config, 200)) {
            return Promise.reject(new axios.Cancel('canceled_by_throttle'));
        }
    }

    if (config.url.includes('/listTableNames') || config.url.includes('/showTree')) {
        if (!throttleRequest(config, 500)) {
            return Promise.reject(new axios.Cancel('canceled_by_throttle'));
        }
    }

    if (config.url.includes('/ai/agent/chatStream')) {
        removePendingRequest(config);
    }

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
            dispatchSessionExpired({ 
                detail: { 
                    message: isLoginExpired ? (msg || '登录已过期，请重新登录') : '' 
                } 
            });
            return Promise.reject(new Error(''));
        }
        if (code === 500) {
            ElMessage({ message: sanitizeError(msg) || '系统错误', type: 'error' });
            return Promise.reject(new Error(sanitizeError(msg) || '系统错误'));
        }
        return response;
    },
    (error) => {
        if (axios.isCancel(error)) {
            return Promise.reject(error);
        }

        if (error.response) {
            const status = error.response.status;

            if (status === 401) {
                const isLoginExpired = !!sessionStorage.getItem('authentication');
                const msg = error.response.data?.msg || '登录已过期，请重新登录';
                sessionStorage.removeItem('authentication');
                sessionStorage.removeItem('currentUser');
                sessionStorage.removeItem('isRemote');
                dispatchSessionExpired({ 
                    detail: { 
                        message: isLoginExpired ? msg : '' 
                    } 
                });
                return Promise.reject(error);
            }

            if (status === 429) {
                ElMessage.warning('请求过于频繁，请稍后再试');
                return Promise.reject(error);
            }

            if (status === 503) {
                ElMessage.warning('服务暂时不可用，请稍后重试');
                return Promise.reject(error);
            }

            const rawMsg = error.response.data?.msg || error.response.statusText || '服务异常';
            ElMessage.error(sanitizeError(rawMsg));
        } else if (error.request) {
            ElMessage.error('网络异常，请检查网络连接');
        } else {
            ElMessage.error('请求失败，请稍后重试');
        }
        return Promise.reject(error);
    }
);

export default http
