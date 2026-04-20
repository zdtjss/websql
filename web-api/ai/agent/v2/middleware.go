package agentv2

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
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

// DangerousSQLInfo 是 tool.StatefulInterrupt 的 info 参数，
// 会通过 InterruptCtx.Info 传递到前端，让用户看到要执行的 SQL。
type DangerousSQLInfo struct {
	SQL       string `json:"sql"`
	RiskLevel string `json:"riskLevel"` // high / medium
	SQLType   string `json:"sqlType"`   // DELETE / DROP / INSERT 等
}

func init() {
	// 注册自定义类型，确保 gob 序列化/反序列化正常
	schema.RegisterName[*DangerousSQLInfo]("agentv2.DangerousSQLInfo")
	// string 类型由 Eino 框架内部自动注册，不需要重复注册
}

// ──────────────────────────────────────────────
// ToolErrorRecoveryMiddleware
// ──────────────────────────────────────────────
//
// Eino 的 ToolsNode 在工具返回 error 时会直接中断整个 graph 执行。
// 但对于业务层面的错误（SQL 语法错误、字段不存在等），我们希望把错误信息
// 作为正常的 tool result 返回给模型，让 ReAct 循环继续。
//
// tool.Interrupt 产生的错误必须原样传播（触发 Runner checkpoint），不能拦截。

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
			// Interrupt 错误必须原样传播，让 Runner 捕获并 checkpoint
			if isInterruptError(err) {
				return "", err
			}
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
			if isInterruptError(err) {
				return nil, err
			}
			log.Printf("[ToolErrorRecovery:Stream] 工具 %s 错误已转为结果 - err=%v\n", tCtx.Name, err)
			errMsg := fmt.Sprintf("[工具调用失败] %s\n请根据错误信息调整参数后重试。", err.Error())
			return schema.StreamReaderFromArray([]string{errMsg}), nil
		}
		return result, nil
	}, nil
}

// isInterruptError 检查错误是否为 Eino 的 Interrupt 错误
// Interrupt 错误包含特定的内部标记，这里通过错误链检查
func isInterruptError(err error) bool {
	// Eino 的 interrupt 错误实现了特定接口或包含特定字符串
	// 最可靠的方式是检查错误字符串中是否包含 interrupt 标记
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "interrupt") || strings.Contains(errStr, "Interrupt")
}
