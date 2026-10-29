// dataset_tool.go — dataset_query 工具：在物化数据集上执行只读 SQL。
package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"websql/internal/ai/agent/dataset"
	admin "websql/internal/app/admin"
)

// DatasetQueryInput dataset_query 入参。
type DatasetQueryInput struct {
	DatasetID string `json:"datasetId" jsonschema:"required" jsonschema_description:"query_data 返回的 datasetId"`
	SQL       string `json:"sql" jsonschema:"required" jsonschema_description:"在数据集上执行的只读 SQL（SELECT/WITH），表名为 result"`
	Limit     int    `json:"limit,omitempty" jsonschema_description:"返回行数，默认 500，最大 2000"`
}

// NewDatasetQueryFunc 构造 dataset_query 工具。
func NewDatasetQueryFunc(auditCtx *ExecAuditCtx) func(ctx context.Context, input *DatasetQueryInput) (*QueryOutput, error) {
	return func(ctx context.Context, input *DatasetQueryInput) (*QueryOutput, error) {
		start := time.Now()
		if input == nil {
			return nil, errors.New("input is required")
		}
		id, ok := runIdentityFrom(ctx)
		if !ok || id.UserID == "" || id.SessionID == "" {
			return nil, errors.New("缺少运行身份，无法访问数据集")
		}
		sec, secErr := RunSecurityFrom(ctx)
		if secErr != nil {
			return nil, secErr
		}
		if sec.UserID != "" && sec.UserID != id.UserID {
			return nil, errors.New("数据集归属校验失败")
		}
		entry, entryErr := dataset.Default().Get(input.DatasetID, id.UserID, id.SessionID)
		if entryErr != nil {
			return nil, entryErr
		}
		// Revision=0 是上传文件数据集，不承载数据库权限语义；数据库数据集才做
		// 数据出域/权限修订号复核。
		if entry.Revision != 0 {
			if !sec.DataToolsAllowed() {
				return nil, errors.New("数据访问安全依赖不可用，已拒绝数据集查询")
			}
			if sec.Profile.RequiresRevisionLease() {
				current, fresh := admin.SnapshotPermissionRevision()
				if !fresh || current != sec.Revision {
					return nil, errors.New("权限已发生变化，数据集查询已失效，请重新查询")
				}
			}
		}
		qr, err := dataset.Default().QueryWithRevision(ctx, input.DatasetID, id.UserID, id.SessionID, sec.Revision, input.SQL, input.Limit)
		status := "success"
		errMsg := ""
		if err != nil {
			status = "failed"
			errMsg = err.Error()
		}
		rows := 0
		if qr != nil {
			rows = qr.ReturnedRows
		}
		recordToolAudit(auditCtx.ForRun(ctx), "dataset_query", "SELECT", input.SQL, "dataset:"+input.DatasetID,
			status, rows, int(time.Since(start).Milliseconds()), errMsg)
		if err != nil {
			recordAgentMetric("ai_dataset_query_total", "failed")
			return nil, err
		}
		recordAgentMetric("ai_dataset_query_total", "success")
		if qr.Truncated {
			recordAgentMetric("ai_dataset_query_truncated_total", "true")
		}
		log.Printf("[Tool:dataset_query] dataset=%s, rows=%d, truncated=%v\n", input.DatasetID, qr.ReturnedRows, qr.Truncated)
		out := &QueryOutput{
			Columns:        qr.Columns,
			Data:           qr.Data,
			Count:          qr.ReturnedRows,
			ReturnedRows:   qr.ReturnedRows,
			RowCount:       qr.RowCount,
			TotalKnown:     true,
			TotalRows:      qr.RowCount,
			EstimatedBytes: qr.EstimatedBytes,
			Truncated:      qr.Truncated,
			HasMore:        qr.Truncated,
		}
		if qr.Truncated {
			out.Notice = fmt.Sprintf("dataset_query 结果超过服务端安全上限，仅返回前 %d 行；请继续聚合/过滤", qr.ReturnedRows)
		}
		return out, nil
	}
}
