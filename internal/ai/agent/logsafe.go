package agent

// 日志安全：日志只记录长度、哈希与数量，不记录原始 SQL、工具参数或模型输出。
//
// 背景：工具参数可能包含查询到的敏感值、连接串；模型输出可能包含用户数据。
// 一旦写入日志文件，SSE/落库的脱敏都无法覆盖。

import "fmt"

// safeTextSummary 返回文本的安全摘要（长度 + 内容哈希前 16 位）。
func safeTextSummary(text string) string {
	return fmt.Sprintf("len=%d sha=%s", len(text), contentSHA(text))
}

// safeSQLSummary 返回 SQL 的安全摘要。
//
// 与 safeTextSummary 分开命名是为了让调用点语义清晰：SQL 同样是敏感文本，
// 不得记录原文（可能含字面量中的身份证/手机号/密钥）。
func safeSQLSummary(sql string) string {
	return fmt.Sprintf("len=%d sha=%s", len(sql), contentSHA(sql))
}

// safeObjectsSummary 返回对象列表的安全摘要（只记录数量）。
func safeObjectsSummary(objects []string) string {
	return fmt.Sprintf("count=%d", len(objects))
}
