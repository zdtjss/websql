package agentv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/jmoiron/sqlx"
)

type PermTableStructureInput struct {
	Tables []string `json:"tables" jsonschema:"required,description=要查询结构的表名列表"`
}

type PermTableStructureOutput struct {
	Tables []PermTableInfo `json:"tables"`
}

type PermTableInfo struct {
	TableName string              `json:"tableName"`
	Exists    bool                `json:"exists"`
	Columns   []PermColumnInfo    `json:"columns"`
}

type PermColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable string `json:"nullable"`
}

type PermUserPermissionsInput struct{}

type PermUserPermissionsOutput struct {
	ConnID          string               `json:"connId"`
	SchemaName      string               `json:"schemaName"`
	HasFullConnAccess   bool              `json:"hasFullConnAccess"`
	HasFullSchemaAccess bool              `json:"hasFullSchemaAccess"`
	TablePermissions    []PermTableAccess `json:"tablePermissions"`
}

type PermTableAccess struct {
	TableName      string   `json:"tableName"`
	AccessLevel    string   `json:"accessLevel"`
	AllowedColumns []string `json:"allowedColumns"`
}

func newGetTableStructureFunc(connID string) func(ctx context.Context, input *PermTableStructureInput) (*PermTableStructureOutput, error) {
	return func(ctx context.Context, input *PermTableStructureInput) (*PermTableStructureOutput, error) {
		log.Printf("[PermAgent:get_table_structure] tables=%v\n", input.Tables)
		conn, _ := GetConn(connID)
		if conn == nil {
			return nil, fmt.Errorf("db conn not found: %s", connID)
		}
		var result []PermTableInfo
		for _, table := range input.Tables {
			if !isValidTableName(table) {
				result = append(result, PermTableInfo{TableName: table, Exists: false})
				continue
			}
			columns, err := getTableColumnsFromConn(conn, table)
			if err != nil {
				result = append(result, PermTableInfo{TableName: table, Exists: false})
				continue
			}
			colInfos := make([]PermColumnInfo, 0, len(columns))
			for _, col := range columns {
				colInfos = append(colInfos, PermColumnInfo{
					Name:     col.Name,
					Type:     col.Type,
					Nullable: col.Nullable,
				})
			}
			result = append(result, PermTableInfo{
				TableName: table,
				Exists:    true,
				Columns:   colInfos,
			})
		}
		return &PermTableStructureOutput{Tables: result}, nil
	}
}

type rawColumnInfo struct {
	Name     string
	Type     string
	Nullable string
}

func getTableColumnsFromConn(conn *sqlx.DB, tableName string) ([]rawColumnInfo, error) {
	deref := func(p *string) string {
		if p != nil {
			return *p
		}
		return ""
	}

	driverName := conn.DriverName()
	var query string
	var args []any

	switch driverName {
	case "mysql", "mariadb":
		query = "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE FROM information_schema.COLUMNS WHERE table_name = ? AND table_schema = DATABASE() ORDER BY ORDINAL_POSITION"
		args = []any{tableName}
	case "sqlite":
		if !isValidTableName(tableName) {
			return nil, errors.New("invalid table name")
		}
		query = "PRAGMA table_info('" + strings.ReplaceAll(tableName, "'", "''") + "')"
	case "oracle":
		query = "SELECT COLUMN_NAME, DATA_TYPE, NULLABLE FROM USER_TAB_COLUMNS WHERE TABLE_NAME = :1 ORDER BY COLUMN_ID"
		args = []any{strings.ToUpper(tableName)}
	default:
		query = "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE FROM information_schema.COLUMNS WHERE table_name = ? AND table_schema = DATABASE() ORDER BY ORDINAL_POSITION"
		args = []any{tableName}
	}

	rows, err := conn.Queryx(query, args...)
	if err != nil {
		_ = deref
		return nil, err
	}
	defer rows.Close()

	var columns []rawColumnInfo
	for rows.Next() {
		if driverName == "sqlite" {
			var cid int
			var name, colType string
			var notNull int
			var dfltValue *string
			var pk int
			if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
				continue
			}
			nullable := "YES"
			if notNull != 0 {
				nullable = "NO"
			}
			columns = append(columns, rawColumnInfo{Name: name, Type: colType, Nullable: nullable})
		} else {
			var name, colType, nullable string
			if err := rows.Scan(&name, &colType, &nullable); err != nil {
				continue
			}
			columns = append(columns, rawColumnInfo{Name: name, Type: colType, Nullable: nullable})
		}
	}
	return columns, nil
}

func newGetUserPermissionsFunc(userID, connID, schemaName string) func(ctx context.Context, input *PermUserPermissionsInput) (*PermUserPermissionsOutput, error) {
	return func(ctx context.Context, input *PermUserPermissionsInput) (*PermUserPermissionsOutput, error) {
		log.Printf("[PermAgent:get_user_permissions] userID=%s, connID=%s, schema=%s\n", userID, connID, schemaName)
		scope := BuildPermissionScope(userID, connID, schemaName)
		output := &PermUserPermissionsOutput{
			ConnID:              connID,
			SchemaName:          schemaName,
			HasFullConnAccess:   scope.HasFullConnAccess,
			HasFullSchemaAccess: scope.HasFullSchemaAccess,
		}

		tableSet := make(map[string]*PermTableAccess)
		for table := range scope.AllowedTables {
			tableSet[table] = &PermTableAccess{
				TableName:   table,
				AccessLevel: "full",
			}
		}
		for table, cols := range scope.AllowedColumns {
			if _, exists := tableSet[table]; exists {
				continue
			}
			colList := make([]string, 0, len(cols))
			for col := range cols {
				colList = append(colList, col)
			}
			tableSet[table] = &PermTableAccess{
				TableName:      table,
				AccessLevel:    "column",
				AllowedColumns: colList,
			}
		}
		for _, v := range tableSet {
			output.TablePermissions = append(output.TablePermissions, *v)
		}
		return output, nil
	}
}

type PermDecisionInput struct {
	SQL        string `json:"sql" jsonschema:"required,description=需要检查权限的SQL语句"`
	ToolName   string `json:"toolName" jsonschema:"required,description=调用该SQL的工具名称query_data/exec_sql/export_*等"`
}

type PermDecisionOutput struct {
	Allowed       bool     `json:"allowed"`
	DeniedTables  []string `json:"deniedTables"`
	DeniedColumns []string `json:"deniedColumns"`
	Reason        string   `json:"reason"`
}

func marshalPermDecision(decision *PermDecisionOutput) string {
	data, _ := json.Marshal(decision)
	return string(data)
}

func unmarshalPermDecision(result string) (*PermDecisionOutput, error) {
	var decision PermDecisionOutput
	if err := json.Unmarshal([]byte(result), &decision); err != nil {
		jsonStart := strings.Index(result, "{")
		jsonEnd := strings.LastIndex(result, "}")
		if jsonStart >= 0 && jsonEnd > jsonStart {
			if err2 := json.Unmarshal([]byte(result[jsonStart:jsonEnd+1]), &decision); err2 != nil {
				return nil, fmt.Errorf("parse permission decision failed: %w (original: %w)", err2, err)
			}
			return &decision, nil
		}
		return nil, fmt.Errorf("parse permission decision failed: %w", err)
	}
	return &decision, nil
}
