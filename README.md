# Web-SQL
    
web版数据库管理工具，由go语言编写，可以做到无依赖跨平台，编译后无需安装运行环境，支持本地模式和远程部署模式，支持基于操作系统的人脸/指纹识别登录，可以满足个人和企业需求。开源协议宽松，可以自由使用。


## 演示地址 http://124.221.221.247:8090
  管理员账号：admin/1
  指纹/人脸识别只在用户支持且https下证书有效或http下使用localhost访问才被支持。

![1.png](1.png)

## 运行参数
  -port 运行端口号，默认80

  -https 是否为https，默认false

  -remote 是否为远程模式，默认false。远程模式下有严格的权限管理，也有会话管理，适合远程、多实例部署。false下没有权限管理，仅支持本机使用。

## 配置文件
  文件名：config.json

```
{
    // 详情参考https://pkg.go.dev/modernc.org/sqlite
    // https://pkg.go.dev/github.com/go-sql-driver/mysql
    // https://pkg.go.dev/github.com/sijms/go-ora/v2
    "db": {
        "type": "sqlite",  // sqlite、 mysql、oracle（oracle暂时只有sql相关操作靠谱）
        "dsn": "nway.sqlite3.db"    // sqlite：数据库文件路径；mysql：user:password@tcp(host:port)/db?params
    },
    // 详情参考 https://pkg.go.dev/github.com/redis/go-redis/v9
    "redis": {
        "addr": "", // host:port
        "password":"",
        "db": 0
    }
}
```

# 构建自己的Docker镜像
```
FROM zdtjss/websql:latest

COPY ./config.json .
```