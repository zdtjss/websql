package agentv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino/adk"
	adkTool "github.com/cloudwego/eino/components/tool"
	einoTool "github.com/cloudwego/eino/components/tool"
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
// SQLApprovalResult — 用户审批结果
// ──────────────────────────────────────────────

// SQLApprovalResult 表示用户对危险SQL的审批结果
type SQLApprovalResult struct {
	Approved         bool   `json:"approved"`
	DisapproveReason string `json:"disapproveReason,omitempty"`
}

// ──────────────────────────────────────────────
// DangerousSQLApprovalMiddleware
// ──────────────────────────────────────────────
//
// 统一拦截 exec_sql 工具的调用，对所有危险SQL执行审批流程。
// 这是 eino 标准 ApprovalMiddleware 模式的具体实现。
//
// 执行流程：
//  1. 首次调用：解析工具参数，检测SQL是否危险
//     - 如果是危险SQL → 触发 StatefulInterrupt，保存SQL到状态
//     - 如果是安全SQL → 直接放行，调用原始 endpoint
//  2. Resume 调用：
//     - 读取用户审批结果
//     - 如果批准 → 调用原始 endpoint 执行（使用服务端保存的SQL）
//     - 如果拒绝 → 返回拒绝信息，不执行
//
// 安全保证：
//  - 所有危险SQL必须经过用户确认才能执行
//  - 执行时使用的是服务端保存的SQL，不是前端传来的，防止篡改
//  - 每条SQL有独立的 interrupt ID，支持逐条确认和批量确认

type DangerousSQLApprovalMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

// WrapInvokableToolCall 拦截同步工具调用
func (m *DangerousSQLApprovalMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return m.wrapToolCall(ctx, endpoint, tCtx)
}

// WrapStreamableToolCall 拦截流式工具调用
// 安全关键：必须实现此方法，否则流式模式下危险SQL可能绕过审批
func (m *DangerousSQLApprovalMiddleware) WrapStreamableToolCall(
	ctx context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	// 只拦截 exec_sql 工具
	if tCtx.Name != "exec_sql" {
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...adkTool.Option) (*schema.StreamReader[string], error) {
		// ── 步骤1：检查是否从中断恢复 ──
		wasInterrupted, hasState, savedArgs := einoTool.GetInterruptState[string](ctx)
		if wasInterrupted && hasState {
			result, err := m.handleResume(ctx, toInvokableEndpoint(endpoint), savedArgs, opts)
			if err != nil {
				return nil, err
			}
			return schema.StreamReaderFromArray([]string{result}), nil
		}

		// ── 步骤2：首次调用，解析参数并检测危险SQL ──
		var input ExecInput
		if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
			return endpoint(ctx, argumentsInJSON, opts...)
		}

		sql := strings.TrimSpace(input.SQL)
		if sql == "" {
			return endpoint(ctx, argumentsInJSON, opts...)
		}

		// 检测是否包含危险SQL（按分号分割，逐条检测）
		var dangerousSQLs []string
		for _, line := range strings.Split(sql, ";") {
			line = strings.TrimSpace(line)
			if line != "" && isDangerousSQL(line) {
				dangerousSQLs = append(dangerousSQLs, line)
			}
		}

		// 如果没有危险SQL，直接放行
		if len(dangerousSQLs) == 0 {
			return endpoint(ctx, argumentsInJSON, opts...)
		}

		// 有危险SQL → 触发中断，保存原始参数供 Resume 后使用
		log.Printf("[ApprovalMiddleware:Stream] 拦截危险SQL - tool=%s, sql=%s\n", tCtx.Name, sql)
		return nil, einoTool.StatefulInterrupt(ctx, &DangerousSQLInfo{
			SQL:       sql,
			RiskLevel: detectRiskLevel(sql),
			SQLType:   detectSQLType(sql),
		}, argumentsInJSON)
	}, nil
}

// wrapToolCall 统一的工具调用拦截逻辑
func (m *DangerousSQLApprovalMiddleware) wrapToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	// 只拦截 exec_sql 工具
	if tCtx.Name != "exec_sql" {
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...adkTool.Option) (string, error) {
		// ── 步骤1：检查是否从中断恢复 ──
		wasInterrupted, hasState, savedArgs := einoTool.GetInterruptState[string](ctx)
		if wasInterrupted && hasState {
			return m.handleResume(ctx, endpoint, savedArgs, opts)
		}

		// ── 步骤2：首次调用，解析参数并检测危险SQL ──
		var input ExecInput
		if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
			return endpoint(ctx, argumentsInJSON, opts...)
		}

		sql := strings.TrimSpace(input.SQL)
		if sql == "" {
			return endpoint(ctx, argumentsInJSON, opts...)
		}

		// 检测是否包含危险SQL（按分号分割，逐条检测）
		var dangerousSQLs []string
		for _, line := range strings.Split(sql, ";") {
			line = strings.TrimSpace(line)
			if line != "" && isDangerousSQL(line) {
				dangerousSQLs = append(dangerousSQLs, line)
			}
		}

		// 如果没有危险SQL，直接放行
		if len(dangerousSQLs) == 0 {
			return endpoint(ctx, argumentsInJSON, opts...)
		}

		// 有危险SQL → 触发中断，保存原始参数供 Resume 后使用
		log.Printf("[ApprovalMiddleware] 拦截危险SQL - tool=%s, sql=%s\n", tCtx.Name, sql)
		return "", einoTool.StatefulInterrupt(ctx, &DangerousSQLInfo{
			SQL:       sql,
			RiskLevel: detectRiskLevel(sql),
			SQLType:   detectSQLType(sql),
		}, argumentsInJSON)
	}, nil
}

// toInvokableEndpoint 将 StreamableToolCallEndpoint 转换为 InvokableToolCallEndpoint
// 用于在流式模式下复用 handleResume 逻辑
func toInvokableEndpoint(endpoint adk.StreamableToolCallEndpoint) adk.InvokableToolCallEndpoint {
	return func(ctx context.Context, argumentsInJSON string, opts ...adkTool.Option) (string, error) {
		reader, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			return "", err
		}
		var result strings.Builder
		for {
			chunk, err := reader.Recv()
			if err != nil {
				break
			}
			result.WriteString(chunk)
		}
		return result.String(), nil
	}
}

// handleResume 处理恢复执行
func (m *DangerousSQLApprovalMiddleware) handleResume(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	savedArgs string,
	opts []adkTool.Option,
) (string, error) {
	// 读取用户审批结果
	isTarget, hasData, approval := einoTool.GetResumeContext[SQLApprovalResult](ctx)
	if !isTarget {
		// 不是恢复目标 → 重新中断以保持状态
		log.Printf("[ApprovalMiddleware] 非恢复目标，重新中断\n")
		return "", einoTool.StatefulInterrupt(ctx, &DangerousSQLInfo{
			SQL: savedArgs, RiskLevel: detectRiskLevel(savedArgs), SQLType: detectSQLType(savedArgs),
		}, savedArgs)
	}

	if !hasData {
		// 没有审批数据 → 重新中断
		log.Printf("[ApprovalMiddleware] 无审批数据，重新中断\n")
		return "", einoTool.StatefulInterrupt(ctx, &DangerousSQLInfo{
			SQL: savedArgs, RiskLevel: detectRiskLevel(savedArgs), SQLType: detectSQLType(savedArgs),
		}, savedArgs)
	}

	if !approval.Approved {
		// 用户拒绝
		reason := approval.DisapproveReason
		if reason == "" {
			reason = "用户取消执行"
		}
		log.Printf("[ApprovalMiddleware] 用户拒绝执行 - reason=%s\n", reason)
		return fmt.Sprintf(`{"message": "%s", "affectedRows": 0}`, reason), nil
	}

	// 用户批准 → 使用服务端保存的原始参数执行
	log.Printf("[ApprovalMiddleware] 用户批准执行 - args=%s\n", savedArgs)
	return endpoint(ctx, savedArgs, opts...)
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
