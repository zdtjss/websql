package export

// OS 级隔离的统一入口：各平台通过 codegen_isolation_<os>.go 提供具体实现。
//
// 语义：CodegenIsolationAvailable 为 false 时，调用方必须 fail-closed
// （不注册高级导出工具），避免在缺乏 OS 级约束的环境暴露临时代码执行。

// CodegenIsolationAvailable 返回当前平台是否存在可用的 OS 级隔离机制。
func CodegenIsolationAvailable() bool { return codegenIsolationAvailable() }

// CodegenIsolationName 返回当前平台隔离机制名称（审计/日志用）。
func CodegenIsolationName() string { return codegenIsolationName() }