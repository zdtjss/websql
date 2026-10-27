FROM ubuntu:22.04
WORKDIR /app
COPY ./config.json .
# python3 / python3-pip：AI 对话中的 docx/pdf 文档解析依赖（internal/ai/agent/fileparse
# 以子进程调用第一方解析脚本）；未安装时 docx 会回退内置 Go 解析、pdf 明确提示不支持。
RUN sed -i "s@http://.*archive.ubuntu.com@http://mirrors.ustc.edu.cn@g" /etc/apt/sources.list && sed -i "s@http://.*security.ubuntu.com@http://mirrors.ustc.edu.cn@g" /etc/apt/sources.list && \
    apt-get update -y && apt-get install -y git && apt-get install -y wget && apt-get install -y gcc && apt-get install -y python3 && apt-get install -y python3-pip && \
    wget https://golang.google.cn/dl/go1.26.0.linux-amd64.tar.gz && tar -C /usr/local -xzf go1.26.0.linux-amd64.tar.gz && export PATH=$PATH:/usr/local/go/bin && export GOPROXY=https://proxy.golang.com.cn,direct && \
    wget https://registry.npmmirror.com/-/binary/node/v22.14.0/node-v22.14.0-linux-x64.tar.gz && tar -C /usr/local -zxf node-v22.14.0-linux-x64.tar.gz && export PATH=$PATH:/usr/local/node-v22.14.0-linux-x64/bin && \
    git clone https://gitee.com/nway/websql.git && cd websql && CGO_ENABLED=0 go build -o /app/WebSql main.go && \
    cd web-src && npm install --registry=https://registry.npmmirror.com && npm run build && mv dist /app/static && cd /app && \
    pip3 install --no-cache-dir --index-url https://pypi.tuna.tsinghua.edu.cn/simple python-docx==1.2.0 pdfplumber==0.11.7 && \
    rm -rf /app/websql && rm -f /app/go1.26.0.linux-amd64.tar.gz && rm -rf /usr/local/go && rm -f /app/node-v22.14.0-linux-x64.tar.gz && rm -rf /usr/local/node-v22.14.0-linux-x64 && apt-get autoremove -y gcc && apt-get autoremove -y git && \
    mkdir -p /app/data /app/logs
EXPOSE 80
CMD ["/app/WebSql", "-port", "80"]