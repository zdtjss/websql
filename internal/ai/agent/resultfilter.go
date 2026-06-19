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
	"log"
	"strings"

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
	var sb strings.Builder
	for {
		chunk, recvErr := reader.Recv()
		if recvErr != nil {
			break
		}
		sb.WriteString(chunk)
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

func (m *PermissionMiddleware) applyQueryResultFilter(result string, tables []string) (string, error) {
	var output QueryOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		return result, nil
	}
	beforeColCount := len(output.Columns)
	beforeRowCount := len(output.Data)
	output.Columns, output.Data = m.Scope.FilterResultColumns(output.Columns, output.Data, tables)
	output.Count = len(output.Data)
	removedCount := beforeColCount - len(output.Columns)
	if removedCount > 0 {
		log.Printf("%s [过滤] query_data 结果集列过滤 - 输入列数=%d, 输出列数=%d, 移除=%d, 输入行数=%d, 输出行数=%d\n",
			m.logPrefix(), beforeColCount, len(output.Columns), removedCount, beforeRowCount, len(output.Data))
	}
	outputJSON, _ := json.Marshal(output)
	return string(outputJSON), nil
}

func (m *PermissionMiddleware) applyStreamQueryResultFilter(reader *schema.StreamReader[string], tables []string) *schema.StreamReader[string] {
	var sb strings.Builder
	for {
		chunk, recvErr := reader.Recv()
		if recvErr != nil {
			break
		}
		sb.WriteString(chunk)
	}
	rawResult := sb.String()

	var output QueryOutput
	if err := json.Unmarshal([]byte(rawResult), &output); err != nil {
		return schema.StreamReaderFromArray([]string{rawResult})
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
