package agent

import (
	"strings"
	"testing"
)

// ──────────────────────────────────────────────────────────────
// PermissionScope 单元测试
//
// 覆盖所有公开方法的边界条件，确保 RBAC 权限判定的正确性。
// 这些方法是整个 Agent 安全边界的基础，任何回归都可能导致越权。
// ──────────────────────────────────────────────────────────────

// ─── SkipChecks ─────────────────────────────────────────────

func TestPermissionScope_SkipChecks(t *testing.T) {
	tests := []struct {
		name string
		scope *PermissionScope
		want  bool
	}{
		{
			name:  "本地模式（非远程）→ 跳过",
			scope: &PermissionScope{IsRemote: false, HasFullConnAccess: false},
			want:  true,
		},
		{
			name:  "远程模式 + 完整连接权限 → 跳过",
			scope: &PermissionScope{IsRemote: true, HasFullConnAccess: true},
			want:  true,
		},
		{
			name:  "远程模式 + 无完整连接权限 → 不跳过",
			scope: &PermissionScope{IsRemote: true, HasFullConnAccess: false},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.SkipChecks(); got != tt.want {
				t.Errorf("SkipChecks() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ─── HasAnyAccess ───────────────────────────────────────────

func TestPermissionScope_HasAnyAccess(t *testing.T) {
	tests := []struct {
		name  string
		scope *PermissionScope
		want  bool
	}{
		{
			name:  "非远程模式 → 始终有访问权",
			scope: &PermissionScope{IsRemote: false},
			want:  true,
		},
		{
			name:  "远程 + 完整连接权限",
			scope: &PermissionScope{IsRemote: true, HasFullConnAccess: true},
			want:  true,
		},
		{
			name: "远程 + 有 FullAccessSchemas",
			scope: &PermissionScope{
				IsRemote:          true,
				FullAccessSchemas: map[string]bool{"mydb": true},
			},
			want: true,
		},
		{
			name: "远程 + 有 AllowedTables",
			scope: &PermissionScope{
				IsRemote:          true,
				FullAccessSchemas: map[string]bool{},
				AllowedTables:     map[string]bool{"users": true},
			},
			want: true,
		},
		{
			name: "远程 + 有 AllowedColumns",
			scope: &PermissionScope{
				IsRemote:          true,
				FullAccessSchemas: map[string]bool{},
				AllowedTables:     map[string]bool{},
				AllowedColumns:    map[string]map[string]bool{"users": {"id": true}},
			},
			want: true,
		},
		{
			name: "远程 + 无任何权限 → 无访问",
			scope: &PermissionScope{
				IsRemote:          true,
				HasFullConnAccess: false,
				FullAccessSchemas: map[string]bool{},
				AllowedTables:     map[string]bool{},
				AllowedColumns:    map[string]map[string]bool{},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.HasAnyAccess(); got != tt.want {
				t.Errorf("HasAnyAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ─── IsTableAllowed ─────────────────────────────────────────

func TestPermissionScope_IsTableAllowed(t *testing.T) {
	tests := []struct {
		name  string
		scope *PermissionScope
		table string
		want  bool
	}{
		{
			name:  "本地模式跳过检查 → 允许一切",
			scope: &PermissionScope{IsRemote: false},
			table: "any_table",
			want:  true,
		},
		{
			name:  "HasFullConnAccess → 允许一切",
			scope: &PermissionScope{IsRemote: true, HasFullConnAccess: true},
			table: "any_table",
			want:  true,
		},
		{
			name: "AllSchemasFull → 允许一切",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: true,
				AllowedTables:  map[string]bool{},
				AllowedColumns: map[string]map[string]bool{},
			},
			table: "unknown_table",
			want:  true,
		},
		{
			name: "表在 AllowedTables 中 → 允许",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{"users": true, "orders": true},
				AllowedColumns: map[string]map[string]bool{},
			},
			table: "users",
			want:  true,
		},
		{
			name: "表不在 AllowedTables 但有列级权限 → 允许",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{},
				AllowedColumns: map[string]map[string]bool{"users": {"id": true, "name": true}},
			},
			table: "users",
			want:  true,
		},
		{
			name: "表不在任何权限列表中 → 拒绝",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{"orders": true},
				AllowedColumns: map[string]map[string]bool{},
			},
			table: "users",
			want:  false,
		},
		{
			name: "空表名 → 拒绝（map 不含空键）",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{"users": true},
				AllowedColumns: map[string]map[string]bool{},
			},
			table: "",
			want:  false,
		},
		{
			name: "大小写敏感：IsTableAllowed 区分大小写",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{"users": true},
				AllowedColumns: map[string]map[string]bool{},
			},
			table: "USERS",
			want:  false, // 注意：IsTableAllowed 是大小写敏感的
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.IsTableAllowed(tt.table); got != tt.want {
				t.Errorf("IsTableAllowed(%q) = %v, want %v", tt.table, got, tt.want)
			}
		})
	}
}

// ─── IsTableAllowedIgnoreCase ───────────────────────────────

func TestPermissionScope_IsTableAllowedIgnoreCase(t *testing.T) {
	baseScope := &PermissionScope{
		IsRemote:       true,
		AllSchemasFull: false,
		AllowedTables:  map[string]bool{"users": true, "Orders": true},
		AllowedColumns: map[string]map[string]bool{"Products": {"id": true, "name": true}},
	}

	tests := []struct {
		name  string
		table string
		want  bool
	}{
		{"精确匹配 AllowedTables", "users", true},
		{"大写匹配 AllowedTables", "USERS", true},
		{"混合大小写匹配 AllowedTables", "Users", true},
		{"大写匹配混合大小写 key", "ORDERS", true},
		{"小写匹配混合大小写 key", "orders", true},
		{"通过 AllowedColumns 匹配", "Products", true},
		{"通过 AllowedColumns 大小写不敏感", "PRODUCTS", true},
		{"通过 AllowedColumns 小写", "products", true},
		{"不存在的表", "payments", false},
		{"空表名", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := baseScope.IsTableAllowedIgnoreCase(tt.table); got != tt.want {
				t.Errorf("IsTableAllowedIgnoreCase(%q) = %v, want %v", tt.table, got, tt.want)
			}
		})
	}
}

// ─── IsColumnAllowed ────────────────────────────────────────

func TestPermissionScope_IsColumnAllowed(t *testing.T) {
	tests := []struct {
		name   string
		scope  *PermissionScope
		table  string
		column string
		want   bool
	}{
		{
			name:   "SkipChecks → 允许一切",
			scope:  &PermissionScope{IsRemote: false},
			table:  "users",
			column: "password",
			want:   true,
		},
		{
			name: "AllSchemasFull → 允许一切",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: true,
				AllowedTables:  map[string]bool{},
				AllowedColumns: map[string]map[string]bool{},
			},
			table:  "users",
			column: "password",
			want:   true,
		},
		{
			name: "表级权限 → 所有列允许",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{"users": true},
				AllowedColumns: map[string]map[string]bool{},
			},
			table:  "users",
			column: "any_column",
			want:   true,
		},
		{
			name: "列级权限 → 仅允许指定列",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{},
				AllowedColumns: map[string]map[string]bool{"users": {"id": true, "name": true}},
			},
			table:  "users",
			column: "id",
			want:   true,
		},
		{
			name: "列级权限 → 拒绝未列出的列",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{},
				AllowedColumns: map[string]map[string]bool{"users": {"id": true, "name": true}},
			},
			table:  "users",
			column: "password",
			want:   false,
		},
		{
			name: "列级权限 → 列名大小写规范化（存储为小写）",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{},
				AllowedColumns: map[string]map[string]bool{"users": {"email": true}},
			},
			table:  "users",
			column: "EMAIL", // IsColumnAllowed 内部 strings.ToLower
			want:   true,
		},
		{
			name: "列级权限 → 大小写混合",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{},
				AllowedColumns: map[string]map[string]bool{"users": {"email": true}},
			},
			table:  "users",
			column: "Email",
			want:   true,
		},
		{
			name: "表不存在于任何权限中 → 拒绝",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{"orders": true},
				AllowedColumns: map[string]map[string]bool{},
			},
			table:  "users",
			column: "id",
			want:   false,
		},
		{
			name: "HasFullConnAccess → 允许一切",
			scope: &PermissionScope{
				IsRemote:          true,
				HasFullConnAccess: true,
				AllowedTables:     map[string]bool{},
				AllowedColumns:    map[string]map[string]bool{},
			},
			table:  "secret_table",
			column: "secret_column",
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.IsColumnAllowed(tt.table, tt.column); got != tt.want {
				t.Errorf("IsColumnAllowed(%q, %q) = %v, want %v", tt.table, tt.column, got, tt.want)
			}
		})
	}
}

// ─── GetTableAccessLevel ────────────────────────────────────

func TestPermissionScope_GetTableAccessLevel(t *testing.T) {
	tests := []struct {
		name  string
		scope *PermissionScope
		table string
		want  string
	}{
		{
			name:  "SkipChecks → full",
			scope: &PermissionScope{IsRemote: false},
			table: "any",
			want:  "full",
		},
		{
			name: "AllSchemasFull → full",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: true,
				AllowedTables:  map[string]bool{},
				AllowedColumns: map[string]map[string]bool{},
			},
			table: "any",
			want:  "full",
		},
		{
			name: "表在 AllowedTables → full",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{"users": true},
				AllowedColumns: map[string]map[string]bool{},
			},
			table: "users",
			want:  "full",
		},
		{
			name: "表有列级权限 → column",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{},
				AllowedColumns: map[string]map[string]bool{"users": {"id": true}},
			},
			table: "users",
			want:  "column",
		},
		{
			name: "表无任何权限 → none",
			scope: &PermissionScope{
				IsRemote:       true,
				AllSchemasFull: false,
				AllowedTables:  map[string]bool{"orders": true},
				AllowedColumns: map[string]map[string]bool{},
			},
			table: "users",
			want:  "none",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.GetTableAccessLevel(tt.table); got != tt.want {
				t.Errorf("GetTableAccessLevel(%q) = %q, want %q", tt.table, got, tt.want)
			}
		})
	}
}

// ─── FilterResultColumns ────────────────────────────────────

func TestPermissionScope_FilterResultColumns(t *testing.T) {
	t.Run("SkipChecks 返回原始数据", func(t *testing.T) {
		scope := &PermissionScope{IsRemote: false}
		cols := []string{"id", "name", "password"}
		data := []map[string]any{{"id": 1, "name": "alice", "password": "secret"}}
		gotCols, gotData := scope.FilterResultColumns(cols, data, []string{"users"})
		if len(gotCols) != 3 || len(gotData) != 1 {
			t.Errorf("SkipChecks should return original data, got cols=%v data=%v", gotCols, gotData)
		}
	})

	t.Run("表级 full 权限不过滤", func(t *testing.T) {
		scope := &PermissionScope{
			IsRemote:       true,
			AllSchemasFull: false,
			AllowedTables:  map[string]bool{"users": true},
			AllowedColumns: map[string]map[string]bool{},
		}
		cols := []string{"id", "name", "password"}
		data := []map[string]any{{"id": 1, "name": "alice", "password": "secret"}}
		gotCols, gotData := scope.FilterResultColumns(cols, data, []string{"users"})
		if len(gotCols) != 3 || len(gotData) != 1 {
			t.Errorf("Full table access should not filter, got cols=%v", gotCols)
		}
	})

	t.Run("列级权限只保留允许的列", func(t *testing.T) {
		scope := &PermissionScope{
			IsRemote:       true,
			AllSchemasFull: false,
			AllowedTables:  map[string]bool{},
			AllowedColumns: map[string]map[string]bool{"users": {"id": true, "name": true}},
		}
		cols := []string{"id", "name", "password", "email"}
		data := []map[string]any{
			{"id": 1, "name": "alice", "password": "secret", "email": "a@b.com"},
			{"id": 2, "name": "bob", "password": "p4ss", "email": "b@b.com"},
		}
		gotCols, gotData := scope.FilterResultColumns(cols, data, []string{"users"})

		// 应只保留 id 和 name
		if len(gotCols) != 2 {
			t.Fatalf("Expected 2 columns, got %v", gotCols)
		}
		for _, col := range gotCols {
			if col != "id" && col != "name" {
				t.Errorf("Unexpected column in filtered result: %s", col)
			}
		}

		// 数据中不应包含 password 和 email
		for _, row := range gotData {
			if _, ok := row["password"]; ok {
				t.Error("Filtered data should not contain 'password'")
			}
			if _, ok := row["email"]; ok {
				t.Error("Filtered data should not contain 'email'")
			}
			if _, ok := row["id"]; !ok {
				t.Error("Filtered data should contain 'id'")
			}
		}
	})

	t.Run("列级权限大小写不敏感匹配", func(t *testing.T) {
		scope := &PermissionScope{
			IsRemote:       true,
			AllSchemasFull: false,
			AllowedTables:  map[string]bool{},
			AllowedColumns: map[string]map[string]bool{"users": {"id": true, "name": true}},
		}
		// 数据库返回大写列名
		cols := []string{"ID", "NAME", "PASSWORD"}
		data := []map[string]any{{"ID": 1, "NAME": "alice", "PASSWORD": "secret"}}
		gotCols, _ := scope.FilterResultColumns(cols, data, []string{"users"})

		// AllowedColumns 中 key 为小写 "id"/"name"，结果集列名为 "ID"/"NAME"
		// FilterResultColumns 使用 strings.ToLower 比较
		if len(gotCols) != 2 {
			t.Fatalf("Expected 2 columns (case-insensitive), got %v", gotCols)
		}
	})

	t.Run("多表混合权限级别", func(t *testing.T) {
		scope := &PermissionScope{
			IsRemote:       true,
			AllSchemasFull: false,
			AllowedTables:  map[string]bool{"orders": true}, // full access
			AllowedColumns: map[string]map[string]bool{"users": {"id": true}}, // column access
		}
		cols := []string{"id", "name", "order_id"}
		data := []map[string]any{{"id": 1, "name": "alice", "order_id": 100}}
		// 表列表包含一个 column 级别的表
		gotCols, _ := scope.FilterResultColumns(cols, data, []string{"users", "orders"})

		// 由于 users 有 column 级别限制，hasRestrictions=true
		// 过滤器使用 AllowedColumns 中 users 的 "id"
		// 但 orders 也被列出，orders 无 AllowedColumns 条目
		// 当前实现只从 AllowedColumns 收集列名
		if len(gotCols) != 1 || gotCols[0] != "id" {
			t.Errorf("Mixed table filtering: expected [id], got %v", gotCols)
		}
	})

	t.Run("无列级限制的表不触发过滤", func(t *testing.T) {
		scope := &PermissionScope{
			IsRemote:       true,
			AllSchemasFull: false,
			AllowedTables:  map[string]bool{"users": true, "orders": true},
			AllowedColumns: map[string]map[string]bool{},
		}
		cols := []string{"id", "name", "password"}
		data := []map[string]any{{"id": 1, "name": "alice", "password": "secret"}}
		gotCols, gotData := scope.FilterResultColumns(cols, data, []string{"users"})
		if len(gotCols) != 3 || len(gotData) != 1 {
			t.Errorf("No column restrictions, should not filter. cols=%v", gotCols)
		}
	})
}

// ─── DescribeForPrompt ──────────────────────────────────────

func TestPermissionScope_DescribeForPrompt(t *testing.T) {
	t.Run("本地模式 + 允许修改 → 空", func(t *testing.T) {
		scope := &PermissionScope{IsRemote: false, AllowModify: true}
		if got := scope.DescribeForPrompt(); got != "" {
			t.Errorf("Expected empty prompt for local+modify, got %q", got)
		}
	})

	t.Run("本地模式 + 禁止修改 → 含禁止修改提示", func(t *testing.T) {
		scope := &PermissionScope{IsRemote: false, AllowModify: false}
		got := scope.DescribeForPrompt()
		if !strings.Contains(got, "禁止修改数据") {
			t.Errorf("Expected modify restriction, got %q", got)
		}
	})

	t.Run("远程 + HasFullConnAccess + AllowModify → 空", func(t *testing.T) {
		scope := &PermissionScope{IsRemote: true, HasFullConnAccess: true, AllowModify: true}
		if got := scope.DescribeForPrompt(); got != "" {
			t.Errorf("Expected empty prompt for full access, got %q", got)
		}
	})

	t.Run("远程 + AllSchemasFull → 含 Schema 列表", func(t *testing.T) {
		scope := &PermissionScope{
			IsRemote:          true,
			AllSchemasFull:    true,
			AllowModify:       true,
			FullAccessSchemas: map[string]bool{"mydb": true},
			AllowedTables:     map[string]bool{},
			AllowedColumns:    map[string]map[string]bool{},
		}
		got := scope.DescribeForPrompt()
		if !strings.Contains(got, "mydb") {
			t.Errorf("Expected schema name in prompt, got %q", got)
		}
	})

	t.Run("远程 + 表级权限 → 含表格", func(t *testing.T) {
		scope := &PermissionScope{
			IsRemote:          true,
			AllSchemasFull:    false,
			AllowModify:       true,
			FullAccessSchemas: map[string]bool{},
			AllowedTables:     map[string]bool{"users": true},
			AllowedColumns:    map[string]map[string]bool{"orders": {"id": true, "amount": true}},
		}
		got := scope.DescribeForPrompt()
		if !strings.Contains(got, "users") || !strings.Contains(got, "orders") {
			t.Errorf("Expected table names in prompt, got %q", got)
		}
		if !strings.Contains(got, "full") || !strings.Contains(got, "column") {
			t.Errorf("Expected access levels in prompt, got %q", got)
		}
	})
}

// ─── PermissionError ────────────────────────────────────────

func TestPermissionError_Error(t *testing.T) {
	e := &PermissionError{
		Message: "权限不足",
		Objects: []string{"users", "orders"},
	}
	got := e.Error()
	if !strings.Contains(got, "权限不足") || !strings.Contains(got, "users") {
		t.Errorf("Unexpected error format: %s", got)
	}
}
