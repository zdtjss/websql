package agentv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// DangerousSQLError 危险 SQL 错误（触发前端确认流程）
// 支持多条 SQL 同时拦截
type DangerousSQLError struct {
	SQL     string   // 兼容单条
	SQLList []string // 多条 SQL
}

func (e *DangerousSQLError) Error() string {
	if len(e.SQLList) > 0 {
		return fmt.Sprintf("DANGEROUS_SQL_MULTI:%s", strings.Join(e.SQLList, ";\n"))
	}
	return fmt.Sprintf("DANGEROUS_SQL:%s", e.SQL)
}

// isDangerousSQL 检查 SQL 是否为写操作（所有写操作都需要用户确认）
func isDangerousSQL(sql string) bool {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	patterns := []string{
		"DROP ", "TRUNCATE ", "DELETE ",
		"ALTER ", "CREATE ", "REPLACE ",
		"INSERT ", "UPDATE ", "MERGE ",
	}
	for _, p := range patterns {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}
	return false
}

// SQLSecurityMiddleware SQL 安全中间件 - 拦截所有写操作
type SQLSecurityMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func (m *SQLSecurityMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if tCtx.Name != "exec_sql" {
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		var input ExecInput
		if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
			return "", err
		}

		log.Printf("[SQLSecurity] 拦截 exec_sql - sql=%s\n", input.SQL)

		if isDangerousSQL(input.SQL) {
			log.Printf("[SQLSecurity] 危险 SQL 已拦截 - sql=%s\n", input.SQL)
			return "", &DangerousSQLError{SQL: strings.TrimSpace(input.SQL)}
		}

		return endpoint(ctx, argumentsInJSON, opts...)
	}, nil
}

func (m *SQLSecurityMiddleware) WrapStreamableToolCall(
	ctx context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	if tCtx.Name != "exec_sql" {
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		var input ExecInput
		if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
			return nil, err
		}

		log.Printf("[SQLSecurity:Stream] 拦截 exec_sql - sql=%s\n", input.SQL)

		if isDangerousSQL(input.SQL) {
			log.Printf("[SQLSecurity:Stream] 危险 SQL 已拦截 - sql=%s\n", input.SQL)
			return nil, &DangerousSQLError{SQL: strings.TrimSpace(input.SQL)}
		}

		return endpoint(ctx, argumentsInJSON, opts...)
	}, nil
}

// ──────────────────────────────────────────────
// ToolErrorRecoveryMiddleware
// ──────────────────────────────────────────────
//
// Eino 的 ToolsNode 在工具返回 error 时会直接中断整个 graph 执行。
// 但对于业务层面的错误（SQL 语法错误、字段不存在等），我们希望把错误信息
// 作为正常的 tool result 返回给模型，让 ReAct 循环继续——模型看到错误后
// 会自动调整策略重新调用工具。
//
// 只有 DangerousSQLError 需要真正中断（触发前端确认流程）。

type ToolErrorRecoveryMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func (m *ToolErrorRecoveryMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			// DangerousSQLError 必须中断，不能恢复
			var dangerousErr *DangerousSQLError
			if errors.As(err, &dangerousErr) {
				return "", err
			}
			// 其他错误转为正常返回值，让模型看到错误信息并重新思考
			log.Printf("[ToolErrorRecovery] 工具 %s 错误已转为结果 - err=%v\n", tCtx.Name, err)
			return fmt.Sprintf("[工具调用失败] %s\n请根据错误信息调整参数后重试。", err.Error()), nil
		}
		return result, nil
	}, nil
}

func (m *ToolErrorRecoveryMiddleware) WrapStreamableToolCall(
	ctx context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			var dangerousErr *DangerousSQLError
			if errors.As(err, &dangerousErr) {
				return nil, err
			}
			log.Printf("[ToolErrorRecovery:Stream] 工具 %s 错误已转为结果 - err=%v\n", tCtx.Name, err)
			errMsg := fmt.Sprintf("[工具调用失败] %s\n请根据错误信息调整参数后重试。", err.Error())
			return schema.StreamReaderFromArray([]string{errMsg}), nil
		}
		return result, nil
	}, nil
}
