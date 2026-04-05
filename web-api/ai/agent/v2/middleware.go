// Package agentv2 基于 Eino ADK 重构的 AI SQL 智能体 v2。
package agentv2

import (
	"context"
	"encoding/json"
	"log"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// contextKey 用于在 context 中传递危险 SQL 信息
type contextKey string

const dangerousSQLKey contextKey = "dangerous_sql"

// SQLSecurityMiddleware SQL 安全中间件
// 在工具调用前拦截并检测危险 SQL，实现提前干预
type SQLSecurityMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

// WrapInvokableToolCall 包装可调用工具的实现
// 这是关键：在工具实际执行前拦截输入参数
func (m *SQLSecurityMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		// 只拦截 exec_sql 工具
		if tCtx.Name == "exec_sql" {
			log.Printf("[SQLSecurityMiddleware] 拦截到 exec_sql 工具调用\n")
			
			// 解析输入参数
			var input ExecInput
			if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
				log.Printf("[SQLSecurityMiddleware] 解析参数失败 - err=%v\n", err)
				return "", err
			}
			log.Printf("[SQLSecurityMiddleware] SQL 内容 - sql=%s\n", input.SQL)

			// 检测危险 SQL
			if isDangerousSQL(input.SQL) {
				log.Printf("[SQLSecurityMiddleware] 检测到危险 SQL - sql=%s\n", input.SQL)
				// 将危险 SQL 信息存储到 context 中
				ctx = context.WithValue(ctx, dangerousSQLKey, input.SQL)
				// 返回特定错误，这个错误会被 event.Err 捕获
				return "", &DangerousSQLError{
					SQL: input.SQL,
				}
			}

			// 非危险 SQL 但也需要确认（如普通 INSERT/UPDATE）
			if needsConfirmation(input.SQL) {
				log.Printf("[SQLSecurityMiddleware] 非危险 SQL 但需要确认 - sql=%s\n", input.SQL)
				ctx = context.WithValue(ctx, dangerousSQLKey, input.SQL)
				return "", &DangerousSQLError{
					SQL: input.SQL,
				}
			}
			log.Printf("[SQLSecurityMiddleware] SQL 检查通过，允许执行\n")
		}

		// 调用原始工具
		return endpoint(ctx, argumentsInJSON, opts...)
	}, nil
}

// WrapStreamableToolCall 包装流式工具调用（如果需要）
func (m *SQLSecurityMiddleware) WrapStreamableToolCall(
	ctx context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	// 对于流式工具调用，同样进行拦截
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		// 只拦截 exec_sql 工具
		if tCtx.Name == "exec_sql" {
			log.Printf("[SQLSecurityMiddleware:Stream] 拦截到 exec_sql 流式工具调用\n")
			
			// 解析输入参数
			var input ExecInput
			if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
				log.Printf("[SQLSecurityMiddleware:Stream] 解析参数失败 - err=%v\n", err)
				return nil, err
			}
			log.Printf("[SQLSecurityMiddleware:Stream] SQL 内容 - sql=%s\n", input.SQL)

			// 检测危险 SQL
			if isDangerousSQL(input.SQL) {
				log.Printf("[SQLSecurityMiddleware:Stream] 检测到危险 SQL - sql=%s\n", input.SQL)
				ctx = context.WithValue(ctx, dangerousSQLKey, input.SQL)
				return nil, &DangerousSQLError{
					SQL: input.SQL,
				}
			}

			// 非危险 SQL 但也需要确认
			if needsConfirmation(input.SQL) {
				log.Printf("[SQLSecurityMiddleware:Stream] 非危险 SQL 但需要确认 - sql=%s\n", input.SQL)
				ctx = context.WithValue(ctx, dangerousSQLKey, input.SQL)
				return nil, &DangerousSQLError{
					SQL: input.SQL,
				}
			}
			log.Printf("[SQLSecurityMiddleware:Stream] SQL 检查通过，允许执行\n")
		}

		// 调用原始工具
		return endpoint(ctx, argumentsInJSON, opts...)
	}, nil
}

// needsConfirmation 检查 SQL 是否需要用户确认
// 扩展逻辑：可以根据需要添加更多规则
func needsConfirmation(sql string) bool {
	// 当前所有写操作都需要确认
	// 未来可以扩展：例如检查是否包含 WHERE 条件、影响行数等
	return true
}

// getDangerousSQLFromContext 从 context 中获取危险 SQL 信息
func getDangerousSQLFromContext(ctx context.Context) string {
	if sql, ok := ctx.Value(dangerousSQLKey).(string); ok {
		return sql
	}
	return ""
}
