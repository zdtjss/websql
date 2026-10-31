// permcheck.go — permission check helpers extracted from middleware.go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"websql/internal/ai/agent/export"
	"websql/internal/ai/agent/sqlutil"
	"websql/internal/pkg/sqlguard"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// containsDangerousSQL 判断 SQL 是否需要人工审批（fail-closed）。
//
// 判定策略：凡不能被词法校验证明为「无副作用单条只读查询」的语句都需要审批，
// 包括 INSERT/UPDATE/DELETE/DDL，也包括 CALL/SET/RENAME/LOCK 等首关键字不在写关键字表中、
// 但同样会修改数据库状态的语句，以及被 MySQL 可执行注释 /*! ... */ 包裹、
// 剥离注释后首关键字为空的语句（旧实现会把这类语句判定为「非危险」而跳过审批）。
func containsDangerousSQL(sql string) bool {
	if strings.TrimSpace(sql) == "" {
		return false
	}
	return sqlguard.RequiresApproval(sql)
}

type DangerousSQLInfo struct {
	SQL       string `json:"sql"`
	RiskLevel string `json:"riskLevel"`
	SQLType   string `json:"sqlType"`
}

func init() {
	schema.RegisterName[*DangerousSQLInfo]("agent.DangerousSQLInfo")
	schema.RegisterName[*EditApprovalInfo]("agent.EditApprovalInfo")
}

// EditApprovalInfo 文件删除类操作的确认信息（复用现有 danger_confirm 交互）。
type EditApprovalInfo struct {
	Summary   string `json:"summary"`
	RiskLevel string `json:"riskLevel"`
}

type SQLApprovalResult struct {
	Approved         bool   `json:"approved"`
	DisapproveReason string `json:"disapproveReason,omitempty"`
}

type execFunc func(ctx context.Context, args string, opts []tool.Option) (string, error)

func approvalGate(
	ctx context.Context,
	toolName string,
	argumentsInJSON string,
	opts []tool.Option,
	exec execFunc,
) (string, error) {

	wasInterrupted, _, savedArgs := tool.GetInterruptState[string](ctx)

	if !wasInterrupted {
		sql := extractSQLFromArgs(argumentsInJSON)
		if sql == "" || !containsDangerousSQL(sql) {
			return exec(ctx, argumentsInJSON, opts)
		}
		log.Printf("[ApprovalMiddleware] 拦截危险 SQL - tool=%s, sql{%s}\n", toolName, safeSQLSummary(sql))
		return "", tool.StatefulInterrupt(ctx, &DangerousSQLInfo{
			SQL:       sql,
			RiskLevel: string(sqlutil.DetectRiskLevel(sql)),
			SQLType:   string(sqlutil.DetectSQLType(sql)),
		}, argumentsInJSON)
	}

	isTarget, hasData, approval := tool.GetResumeContext[SQLApprovalResult](ctx)

	if isTarget && hasData && approval.Approved {
		log.Printf("[ApprovalMiddleware] 用户批准执行 - tool=%s\n", toolName)
		return exec(ctx, savedArgs, opts)
	}

	if isTarget && hasData && !approval.Approved {
		payload, err := cancelResultPayload(approval.DisapproveReason)
		if err != nil {
			return "", err
		}
		// 日志只记工具名：未批准理由为客户端输入，不得直接写进日志（日志注入/伪条数）。
		log.Printf("[ApprovalMiddleware] 用户未批准执行 - tool=%s\n", toolName)
		return payload, nil
	}

	sql := extractSQLFromArgs(savedArgs)
	if sql == "" {
		sql = savedArgs
	}
	log.Printf("[ApprovalMiddleware] 重新中断 - tool=%s, isTarget=%v, hasData=%v\n", toolName, isTarget, hasData)
	return "", tool.StatefulInterrupt(ctx, &DangerousSQLInfo{
		SQL:       sql,
		RiskLevel: string(sqlutil.DetectRiskLevel(sql)),
		SQLType:   string(sqlutil.DetectSQLType(sql)),
	}, savedArgs)
}

// cancelResultPayload 构造「用户未批准执行」的工具结果 JSON。
//
// 未批准理由来自客户端，必须走 JSON 编码而不是拼接：
// 直接拼进字符串字面量会让引号/反斜杠破坏结构（结果过滤层是 fail-closed，会整条吞掉），
// 也使客户端得以向工具结果注入任意字段。空理由归一为默认文案，过长则按字符数截断。
func cancelResultPayload(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "用户取消执行"
	}
	if runes := []rune(reason); len(runes) > 200 {
		reason = string(runes[:200])
	}
	encoded, err := json.Marshal(map[string]any{
		"message":      reason,
		"affectedRows": 0,
	})
	if err != nil {
		return "", fmt.Errorf("序列化未批准执行结果失败: %w", err)
	}
	return string(encoded), nil
}

func extractSQLFromArgs(argumentsInJSON string) string {
	var input ExecInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return ""
	}
	return strings.TrimSpace(input.SQL)
}

// editOperationsFromArgs 解析 repair_export 的 operations。
func editOperationsFromArgs(argumentsInJSON string) []export.EditOperation {
	var input struct {
		Operations []export.EditOperation `json:"operations"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return nil
	}
	return input.Operations
}

// editApprovalGate 删除类文件编辑的服务端确认：
// 未确认时以 StatefulInterrupt 中断；用户批准后 resume 再执行。
// 复用 SQLApprovalResult 的 resume 负载，前端无需新增交互类型。
func editApprovalGate(ctx context.Context, toolName, argumentsInJSON string, opts []tool.Option, exec execFunc) (string, error) {
	wasInterrupted, _, savedArgs := tool.GetInterruptState[string](ctx)

	if !wasInterrupted {
		summary := export.DescribeDestructiveOperations(editOperationsFromArgs(argumentsInJSON))
		if strings.TrimSpace(summary) == "" {
			return exec(ctx, argumentsInJSON, opts)
		}
		log.Printf("[EditApproval] 拦截删除操作 - tool=%s, ops=%d", toolName, len(editOperationsFromArgs(argumentsInJSON)))
		return "", tool.StatefulInterrupt(ctx, &EditApprovalInfo{Summary: summary, RiskLevel: "high"}, argumentsInJSON)
	}

	isTarget, hasData, approval := tool.GetResumeContext[SQLApprovalResult](ctx)
	if isTarget && hasData && approval.Approved {
		log.Printf("[EditApproval] 用户批准编辑 - tool=%s", toolName)
		return exec(ctx, savedArgs, opts)
	}
	if isTarget && hasData && !approval.Approved {
		reason := approval.DisapproveReason
		if reason == "" {
			reason = "用户取消删除操作"
		}
		// 与 approvalGate 同规：理由不写日志（可能含用户输入），并用 JSON 编码避免破坏载荷。
		log.Printf("[EditApproval] 用户拒绝编辑 - tool=%s", toolName)
		return cancelResultPayload(reason)
	}

	summary := export.DescribeDestructiveOperations(editOperationsFromArgs(savedArgs))
	log.Printf("[EditApproval] 重新中断 - tool=%s", toolName)
	return "", tool.StatefulInterrupt(ctx, &EditApprovalInfo{Summary: summary, RiskLevel: "high"}, savedArgs)
}
