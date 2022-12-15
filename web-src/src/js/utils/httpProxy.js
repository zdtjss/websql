import axios from 'axios';

const http = axios.create({
    timeout: 1000
});

/**
 * @method httpProxy
 * @param {string} url{string} api地址 data{object} 参数 ContentType{boole} post拼接路径 responseType{boole}下载文件流 londingstatus{boole}加载遮罩
 * @param {string} [method] {@link module:constants/http method}
 * */

export default (url, method = 'GET', data = {}) => {
    let targetUrl = "http://localhost:8089" + url;
    const options = {
        url: targetUrl,
        method,
        params: data,
        headers: {},
        timeout: 1000 * 60 * 20
    };

    if (method !== 'GET') {
        options["params"] = data.query
        if (!data.query) {
            options["data"] = data
        }
    }

    return http(options);
};
