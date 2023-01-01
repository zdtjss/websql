import axios from 'axios';

const env = import.meta.env

const http = axios.create({
    timeout: 1000 * 30
});

http.interceptors.request.use((config) => {
    config.url = env.VITE_API_URL + config.url
    return config
});

http.interceptors.response.use(function (response) {
    return response;
}, function (error) {
    ElMessage({ message: error.response.data.msg, type: "error" })
    return Promise.reject(error);
});

export default http
