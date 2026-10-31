// resultfilter.go — 结果集过滤逻辑从 permmw.go 拆分而来。
//
// 本文件集中存放对工具调用结果进行后过滤的方法：
//   - postFilterSync / postFilterStream：通用同步/流式后过滤包装器
//   - filterListTablesResult：过滤 list_tables 结果中无权限的表
//   - applyQueryResultFilter / applyStreamQueryResultFilter：过滤 query_data 结果集中无权限的列
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func (m *PermissionMiddleware) postFilterSync(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, toolName string, filter func(string) string, opts ...tool.Option) (string, error) {
	if m.Scope.AllSchemasFull || m.Scope.SkipChecks() {
		m.logAllow(toolName, "full_access")
		return endpoint(ctx, args, opts...)
	}
	result, err := endpoint(ctx, args, opts...)
	if err != nil {
		return "", err
	}
	m.logAllow(toolName, "post_filter")
	return filter(result), nil
}

func (m *PermissionMiddleware) postFilterStream(ctx context.Context, args string, endpoint adk.StreamableToolCallEndpoint, toolName string, filter func(string) string, opts ...tool.Option) (*schema.StreamReader[string], error) {
	if m.Scope.AllSchemasFull || m.Scope.SkipChecks() {
		m.logAllow(toolName+"(stream)", "full_access")
		return endpoint(ctx, args, opts...)
	}
	reader, err := endpoint(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	var sb strings.Builder
	if recvErr := drainStream(reader, &sb); recvErr != nil {
		return streamFilterError(toolName, recvErr), nil
	}
	m.logAllow(toolName+"(stream)", "post_filter")
	return schema.StreamReaderFromArray([]string{filter(sb.String())}), nil
}

func (m *PermissionMiddleware) filterListTablesResult(result string) string {
	var output ListTablesOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		m.logDeny("list_tables", "结果JSON解析失败，拒绝返回未过滤数据", nil)
		safeOutput, _ := json.Marshal(ListTablesOutput{Tables: []TableInfo{}, Count: 0})
		return string(safeOutput)
	}

	filtered := make([]TableInfo, 0, len(output.Tables))
	for _, t := range output.Tables {
		if m.Scope.IsTableAllowedIgnoreCase(t.TableName) {
			filtered = append(filtered, t)
		}
	}

	removedCount := len(output.Tables) - len(filtered)
	if removedCount > 0 {
		m.logInfo("list_tables", "过滤无权限表 - 原始=%d, 保留=%d, 移除=%d", len(output.Tables), len(filtered), removedCount)
	}

	output.Tables = filtered
	output.Count = len(filtered)
	outputJSON, _ := json.Marshal(output)
	return string(outputJSON)
}

// drainStream 聚合流式工具结果，区分正常 EOF 与真实读取错误。
func drainStream(reader *schema.StreamReader[string], sb *strings.Builder) error {
	for {
		chunk, recvErr := reader.Recv()
		if recvErr == nil {
			sb.WriteString(chunk)
			continue
		}
		if errors.Is(recvErr, io.EOF) {
			return nil
		}
		return recvErr
	}
}

func streamFilterError(toolName string, err error) *schema.StreamReader[string] {
	log.Printf("%s [过滤] 流式结果读取失败，按 fail-closed 拒绝返回 - err=%v\n", toolName, err)
	recordDegraded("stream", "recv_failed")
	return schema.StreamReaderFromArray([]string{`{"error":true,"message":"工具结果读取失败，已阻止返回不完整数据"}`})
}

func (m *PermissionMiddleware) applyQueryResultFilter(result string, tables []string) (string, error) {
	// 工具错误载荷（{"error":true,...}）不含数据行，原样透传给模型用于自我修复。
	// 必须在反序列化前判断：错误载荷能被解析成空 QueryOutput，会把错误信息悄悄丢掉。
	if strings.Contains(result, `"error":true`) {
		return result, nil
	}
	var output QueryOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		// fail-closed：解析失败说明无法确认结果集内容，可能是异常/被篡改的载荷。
		log.Printf("%s [过滤] query_data 结果解析失败，按 fail-closed 拒绝返回 - err=%v\n", m.logPrefix(), err)
		return "", &PermissionError{
			Message: "查询结果校验失败（结果格式异常），已阻止返回数据。请重试或联系管理员。",
			Objects: []string{},
		}
	}
	beforeColCount := len(output.Columns)
	beforeRowCount := len(output.Data)
	output.Columns, output.Data = m.Scope.FilterResultColumns(output.Columns, output.Data, tables)
	refreshQueryOutputStats(&output)
	removedCount := beforeColCount - len(output.Columns)
	if removedCount > 0 {
		log.Printf("%s [过滤] query_data 结果集列过滤 - 输入列数=%d, 输出列数=%d, 移除=%d, 输入行数=%d, 输出行数=%d\n",
			m.logPrefix(), beforeColCount, len(output.Columns), removedCount, beforeRowCount, len(output.Data))
	}
	outputJSON, _ := json.Marshal(output)
	return string(outputJSON), nil
}

// applyQueryResultFilterMasked 先过滤未授权列，再按「输出列 → 原始列」脱敏。
//
// 顺序不能颠倒：先脱敏会把即将被删除的未授权列也过一遍规则，且无法确认列来源。
// 实现只解析一次 JSON，避免大结果集重复序列化开销。
func (m *PermissionMiddleware) applyQueryResultFilterMasked(ctx context.Context, sql, result string, tables []string) (string, error) {
	start := time.Now()
	defer func() { recordSecurityDuration(StageOutput, time.Since(start)) }()
	sec, secErr := RunSecurityFrom(ctx)
	if secErr != nil {
		return "", secErr
	}
	if strings.Contains(result, `"error":true`) {
		// 错误载荷可能含 SQL 值/连接地址：只做文本脱敏后透传，保留模型自我修复能力。
		return maskTextOrRaw(sec, result), nil
	}
	masker := sec.Masker
	if masker == nil {
		masker = NewNoopMasker()
	}

	var out QueryOutput
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		log.Printf("%s [过滤] query_data 结果解析失败，按 fail-closed 拒绝返回 - err=%v\n", m.logPrefix(), err)
		return "", &PermissionError{
			Message: "查询结果校验失败（结果格式异常），已阻止返回数据。请重试或联系管理员。",
			Objects: []string{},
		}
	}

	beforeColCount := len(out.Columns)
	beforeRowCount := len(out.Data)
	origins := resolveColumnOriginsForNames(sql, out.Columns, m.Scope)
	masker.MaskQueryOutput(&out, tables, origins, m.Scope)
	// 候选策略 2：高敏感列名整列脱敏（shadow 只记指标，enforce 才替换）。
	if candidateColumns := candidateMaskHighRiskColumns(sec, out.Columns, origins, masker); len(candidateColumns) > 0 {
		redactQueryOutputColumns(&out, candidateColumns)
	}

	removedCount := beforeColCount - len(out.Columns)
	if removedCount > 0 {
		log.Printf("%s [过滤] query_data 结果集列过滤 - 输入列数=%d, 输出列数=%d, 移除=%d, 输入行数=%d, 输出行数=%d\n",
			m.logPrefix(), beforeColCount, len(out.Columns), removedCount, beforeRowCount, len(out.Data))
	}
	if masker.HasRules() {
		recordMaskEvent(StageTool, "query_result")
	}
	refreshQueryOutputStats(&out)

	data, err := json.Marshal(out)
	if err != nil {
		recordSecurityEvent(StageTool, "mask", "deny")
		return "", err
	}
	return string(data), nil
}

// refreshQueryOutputStats 在列过滤/脱敏后刷新计数与字节估算。
func refreshQueryOutputStats(out *QueryOutput) {
	if out == nil {
		return
	}
	out.Count = len(out.Data)
	out.ReturnedRows = len(out.Data)
	if out.TotalKnown {
		if out.RowCount < len(out.Data) {
			out.RowCount = len(out.Data)
		}
	} else {
		out.RowCount = 0
	}
	if encoded, err := json.Marshal(out); err == nil {
		out.EstimatedBytes = len(encoded)
	}
}

// applyStreamQueryResultFilterMasked 流式版本：先聚合再过滤与脱敏。
//
// 流式工具结果最终也是一次性交给模型的，聚合不会改变语义，但能保证
// 「先权限过滤、后脱敏」的固定顺序。
func (m *PermissionMiddleware) applyStreamQueryResultFilterMasked(ctx context.Context, sql string, reader *schema.StreamReader[string], tables []string) *schema.StreamReader[string] {
	defer reader.Close()
	var sb strings.Builder
	if recvErr := drainStream(reader, &sb); recvErr != nil {
		return streamFilterError("query_data", recvErr)
	}
	masked, err := m.applyQueryResultFilterMasked(ctx, sql, sb.String(), tables)
	if err != nil {
		// 与同步路径一致：不回退原始结果，向模型返回安全错误
		return schema.StreamReaderFromArray([]string{`{"error":true,"message":"查询结果校验失败，已阻止返回数据"}`})
	}
	return schema.StreamReaderFromArray([]string{masked})
}

func (m *PermissionMiddleware) applyStreamQueryResultFilter(reader *schema.StreamReader[string], tables []string) *schema.StreamReader[string] {
	defer reader.Close()
	var sb strings.Builder
	if recvErr := drainStream(reader, &sb); recvErr != nil {
		return streamFilterError("query_data", recvErr)
	}
	rawResult := sb.String()

	// 与同步路径保持一致：错误载荷透传（需在反序列化前判断），其余解析失败按 fail-closed 拒绝
	if strings.Contains(rawResult, `"error":true`) {
		return schema.StreamReaderFromArray([]string{rawResult})
	}
	var output QueryOutput
	if err := json.Unmarshal([]byte(rawResult), &output); err != nil {
		log.Printf("%s [过滤] query_data(stream) 结果解析失败，按 fail-closed 拒绝返回 - err=%v\n", m.logPrefix(), err)
		return schema.StreamReaderFromArray([]string{`{"error":true,"message":"查询结果校验失败（结果格式异常），已阻止返回数据，请重试。"}`})
	}

	beforeColCount := len(output.Columns)
	beforeRowCount := len(output.Data)
	output.Columns, output.Data = m.Scope.FilterResultColumns(output.Columns, output.Data, tables)
	output.Count = len(output.Data)

	removedCount := beforeColCount - len(output.Columns)
	if removedCount > 0 {
		log.Printf("%s [过滤] query_data(stream) 结果集列过滤 - 输入列数=%d, 输出列数=%d, 移除=%d, 输入行数=%d, 输出行数=%d\n",
			m.logPrefix(), beforeColCount, len(output.Columns), removedCount, beforeRowCount, len(output.Data))
	}

	outputJSON, _ := json.Marshal(output)
	return schema.StreamReaderFromArray([]string{string(outputJSON)})
}
