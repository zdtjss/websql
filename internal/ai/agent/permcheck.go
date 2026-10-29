// permcheck.go — permission check helpers extracted from middleware.go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

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
		reason := approval.DisapproveReason
		if reason == "" {
			reason = "用户取消执行"
		}
		log.Printf("[ApprovalMiddleware] 用户拒绝执行 - tool=%s, reason=%s\n", toolName, reason)
		return fmt.Sprintf(`{"message": "%s", "affectedRows": 0}`, reason), nil
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

func extractSQLFromArgs(argumentsInJSON string) string {
	var input ExecInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return ""
	}
	return strings.TrimSpace(input.SQL)
}
