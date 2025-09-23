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

http.interceptors.response.use((response) => {
    if (response.data.code === 500) {
        ElMessage({ message: response.data.msg, type: "error" })
        return Promise.reject(new Error(response.data.msg || "业务错误"));
    }
    return response;
}, (error) => {
    if (error.response && error.response.data) {
        ElMessage.error(error.response.data.msg)
    } else {
        ElMessage.error(error.message)
    }
    return Promise.reject(error);
});

export default http
