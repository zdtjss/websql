package admin

import (
	"bytes"
	"errors"
	"strings"

	"websql/internal/config"
	"websql/internal/logger"
)

func GroupPowerDetailsByRole(powerDetails []*PowerDetail, connId string) map[string][]*PowerDetail {
	byRole := make(map[string][]*PowerDetail)
	for _, p := range powerDetails {
		if p.ConnId != connId {
			continue
		}
		byRole[p.RoleId] = append(byRole[p.RoleId], p)
	}
	return byRole
}

func checkPowerForRole(roleDetails []*PowerDetail, param *PowerCheckParam) bool {
	r := ResolveRolePermissions(roleDetails)
	if param.ColumnName != "" {
		return r.CanAccessColumn(param.SchemaName, param.TableName, param.ColumnName)
	}
	if param.TableName != "" {
		return r.CanAccessTable(param.SchemaName, param.TableName)
	}
	return r.CanAccessSchema(param.SchemaName)
}

func CheckPower(userPower *UserPower, param *PowerCheckParam) bool {
	if config.IsLocalMode() {
		return true
	}

	powerDetails := findUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return false
	}

	byRole := GroupPowerDetailsByRole(powerDetails, param.ConnId)
	for _, roleDetails := range byRole {
		if checkPowerForRole(roleDetails, param) {
			return true
		}
	}

	return false
}

func CheckConnAccessByUserId(userId, connId string) bool {
	if config.IsLocalMode() {
		return true
	}
	connIds := findUserPower(userId)
	for _, id := range connIds {
		if id == connId {
			return true
		}
	}
	return false
}

func CheckConnAccess(userPower *UserPower, connId string) bool {
	if config.IsLocalMode() {
		return true
	}

	if userPower == nil || len(userPower.Power) == 0 {
		return false
	}

	for _, powerConnId := range userPower.Power {
		if powerConnId == connId {
			return true
		}
	}

	return false
}

func AppendPmsn(sql *bytes.Buffer, col string, param *[]any, userPower *UserPower) {
	if config.IsLocalMode() {
		return
	}
	powerCount := len(userPower.Power)
	sql.WriteString(" and ")
	if powerCount == 0 {
		sql.WriteString(" 1 = 2 ")
		return
	}
	sql.WriteString(col)
	sql.WriteString(" in ( ")
	sql.WriteString(strings.Repeat("?,", powerCount)[0 : powerCount*2-1])
	sql.WriteString(") ")

	for i := 0; i < powerCount; i++ {
		*param = append(*param, userPower.Power[i])
	}
}

func checkSchemaAccessForRole(roleDetails []*PowerDetail, schemaName string) bool {
	r := ResolveRolePermissions(roleDetails)
	return r.CanAccessSchema(schemaName)
}

func CheckSchemaAccess(connId, schemaName, authorization string) {
	if config.IsLocalMode() {
		return
	}

	userPower := GetUserPower(authorization)
	if userPower == nil || len(userPower.Power) == 0 {
		logger.PanicErr(errors.New("无权访问此Schema"))
		return
	}
	powerDetails := findUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		logger.PanicErr(errors.New("无权访问此Schema"))
		return
	}
	byRole := GroupPowerDetailsByRole(powerDetails, connId)
	for _, roleDetails := range byRole {
		if checkSchemaAccessForRole(roleDetails, schemaName) {
			return
		}
	}
	logger.PanicErr(errors.New("无权访问此Schema"))
}

func checkTableAccessForRole(roleDetails []*PowerDetail, schemaName, tableName string) bool {
	r := ResolveRolePermissions(roleDetails)
	return r.CanAccessTable(schemaName, tableName)
}

func CheckTableAccess(connId, schemaName, tableName, authorization string) {
	if config.IsLocalMode() {
		return
	}

	userPower := GetUserPower(authorization)
	if userPower == nil || len(userPower.Power) == 0 {
		logger.PanicErr(errors.New("无权访问此表"))
		return
	}
	powerDetails := findUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		logger.PanicErr(errors.New("无权访问此表"))
		return
	}
	byRole := GroupPowerDetailsByRole(powerDetails, connId)
	for _, roleDetails := range byRole {
		if checkTableAccessForRole(roleDetails, schemaName, tableName) {
			return
		}
	}
	logger.PanicErr(errors.New("无权访问此表"))
}

type RolePermResult struct {
	HasConnLevel bool
	BySchema     map[string]*SchemaPermResult
}

type SchemaPermResult struct {
	HasSchemaLevel        bool
	HasTableLevelInSchema bool
	ByTable               map[string]*TablePermResult
}

type TablePermResult struct {
	HasTableLevel bool
	Columns       map[string]bool
}

func ResolveRolePermissions(roleDetails []*PowerDetail) *RolePermResult {
	result := &RolePermResult{
		BySchema: make(map[string]*SchemaPermResult),
	}
	for _, p := range roleDetails {
		pSchema := ""
		if p.SchemaName != nil {
			pSchema = *p.SchemaName
		}
		pTable := ""
		if p.TableName != nil {
			pTable = *p.TableName
		}
		pColumn := ""
		if p.ColumnName != nil {
			pColumn = *p.ColumnName
		}
		switch p.Level {
		case "conn":
			result.HasConnLevel = true
		case "schema":
			sp := result.BySchema[pSchema]
			if sp == nil {
				sp = &SchemaPermResult{ByTable: make(map[string]*TablePermResult)}
				result.BySchema[pSchema] = sp
			}
			sp.HasSchemaLevel = true
		case "table":
			sp := result.BySchema[pSchema]
			if sp == nil {
				sp = &SchemaPermResult{ByTable: make(map[string]*TablePermResult)}
				result.BySchema[pSchema] = sp
			}
			sp.HasTableLevelInSchema = true
			if pTable != "" {
				tp := sp.ByTable[pTable]
				if tp == nil {
					tp = &TablePermResult{Columns: make(map[string]bool)}
					sp.ByTable[pTable] = tp
				}
				tp.HasTableLevel = true
			}
		case "column":
			sp := result.BySchema[pSchema]
			if sp == nil {
				sp = &SchemaPermResult{ByTable: make(map[string]*TablePermResult)}
				result.BySchema[pSchema] = sp
			}
			if pTable != "" && pColumn != "" {
				tp := sp.ByTable[pTable]
				if tp == nil {
					tp = &TablePermResult{Columns: make(map[string]bool)}
					sp.ByTable[pTable] = tp
				}
				colName := ParseColumnName(pColumn)
				tp.Columns[colName] = true
			}
		}
	}
	return result
}

func (r *RolePermResult) CanAccessSchema(schema string) bool {
	if r.HasConnLevel {
		return true
	}
	_, exists := r.BySchema[schema]
	return exists
}

func (r *RolePermResult) CanAccessAllTablesInSchema(schema string) bool {
	sp := r.BySchema[schema]
	if r.HasConnLevel {
		return sp == nil || !sp.HasRestriction()
	}
	if sp != nil && sp.HasSchemaLevel {
		return !sp.HasRestriction()
	}
	return false
}

func (sp *SchemaPermResult) HasRestriction() bool {
	if sp.HasTableLevelInSchema {
		return true
	}
	for _, tp := range sp.ByTable {
		if len(tp.Columns) > 0 {
			return true
		}
	}
	return false
}

func (r *RolePermResult) CanAccessTable(schema, table string) bool {
	sp := r.BySchema[schema]

	// 判断该 schema 下是否存在更具体的限制（table 级或 column 级配置）
	schemaHasRestriction := sp != nil && sp.HasRestriction()

	// conn 级权限：如果该 schema 下没有更具体的配置，直接放行
	if r.HasConnLevel && !schemaHasRestriction {
		return true
	}
	// schema 级权限：同理
	if sp != nil && sp.HasSchemaLevel && !schemaHasRestriction {
		return true
	}

	// 有限制时，检查该表是否在 ByTable 中（table 级或 column 级都算有权限）
	if sp != nil {
		_, exists := sp.ByTable[table]
		return exists
	}
	return false
}

func (r *RolePermResult) CanAccessColumn(schema, table, column string) bool {
	sp := r.BySchema[schema]

	// 判断该 schema 下是否存在更具体的限制
	schemaHasRestriction := sp != nil && sp.HasRestriction()

	if r.HasConnLevel && !schemaHasRestriction {
		return true
	}
	if sp != nil && sp.HasSchemaLevel && !schemaHasRestriction {
		return true
	}
	if sp == nil {
		return false
	}
	tp := sp.ByTable[table]
	if tp == nil {
		return false
	}
	// 最具体优先：如果有 column 级配置，即使同时有 table 级也以 column 级为准
	if len(tp.Columns) > 0 {
		return tp.Columns[strings.ToLower(column)]
	}
	if tp.HasTableLevel {
		return true
	}
	return false
}

// ParseColumnName 解析列名：截取双空格前的部分，去除首尾空格，并统一转为小写。
// 列名大小写归一化确保权限匹配不受数据库列名大小写差异影响。
func ParseColumnName(raw string) string {
	if idx := strings.Index(raw, "  "); idx > 0 {
		return strings.ToLower(strings.TrimSpace(raw[:idx]))
	}
	return strings.ToLower(strings.TrimSpace(raw))
}
