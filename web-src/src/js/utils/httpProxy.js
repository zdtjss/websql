import axios from 'axios';

const env = import.meta.env

const http = axios.create({
    timeout: 1000 * 30
});

http.interceptors.request.use((config) => {
    config.url = env.VITE_API_URL + config.url
    return config
});

export default http
