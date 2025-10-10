import axios from 'axios';

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
        // 假设后端总是返回 HTTP 200，用 data.code 表示业务状态
        const { code, msg } = response.data;
        if (code === 500) {
            ElMessage({ message: msg || '系统错误', type: 'error' });
            return Promise.reject(new Error(msg || '系统错误'));
        }
        return response;
    },
    (error) => {
        let message = '请求失败';
        if (error.response) {
            // 服务器返回了响应（但状态码非 2xx）
            message = error.response.data?.msg || error.response.statusText || '服务异常';
        } else if (error.request) {
            // 请求已发出但无响应（如网络错误）
            message = '网络异常，请检查网络连接';
        } else {
            // 其他错误（如配置错误）
            message = error.message || '未知错误';
        }
        ElMessage.error(message);
        return Promise.reject(error);
    }
);

export default http
