package agentv2

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino/adk"
	adkTool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// isDangerousSQL 检查 SQL 是否为写操作（所有写操作都需要用户确认）
// 会先去除 SQL 注释，防止通过注释前缀绕过检测
func isDangerousSQL(sql string) bool {
	stripped := stripSQLComments(strings.TrimSpace(sql))
	upper := strings.ToUpper(stripped)
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

// stripSQLComments 去除 SQL 开头的注释（行注释和块注释），防止注释绕过安全检测
func stripSQLComments(sql string) string {
	for {
		sql = strings.TrimSpace(sql)
		if sql == "" {
			return sql
		}
		if strings.HasPrefix(sql, "--") {
			idx := strings.Index(sql, "\n")
			if idx == -1 {
				return ""
			}
			sql = sql[idx+1:]
			continue
		}
		if strings.HasPrefix(sql, "/*") {
			idx := strings.Index(sql, "*/")
			if idx == -1 {
				return ""
			}
			sql = sql[idx+2:]
			continue
		}
		break
	}
	return strings.TrimSpace(sql)
}

// ──────────────────────────────────────────────
// DangerousSQLInfo — 危险 SQL 中断时传递给用户的信息
// ──────────────────────────────────────────────

type DangerousSQLInfo struct {
	SQL       string `json:"sql"`
	RiskLevel string `json:"riskLevel"`
	SQLType   string `json:"sqlType"`
}

func init() {
	schema.RegisterName[*DangerousSQLInfo]("agentv2.DangerousSQLInfo")
}

// ──────────────────────────────────────────────
// ToolErrorRecoveryMiddleware
// ──────────────────────────────────────────────
//
// 将业务错误（SQL 语法错误等）转为正常 tool result，让 ReAct 循环继续。
// Interrupt 错误必须原样传播，由 Runner 捕获并 checkpoint。
// 使用 compose.ExtractInterruptInfo 精确判断 Interrupt 错误。

type ToolErrorRecoveryMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

// isInterruptErr uses three methods to reliably detect Eino interrupt errors:
// 1. compose.ExtractInterruptInfo — detects graph-level interrupts
// 2. compose.IsInterruptRerunError — detects tool-level interrupts (core.InterruptSignal)
// 3. errors.As for *adk.InterruptSignal — direct type check as final fallback
func isInterruptErr(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := compose.ExtractInterruptInfo(err); ok {
		return true
	}
	if _, ok := compose.IsInterruptRerunError(err); ok {
		return true
	}
	var is *adk.InterruptSignal
	return errors.As(err, &is)
}

func (m *ToolErrorRecoveryMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...adkTool.Option) (string, error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			if isInterruptErr(err) {
				return "", err
			}
			log.Printf("[ToolErrorRecovery] tool %s error converted to result - err=%v\n", tCtx.Name, err)
			return fmt.Sprintf("[tool call failed] %s\nPlease adjust parameters and retry.", err.Error()), nil
		}
		return result, nil
	}, nil
}

func (m *ToolErrorRecoveryMiddleware) WrapStreamableToolCall(
	ctx context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...adkTool.Option) (*schema.StreamReader[string], error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			if isInterruptErr(err) {
				return nil, err
			}
			log.Printf("[ToolErrorRecovery:Stream] tool %s error converted to result - err=%v\n", tCtx.Name, err)
			errMsg := fmt.Sprintf("[tool call failed] %s\nPlease adjust parameters and retry.", err.Error())
			return schema.StreamReaderFromArray([]string{errMsg}), nil
		}
		return result, nil
	}, nil
}
