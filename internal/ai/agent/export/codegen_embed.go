package export

import _ "embed"

// 高级导出沙箱的受信助手源码（编译期嵌入，运行时写入 exports/.codegen/_helpers/）。

//go:embed codegen_helper/sitecustomize.py
var codegenSiteCustomizePy string

//go:embed codegen_helper/websql_db.py
var codegenWebSQLDBPy string