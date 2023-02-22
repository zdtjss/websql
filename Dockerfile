FROM ubuntu:22.04
WORKDIR /app
COPY ./config.json .
RUN sed -i "s@http://.*archive.ubuntu.com@http://mirrors.ustc.edu.cn@g" /etc/apt/sources.list && sed -i "s@http://.*security.ubuntu.com@http://mirrors.ustc.edu.cn@g" /etc/apt/sources.list && \
    apt-get update -y && apt-get install -y git && apt-get install -y wget && apt-get install -y gcc && \
    wget https://golang.google.cn/dl/go1.20.1.linux-amd64.tar.gz && tar -C /usr/local -xzf go1.20.1.linux-amd64.tar.gz && export PATH=$PATH:/usr/local/go/bin && export GOPROXY=https://proxy.golang.com.cn,direct && \
    wget https://registry.npmmirror.com/-/binary/node/v18.14.1/node-v18.14.1-linux-x64.tar.gz && tar -C /usr/local -zxf node-v18.14.1-linux-x64.tar.gz && export PATH=$PATH:/usr/local/node-v18.14.1-linux-x64/bin && \
    git clone https://gitee.com/nway/websql.git && cd websql && go mod tidy && go build -o /app/WebSql main.go && \
    cd web-src && npm install --registry=https://registry.npmmirror.com && npm run build && mv dist /app/static && cd /app && \
    rm -rf /app/websql && rm -f /app/go1.20.1.linux-amd64.tar.gz && rm -rf /usr/local/go && rm -f /app/node-v18.14.1-linux-x64.tar.gz && rm -rf /usr/local/node-v18.14.1-linux-x64 && apt-get autoremove -y gcc && apt-get autoremove -y git
CMD ["/app/WebSql", "-remote"] 