// permcheck.go — permission check helpers extracted from middleware.go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"websql/internal/ai/agent/sqlutil"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func containsDangerousSQL(sql string) bool {
	for _, line := range strings.Split(sql, ";") {
		line = strings.TrimSpace(line)
		if line != "" && sqlutil.IsDangerousSQL(line) {
			return true
		}
	}
	return false
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
		log.Printf("[ApprovalMiddleware] 拦截危险 SQL - tool=%s, sql=%s\n", toolName, sql)
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
