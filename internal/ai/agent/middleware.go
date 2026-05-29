package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func isDangerousSQL(sql string) bool {
	stripped := stripSQLComments(strings.TrimSpace(sql))
	upper := strings.ToUpper(stripped)
	for _, p := range []string{
		"DROP ", "TRUNCATE ", "DELETE ",
		"ALTER ", "CREATE ", "REPLACE ",
		"INSERT ", "UPDATE ", "MERGE ",
	} {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}
	return false
}

func containsDangerousSQL(sql string) bool {
	for _, line := range strings.Split(sql, ";") {
		line = strings.TrimSpace(line)
		if line != "" && isDangerousSQL(line) {
			return true
		}
	}
	return false
}

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

type DangerousSQLApprovalMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func (m *DangerousSQLApprovalMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if tCtx.Name != "exec_sql" {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		return approvalGate(ctx, tCtx.Name, argumentsInJSON, opts,
			func(ctx context.Context, args string, opts []tool.Option) (string, error) {
				return endpoint(ctx, args, opts...)
			},
		)
	}, nil
}

func (m *DangerousSQLApprovalMiddleware) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	if tCtx.Name != "exec_sql" {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		result, err := approvalGate(ctx, tCtx.Name, argumentsInJSON, opts,
			func(ctx context.Context, args string, opts []tool.Option) (string, error) {
				reader, err := endpoint(ctx, args, opts...)
				if err != nil {
					return "", err
				}
				var sb strings.Builder
				for {
					chunk, recvErr := reader.Recv()
					if recvErr != nil {
						break
					}
					sb.WriteString(chunk)
				}
				return sb.String(), nil
			},
		)
		if err != nil {
			return nil, err
		}
		return schema.StreamReaderFromArray([]string{result}), nil
	}, nil
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
			RiskLevel: detectRiskLevel(sql),
			SQLType:   detectSQLType(sql),
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
		RiskLevel: detectRiskLevel(sql),
		SQLType:   detectSQLType(sql),
	}, savedArgs)
}

func extractSQLFromArgs(argumentsInJSON string) string {
	var input ExecInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return ""
	}
	return strings.TrimSpace(input.SQL)
}

type ToolErrorRecoveryMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

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
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			if isInterruptErr(err) {
				return "", err
			}
			log.Printf("[ToolErrorRecovery] tool %s error → result - err=%v\n", tCtx.Name, err)
			return fmt.Sprintf("[tool call failed] %s\n%s", err.Error(), recoveryHint(tCtx.Name, argumentsInJSON, err)), nil
		}
		return result, nil
	}, nil
}

func (m *ToolErrorRecoveryMiddleware) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			if isInterruptErr(err) {
				return nil, err
			}
			log.Printf("[ToolErrorRecovery:Stream] tool %s error → result - err=%v\n", tCtx.Name, err)
			errMsg := fmt.Sprintf("[tool call failed] %s\n%s", err.Error(), recoveryHint(tCtx.Name, argumentsInJSON, err))
			return schema.StreamReaderFromArray([]string{errMsg}), nil
		}
		return result, nil
	}, nil
}

func recoveryHint(toolName, args string, err error) string {
	errStr := err.Error()
	isExecuteOrSkill := toolName == "execute" || toolName == "skill"
	if !isExecuteOrSkill {
		return "Please adjust parameters and retry."
	}

	isPipOrPython := strings.Contains(args, "pip") ||
		strings.Contains(args, "python") ||
		strings.Contains(args, "matplotlib") ||
		strings.Contains(args, "pptx") ||
		strings.Contains(args, "docx")

	isEncodingOrSyntax := strings.Contains(errStr, "UnicodeEncodeError") ||
		strings.Contains(errStr, "SyntaxError") ||
		strings.Contains(errStr, "gbk") ||
		strings.Contains(errStr, "unterminated string") ||
		strings.Contains(errStr, "illegal multibyte")

	isSkillExport := strings.Contains(args, "export-word") ||
		strings.Contains(args, "export-ppt") ||
		strings.Contains(args, "word_generator") ||
		strings.Contains(args, "export_ppt.py") ||
		strings.Contains(args, "generate_notice") ||
		strings.Contains(args, "check_fonts") ||
		strings.Contains(args, "check_deps")

	if isPipOrPython && (isEncodingOrSyntax || isSkillExport) {
		return "Python skill execution failed on Windows. Use dedicated export tools: 'export_ppt' for PPT, 'export_analysis_docx' for Word. These tools internally prefer Python Skill (same quality) with automatic Go fallback - no manual script assembly needed."
	}

	if isSkillExport {
		return "Skill script execution failed. Use dedicated export tools: 'export_ppt' for PPT, 'export_analysis_docx' for Word. They automatically try Python Skill first and fallback to Go if needed."
	}

	return "Please adjust parameters and retry."
}

type ToolCallLoggingMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func (m *ToolCallLoggingMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		startTime := time.Now()
		log.Printf("[ToolCall] 开始调用 - name=%s, args=%s\n", tCtx.Name, argumentsInJSON)

		result, err := endpoint(ctx, argumentsInJSON, opts...)

		elapsed := time.Since(startTime)
		if err != nil {
			log.Printf("[ToolCall] 调用失败 - name=%s, duration=%v, err=%v\n", tCtx.Name, elapsed, err)
		} else {
			log.Printf("[ToolCall] 调用完成 - name=%s, duration=%v, result=%s\n", tCtx.Name, elapsed, result)
		}

		return result, err
	}, nil
}

func (m *ToolCallLoggingMiddleware) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		startTime := time.Now()
		log.Printf("[ToolCall:Stream] 开始调用 - name=%s, args=%s\n", tCtx.Name, argumentsInJSON)

		reader, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			elapsed := time.Since(startTime)
			log.Printf("[ToolCall:Stream] 调用失败 - name=%s, duration=%v, err=%v\n", tCtx.Name, elapsed, err)
			return nil, err
		}

		var contentBuf strings.Builder
		wrapped := schema.StreamReaderWithConvert(reader, func(s string) (string, error) {
			contentBuf.WriteString(s)
			return s, nil
		})

		go func() {
			for {
				_, err := wrapped.Recv()
				if err != nil {
					elapsed := time.Since(startTime)
					content := contentBuf.String()
					log.Printf("[ToolCall:Stream] 流结束 - name=%s, duration=%v, resultLen=%d, result=%s\n",
						tCtx.Name, elapsed, len(content), content)
					return
				}
			}
		}()

		return reader, nil
	}, nil
}

const defaultReductionMaxRows = 100

type ReductionMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	MaxRows int
}

func (m *ReductionMiddleware) maxRows() int {
	if m.MaxRows > 0 {
		return m.MaxRows
	}
	return defaultReductionMaxRows
}

func (m *ReductionMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if tCtx.Name != "query_data" {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			return result, err
		}
		return m.reduceQueryResult(result), nil
	}, nil
}

func (m *ReductionMiddleware) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	if tCtx.Name != "query_data" {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		reader, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			return nil, err
		}
		var sb strings.Builder
		for {
			chunk, recvErr := reader.Recv()
			if recvErr != nil {
				break
			}
			sb.WriteString(chunk)
		}
		reduced := m.reduceQueryResult(sb.String())
		return schema.StreamReaderFromArray([]string{reduced}), nil
	}, nil
}

func (m *ReductionMiddleware) reduceQueryResult(result string) string {
	var output QueryOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		return result
	}
	maxRows := m.maxRows()
	if len(output.Data) <= maxRows {
		return result
	}
	totalRows := len(output.Data)
	output.Data = output.Data[:maxRows]
	output.Count = totalRows
	reducedJSON, _ := json.Marshal(output)
	log.Printf("[Reduction] query_data 结果精简 - 原始行数=%d, 保留行数=%d\n", totalRows, maxRows)
	return string(reducedJSON)
}
