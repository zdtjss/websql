package agentv2

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// DangerousSQLError 危险 SQL 错误（触发前端确认流程）
type DangerousSQLError struct {
	SQL string
}

func (e *DangerousSQLError) Error() string {
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
