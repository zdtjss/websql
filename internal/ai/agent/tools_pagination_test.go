package agent

import "testing"

func TestPaginateTables(t *testing.T) {
	tables := make([]TableInfo, 0, 1200)
	for i := 0; i < 1200; i++ {
		tables = append(tables, TableInfo{TableName: "t"})
	}
	page, total, offset, limit, next, hasMore := paginateTables(tables, 500, 400)
	if total != 1200 || offset != 500 || limit != 400 || len(page) != 400 || !hasMore || next != 900 {
		t.Fatalf("分页结果不正确：total=%d offset=%d limit=%d page=%d next=%d hasMore=%v",
			total, offset, limit, len(page), next, hasMore)
	}
	page, _, _, _, next, hasMore = paginateTables(tables, 1000, 400)
	if len(page) != 200 || hasMore || next != 0 {
		t.Fatalf("末页结果不正确：page=%d next=%d hasMore=%v", len(page), next, hasMore)
	}
}
