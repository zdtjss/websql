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

http.interceptors.response.use(function (response) {
    return response;
}, function (error) {
    if (error.response && error.response.data) {
        ElMessage.error(error.response.data.msg)
    } else {
        ElMessage.error(error.message)
    }
    return Promise.reject(error);
});

export default http
