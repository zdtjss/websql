package agent

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestGuardWholeText_MatchesBuiltinMasking(t *testing.T) {
	sec := &RunSecurityContext{Profile: ProfileDesktopLocal, Masker: NewNoopMasker()}
	got, err := guardWholeText(sec, "联系 13800138000 获取 password=hunter2")
	if err != nil {
		t.Fatalf("整段审计失败：%v", err)
	}
	if strings.Contains(got, "13800138000") || strings.Contains(got, "hunter2") {
		t.Fatalf("整段审计未脱敏：%q", got)
	}
	// 与流式守卫结果一致（同一算法）
	guard := NewOutputGuard(NewNoopMasker(), 0)
	streamed, err := collectGuard(t, guard, []string{"联系 13800138000 获取 password=hunter2"})
	if err != nil {
		t.Fatalf("流式审计失败：%v", err)
	}
	if streamed != got {
		t.Fatalf("整段与流式结果应一致\nstream=%q\nwhole=%q", streamed, got)
	}
}

func TestSanitizeToolCallsForPersist(t *testing.T) {
	sec := &RunSecurityContext{Profile: ProfileDesktopLocal, Masker: NewNoopMasker()}
	calls := []schema.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      "query_data",
			Arguments: `{"sql":"SELECT * FROM users WHERE phone='13800138000' AND password='hunter2'"}`,
		},
	}}
	out := sanitizeToolCallsForPersist(sec, calls)
	if len(out) != 1 {
		t.Fatalf("工具调用数量错误：%d", len(out))
	}
	args := out[0].Function.Arguments
	if strings.Contains(args, "13800138000") || strings.Contains(args, "hunter2") {
		t.Fatalf("持久化副本未脱敏：%s", args)
	}
	if out[0].ID != "call-1" || out[0].Function.Name != "query_data" {
		t.Fatalf("工具调用标识被破坏：%+v", out[0])
	}
	// 原始参数不变（运行中可执行参数不被篡改）
	if calls[0].Function.Arguments == args {
		t.Fatal("应为副本，不应原地修改原始参数")
	}
}

func TestResourcesFromToolCall(t *testing.T) {
	sec := &RunSecurityContext{Profile: ProfileWebRemote, DefaultConnID: "c1", Masker: NewNoopMasker()}

	cases := []struct {
		name string
		tool string
		args string
		want []ResourceRef
	}{
		{
			name: "query_data",
			tool: "query_data",
			args: `{"sql":"SELECT * FROM s1.orders o JOIN s2.users u ON o.uid=u.id"}`,
			want: []ResourceRef{
				{ConnID: "c1", Schema: "s1", Table: "orders"},
				{ConnID: "c1", Schema: "s2", Table: "users"},
			},
		},
		{
			name: "query_data_conn_override",
			tool: "query_data",
			args: `{"connId":"c2","sql":"SELECT id FROM t1"}`,
			want: []ResourceRef{{ConnID: "c2", Table: "t1"}},
		},
		{
			name: "get_table_schema",
			tool: "get_table_schema",
			args: `{"tables":["s1.orders"]}`,
			want: []ResourceRef{{ConnID: "c1", Schema: "s1", Table: "orders"}},
		},
		{
			name: "cross_db_join",
			tool: "cross_db_join",
			args: `{"leftTable":"s1.a","rightTable":"s2.b","leftKey":"id","rightKey":"id"}`,
			want: []ResourceRef{
				{ConnID: "c1", Schema: "s1", Table: "a"},
				{ConnID: "c1", Schema: "s2", Table: "b"},
			},
		},
		{
			name: "list_tables",
			tool: "list_tables",
			args: `{}`,
			want: []ResourceRef{{ConnID: "c1"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, complete := resourcesFromToolCall(tc.tool, tc.args, sec)
			if !complete {
				t.Fatalf("合法工具参数应完整枚举资源：got=%v", got)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("资源数量错误：got=%v want=%v", got, tc.want)
			}
			// 表名提取来自 map 遍历，顺序不稳定：按集合比较
			gotSet := make(map[ResourceRef]bool, len(got))
			for _, ref := range got {
				gotSet[ref] = true
			}
			for _, want := range tc.want {
				if !gotSet[want] {
					t.Fatalf("缺少资源 %v：got=%v", want, got)
				}
			}
		})
	}

	if refs, complete := resourcesFromToolCall("unknown_tool", `{}`, sec); refs != nil || !complete {
		t.Fatalf("非数据工具不应产生资源溯源且应视为完整：refs=%v complete=%v", refs, complete)
	}
	// 无法解析出表名的数据工具：必须标记为来源不完整（权限变化后隔离）
	if refs, complete := resourcesFromToolCall("query_data", `{"sql":"SELECT 1"}`, sec); complete || len(refs) != 0 {
		t.Fatalf("无表查询应标记为来源不完整：refs=%v complete=%v", refs, complete)
	}
}

func TestRevisionStillValid_DesktopAlwaysValid(t *testing.T) {
	sec := &RunSecurityContext{Profile: ProfileDesktopLocal}
	if !sec.RevisionStillValid() {
		t.Fatal("桌面 Profile 不依赖修订号租约")
	}
	var nilSec *RunSecurityContext
	if !nilSec.RevisionStillValid() {
		t.Fatal("nil 上下文应按不阻断处理（仅测试用）")
	}
}
