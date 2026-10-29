package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"websql/internal/ai/agent/dataset"
)

func TestDatasetMiddlewareMaterializesLargeResult(t *testing.T) {
	store := dataset.NewStore(t.TempDir(), time.Hour)
	defer store.Close()
	mw := &DatasetMiddleware{Store: store}
	ctx := WithRunIdentity(context.Background(), RunIdentity{UserID: "u1", SessionID: "s1"})

	rows := make([]map[string]any, 0, 100)
	for i := 0; i < 100; i++ {
		rows = append(rows, map[string]any{"id": int64(i), "group": "g", "amount": int64(i % 7)})
	}
	out := QueryOutput{Columns: []string{"id", "group", "amount"}, Data: rows, Count: len(rows), EstimatedBytes: 1 << 20}
	encoded, _ := json.Marshal(&out)
	got := mw.materialize(ctx, string(encoded))

	var parsed QueryOutput
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("解析中间件结果失败：%v", err)
	}
	if parsed.DatasetID == "" {
		t.Fatalf("大结果应返回 datasetId：%s", got)
	}
	if len(parsed.Data) != 0 {
		t.Fatalf("物化后不应保留完整 Data：%+v", parsed.Data)
	}
	if len(parsed.Sample) == 0 {
		t.Fatal("物化后应返回样本")
	}

	qr, err := store.Query(ctx, parsed.DatasetID, "u1", "s1", `SELECT COUNT(*) AS n FROM result`, 0)
	if err != nil {
		t.Fatalf("dataset 查询失败：%v", err)
	}
	if len(qr.Data) != 1 {
		t.Fatalf("COUNT 结果行数不正确：%+v", qr.Data)
	}
}

func TestDatasetMiddlewareLeavesSmallResultUnchanged(t *testing.T) {
	store := dataset.NewStore(t.TempDir(), time.Hour)
	defer store.Close()
	mw := &DatasetMiddleware{Store: store}
	ctx := WithRunIdentity(context.Background(), RunIdentity{UserID: "u1", SessionID: "s1"})
	out := QueryOutput{Columns: []string{"id"}, Data: []map[string]any{{"id": 1}}, Count: 1, EstimatedBytes: 100}
	encoded, _ := json.Marshal(&out)
	if got := mw.materialize(ctx, string(encoded)); got != string(encoded) {
		t.Fatalf("小结果不应被改写：%s", got)
	}
}
