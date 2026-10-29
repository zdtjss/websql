// dataset_mw.go — 把大查询结果物化为 dataset 句柄的中间件。
//
// 顺序很重要：本中间件位于 Reduction 内层、PermissionMiddleware 外层。
// 这样 PermissionMiddleware 先完成列过滤/脱敏，DatasetMiddleware 再物化，
// Reduction 最后只看到紧凑结果，不会把明细写进 offload。
package agent

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"websql/internal/ai/agent/dataset"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

const (
	// datasetMaterializeMinBytes 结果超过该字节数时物化为数据集。
	// 代码层安全/成本阈值，不写入提示词，也不作为“建议取数数量”。
	datasetMaterializeMinBytes = 16 * 1024
	// datasetSampleRows 返回给模型的样本行数。
	datasetSampleRows = 5
)

// DatasetMiddleware 仅包裹 query_data，将大结果替换为 datasetId + 少量样本。
type DatasetMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	Store *dataset.Store
}

// NewDatasetMiddleware 创建数据集中间件；store 为 nil 时使用默认存储。
func NewDatasetMiddleware() *DatasetMiddleware {
	return &DatasetMiddleware{Store: dataset.Default()}
}

func (m *DatasetMiddleware) enabled() bool {
	return m != nil && m.Store != nil
}

func (m *DatasetMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	if !m.enabled() || tCtx == nil || normalizeToolName(tCtx.Name) != "query_data" {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			return "", err
		}
		return m.materialize(ctx, result), nil
	}, nil
}

func (m *DatasetMiddleware) WrapStreamableToolCall(_ context.Context, endpoint adk.StreamableToolCallEndpoint, tCtx *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	// query_data 由 utils.InferTool 构造为 InvokableTool，实际走上面的包裹链。
	return endpoint, nil
}

func (m *DatasetMiddleware) materialize(ctx context.Context, result string) string {
	var out QueryOutput
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		return result
	}
	if len(out.Columns) == 0 || len(out.Data) == 0 {
		return result
	}
	// 单行大字段（如 DDL/SHOW 结果）交给 reduction 行式落盘更合适，不做数据集。
	if len(out.Data) < 2 && !out.Truncated {
		return result
	}
	// 小结果不物化，保持原有直接返回体验。
	if !out.Truncated && out.EstimatedBytes > 0 && out.EstimatedBytes < datasetMaterializeMinBytes {
		return result
	}
	// EstimatedBytes 缺失时按行数估算；避免把明显很小的结果物化。
	if out.EstimatedBytes == 0 && len(out.Data) < 50 {
		return result
	}
	id, ok := runIdentityFrom(ctx)
	if !ok || id.UserID == "" || id.SessionID == "" {
		return result
	}

	createCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var revision uint64
	if sec, secErr := RunSecurityFrom(ctx); secErr == nil {
		if sec.UserID != "" && sec.UserID != id.UserID {
			log.Printf("[Dataset] 运行身份与安全上下文用户不一致，跳过物化\n")
			return result
		}
		revision = sec.Revision
	}
	entry, err := m.Store.CreateWithRevision(createCtx, id.UserID, id.SessionID, revision, out.Columns, out.Data)
	if err != nil {
		recordAgentMetric("ai_dataset_created_total", "failed")
		log.Printf("[Dataset] 物化失败，保留原结果 - user=%s, session=%s, rows=%d, err=%v\n",
			id.UserID, id.SessionID, len(out.Data), err)
		out.Notice = joinNotice(out.Notice, "数据集物化失败，已保留原始结果；请改用聚合或分页继续分析")
		if encoded, marshalErr := json.Marshal(out); marshalErr == nil {
			return string(encoded)
		}
		return result
	}

	recordAgentMetric("ai_dataset_created_total", "ok")
	log.Printf("[Dataset] 已物化 - id=%s, rows=%d, user=%s, session=%s\n",
		entry.ID, entry.RowCount, id.UserID, id.SessionID)

	sample := make([]map[string]any, 0, datasetSampleRows)
	for i, row := range out.Data {
		if i >= datasetSampleRows {
			break
		}
		sampleRow := make(map[string]any, len(entry.Columns))
		for j, original := range out.Columns {
			if j >= len(entry.Columns) {
				break
			}
			sampleRow[entry.Columns[j]] = row[original]
		}
		sample = append(sample, sampleRow)
	}

	out.Columns = entry.Columns
	out.Data = nil
	out.Sample = sample
	out.DatasetID = entry.ID
	out.ReturnedRows = len(sample)
	out.RowCount = entry.RowCount
	out.Count = entry.RowCount
	out.TotalKnown = true
	out.TotalRows = entry.RowCount
	out.Notice = joinNotice(out.Notice, "结果已物化为数据集，可用 dataset_query 在数据集上执行只读 SQL（聚合/过滤/采样）；样本仅用于预览")
	out.EstimatedBytes = 0
	if encoded, marshalErr := json.Marshal(out); marshalErr == nil {
		out.EstimatedBytes = len(encoded)
		return string(encoded)
	}
	return result
}
