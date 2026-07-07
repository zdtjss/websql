package modeler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	agent "websql/internal/ai/agent"
	"websql/internal/app/conn"
	system "websql/internal/app/system"
	"websql/internal/pkg/sqlguard"

	"github.com/cloudwego/eino/schema"
)

// ReverseEngineerByService 反向工程: 从数据库结构生成 ERModel。
// 业务来自 ReverseEngineer handler。
func ReverseEngineerByService(connId, schema, includeRelations, authorization string) *ERModel {
	if includeRelations == "" {
		includeRelations = "true"
	}
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	model := &ERModel{
		Tables:    make([]ERTable, 0),
		Relations: make([]ERRelation, 0),
	}

	tableNames := getSyncTableList(db, dbType, schema)
	for _, tableName := range tableNames {
		table := buildERTable(db, dbType, schema, tableName)
		model.Tables = append(model.Tables, table)
	}

	if includeRelations == "true" {
		model.Relations = extractRelations(db, dbType, schema)
	}

	return model
}

// ForwardEngineerByService 正向工程: 应用 DDL 到数据库。
// 业务来自 ForwardEngineer handler。DDL 必须经 sqlguard.ValidateDDL 安全校验。
func ForwardEngineerByService(connId, ddlSql, authorization string) map[string]any {
	db := conn.GetConn(connId, authorization)

	if strings.TrimSpace(ddlSql) == "" {
		return map[string]any{"error": "DDL不能为空"}
	}

	statements := splitDDL(ddlSql)
	results := make([]map[string]any, 0)
	successCount := 0
	failCount := 0

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := sqlguard.ValidateDDL(stmt); err != nil {
			results = append(results, map[string]any{
				"sql":     clampStrLen(200, stmt),
				"success": false,
				"error":   err.Error(),
			})
			failCount++
			continue
		}
		_, err := db.Exec(stmt)
		if err != nil {
			results = append(results, map[string]any{
				"sql":     clampStrLen(200, stmt),
				"success": false,
				"error":   err.Error(),
			})
			failCount++
		} else {
			results = append(results, map[string]any{
				"sql":     clampStrLen(200, stmt),
				"success": true,
			})
			successCount++
		}
	}

	return map[string]any{
		"successCount": successCount,
		"failCount":    failCount,
		"results":      results,
		"allSuccess":   failCount == 0,
	}
}

// ExportModelByService 导出 ERModel 为 JSON 或 DDL。
// 业务来自 ExportModel handler。
func ExportModelByService(connId, schema, format, authorization string) map[string]any {
	if format == "" {
		format = "json"
	}
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	model := &ERModel{
		Tables:    make([]ERTable, 0),
		Relations: make([]ERRelation, 0),
	}

	tableNames := getSyncTableList(db, dbType, schema)
	for _, tableName := range tableNames {
		table := buildERTable(db, dbType, schema, tableName)
		model.Tables = append(model.Tables, table)
	}

	model.Relations = extractRelations(db, dbType, schema)

	switch format {
	case "ddl":
		ddl := generateDDLExport(model)
		return map[string]any{"ddl": ddl, "format": "ddl"}
	case "json":
		return map[string]any{"model": model, "format": "json"}
	default:
		return map[string]any{"model": model, "format": format}
	}
}

// AnalyzeRelationsByService 通过 AI 推断表关系。
// 业务来自 AnalyzeRelationsHandler handler。
// 超时返回 ("", error),其他错误返回 ("", error) 由调用方处理。
func AnalyzeRelationsByService(ctx context.Context, req *AnalyzeTableRequest) (*AnalyzeResponse, error) {
	if len(req.Tables) == 0 {
		return &AnalyzeResponse{Relations: []AnalyzeRelation{}}, nil
	}

	cfg := system.GetAIConfigFromDB()
	if cfg == nil || cfg.Provider == "" || cfg.Model == "" {
		return nil, fmt.Errorf("AI 未配置，请联系管理员在系统设置中配置 AI")
	}

	const maxTables = 30
	tables := req.Tables
	if len(tables) > maxTables {
		tables = tables[:maxTables]
	}

	prompt := buildAnalyzePrompt(tables, req.ExistingRelations)

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cm, err := agent.BuildChatModel(ctx, cfg)
	if err != nil {
		log.Printf("[ER-Analyze] 创建模型失败 - err=%v\n", err)
		return nil, fmt.Errorf("AI 模型创建失败")
	}

	msgs := []*schema.Message{
		{Role: schema.System, Content: "你是一名资深数据库架构师，精通关系型数据库表结构设计。请根据提供的表元数据，分析表之间可能的关系。严格按要求输出 JSON，不要输出任何额外解释。"},
		{Role: schema.User, Content: prompt},
	}

	resp, err := cm.Generate(ctx, msgs)
	if err != nil {
		log.Printf("[ER-Analyze] AI 调用失败 - err=%v\n", err)
		return nil, fmt.Errorf("AI 分析失败: %s", err.Error())
	}

	relations := parseAnalyzeResponse(resp.Content)
	log.Printf("[ER-Analyze] schema=%s tables=%d inferred=%d\n",
		req.Schema, len(tables), len(relations))

	return &AnalyzeResponse{Relations: relations}, nil
}
