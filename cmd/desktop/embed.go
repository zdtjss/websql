package main

import "embed"

// staticFS 嵌入前端构建产物（index.html + assets/ + favicon.ico）。
// 构建脚本会在 go build 前将 web-src/dist/ 复制到 cmd/desktop/static/。
//
//go:embed all:static
var staticFS embed.FS

// configData 嵌入桌面版专用配置，确保 isRemote=false、https.enable=false。
//
//go:embed config.desktop.json
var configData []byte

// migrationFS 嵌入管理库增量迁移脚本。
// 构建脚本会在 go build 前将项目根 migrations/sqlite/ 复制到 cmd/desktop/migrations/sqlite/。
//
//go:embed migrations/sqlite/*.sql
var migrationFS embed.FS

// fullInitSQL 嵌入 SQLite 全量初始化脚本，用于全新库快速建库。
// 构建脚本会在 go build 前将项目根 migrations/full/sqlite_full.sql 复制到 cmd/desktop/migrations/full/。
//
//go:embed migrations/full/sqlite_full.sql
var fullInitSQL []byte
