package agent

// 事件消费阶段的安全辅助：整段审计、工具调用参数净化、资源溯源与安全事件审计。

import (
	"encoding/json"
	"strings"

	appperm "websql/internal/app/permission"
	"websql/internal/audit"

	"github.com/cloudwego/eino/schema"
)

// guardWholeText 用同一套增量审计算法处理整段文本（非流式正文 / 工具结果）。
//
// 等价于「Push(全部) + Close」，因此与流式路径使用完全相同的规则与 holdback 语义。
func guardWholeText(sec *RunSecurityContext, text string) (string, error) {
	if text == "" {
		return "", nil
	}
	masker := NewNoopMasker()
	if sec != nil && sec.Masker != nil {
		masker = sec.Masker
	}
	guard := NewOutputGuard(masker, 0)
	// Push 只能释放「非 holdback」前缀，holdback 尾部由 Close 释放；
	// 两段必须拼接，否则长文本会只剩末尾 holdback 个字符（工具结果被截断）。
	safe, err := guard.Push(text)
	if err != nil {
		return "", err
	}
	tail, err := guard.Close()
	if err != nil {
		return "", err
	}
	if tail == "" {
		return safe, nil
	}
	return safe + tail, nil
}

// sanitizeToolCallsForPersist 净化工具调用参数的会话持久化副本。
//
// 安全语义：
//   - 运行中的可执行参数不被篡改（工具已按原始参数执行）；
//   - 只对落库副本脱敏，避免 SQL 字面量中的手机号/密钥进入会话历史；
//   - 参数 JSON 结构保持不变（规则只替换值内容）。
func sanitizeToolCallsForPersist(sec *RunSecurityContext, calls []schema.ToolCall) []SessionToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]SessionToolCall, 0, len(calls))
	for _, tc := range calls {
		args := tc.Function.Arguments
		if sec != nil && sec.Masker != nil {
			args = sec.Masker.MaskText(args)
		}
		out = append(out, SessionToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: SessionFunctionCall{
				Name:      tc.Function.Name,
				Arguments: args,
			},
		})
	}
	return out
}

// resourcesFromToolCall 从工具调用参数解析资源溯源（conn/schema/table）。
//
// 返回值 complete 表示「是否完整枚举了数据来源」：
//   - false 时调用方必须把消息标记为 Untraceable，权限快照变化后隔离；
//   - 只做结构化解析 + 表名提取，不重新执行权限判断。
func resourcesFromToolCall(name, args string, sec *RunSecurityContext) (refs []ResourceRef, complete bool) {
	connID := ""
	if sec != nil {
		connID = sec.DefaultConnID
	}
	addTables := func(sqlText string) ([]ResourceRef, bool) {
		tables := appperm.ExtractTablesFromSQL(sqlText)
		if len(tables) == 0 {
			return nil, false
		}
		out := make([]ResourceRef, 0, len(tables))
		for _, table := range tables {
			schemaName, tableName := splitSchemaTable(table, "")
			if tableName == "" {
				continue
			}
			out = append(out, ResourceRef{ConnID: connID, Schema: schemaName, Table: tableName})
		}
		return out, len(out) > 0
	}

	switch name {
	case "query_data", "exec_sql", "export_excel", "export_excel_with_chart",
		"export_analysis_docx", "export_ppt", "export_html":
		var in struct {
			SQL    string `json:"sql"`
			ConnID string `json:"connId"`
		}
		if err := json.Unmarshal([]byte(args), &in); err != nil {
			return nil, false
		}
		if strings.TrimSpace(in.ConnID) != "" {
			connID = normalizeResourceConnID(sec, in.ConnID)
		}
		if strings.TrimSpace(in.SQL) == "" {
			return nil, false
		}
		return addTables(in.SQL)
	case "get_table_schema":
		var in struct {
			Tables []string `json:"tables"`
		}
		if err := json.Unmarshal([]byte(args), &in); err != nil {
			return nil, false
		}
		if len(in.Tables) == 0 {
			return nil, true
		}
		out := make([]ResourceRef, 0, len(in.Tables))
		for _, table := range in.Tables {
			schemaName, tableName := splitSchemaTable(table, "")
			if tableName == "" {
				continue
			}
			out = append(out, ResourceRef{ConnID: connID, Schema: schemaName, Table: tableName})
		}
		return out, len(out) == len(in.Tables)
	case "cross_db_join":
		var in struct {
			LeftTable  string `json:"leftTable"`
			RightTable string `json:"rightTable"`
		}
		if err := json.Unmarshal([]byte(args), &in); err != nil {
			return nil, false
		}
		out := make([]ResourceRef, 0, 2)
		for _, table := range []string{in.LeftTable, in.RightTable} {
			schemaName, tableName := splitSchemaTable(table, "")
			if tableName == "" {
				continue
			}
			out = append(out, ResourceRef{ConnID: connID, Schema: schemaName, Table: tableName})
		}
		return out, len(out) == 2
	case "list_tables":
		// 连接级来源：无具体表，仅用于审计与再授权时的连接归属校验
		return []ResourceRef{{ConnID: connID}}, true
	default:
		// 非数据工具（文件/命令/时间等）：不依赖数据库 Scope
		return nil, true
	}
}

// normalizeResourceConnID 把工具参数里的连接标识归一化为真实 ConnID。
//
// 系统提示允许模型把 Schema 名当 connId 传入（自动路由）；若直接写入
// ResourceRef.ConnID，后续 sec.Authorize 会因找不到连接而误隔离历史。
func normalizeResourceConnID(sec *RunSecurityContext, raw string) string {
	if sec == nil {
		return strings.TrimSpace(raw)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return sec.DefaultConnID
	}
	if scope, err := sec.ScopeFor(raw); err == nil {
		return scope.ConnID
	}
	for _, scope := range sec.Scopes {
		if scope == nil {
			continue
		}
		if strings.EqualFold(scope.SchemaName, raw) {
			return scope.ConnID
		}
		for schema := range scope.FullAccessSchemas {
			if strings.EqualFold(schema, raw) {
				return scope.ConnID
			}
		}
		for key := range scope.AllowedTables {
			if strings.EqualFold(splitScopeKey(key).schema, raw) {
				return scope.ConnID
			}
		}
		for key := range scope.AllowedColumns {
			if strings.EqualFold(splitScopeKey(key).schema, raw) {
				return scope.ConnID
			}
		}
	}
	return raw
}

// auditSecurityDenied 记录一条安全拒绝事件（只含阶段/动作与代码内原因，不含数据内容）。
func auditSecurityDenied(stage, toolName, reason, userID string) {
	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:    "agent",
		ToolName:  toolName,
		SQLType:   "SEC_" + strings.ToUpper(stage),
		RiskLevel: "high",
		Status:    "denied",
		UserID:    userID,
		ErrorMsg:  reason,
	})
}
