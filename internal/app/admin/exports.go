package admin

import (
	"bytes"
	"errors"
	"strings"

	"websql/internal/config"
	"websql/internal/logger"
)

func CheckPower(userPower *UserPower, param *PowerCheckParam) bool {
	if !config.Cfg.IsRemote {
		return true
	}

	powerDetails := findUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return false
	}

	hasConnLevel := false
	hasSchemaLevel := false
	hasTableLevel := false
	hasTableOrColumnForSchema := false

	for _, power := range powerDetails {
		if power.ConnId != param.ConnId {
			continue
		}

		switch power.Level {
		case "conn":
			hasConnLevel = true
		case "schema":
			if power.SchemaName != nil && *power.SchemaName == param.SchemaName {
				hasSchemaLevel = true
			}
		case "table":
			if power.SchemaName != nil && *power.SchemaName == param.SchemaName {
				hasTableOrColumnForSchema = true
				if power.TableName != nil && *power.TableName == param.TableName {
					hasTableLevel = true
				}
			}
		case "column":
			if power.SchemaName != nil && *power.SchemaName == param.SchemaName {
				hasTableOrColumnForSchema = true
			}
		}
	}

	if hasConnLevel && !hasTableOrColumnForSchema {
		return true
	}

	if hasSchemaLevel && !hasTableOrColumnForSchema {
		return true
	}

	if hasTableLevel {
		return true
	}

	for _, power := range powerDetails {
		if power.ConnId != param.ConnId {
			continue
		}

		if power.Level == "column" {
			if power.SchemaName != nil && *power.SchemaName == param.SchemaName &&
				power.TableName != nil && *power.TableName == param.TableName &&
				power.ColumnName != nil && *power.ColumnName == param.ColumnName {
				return true
			}
		}
	}

	return false
}

func CheckConnAccess(userPower *UserPower, connId string) bool {
	if config.Cfg == nil || !config.Cfg.IsRemote {
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
	if !config.Cfg.IsRemote {
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

func CheckSchemaAccess(connId, schemaName, authorization string) {
	if !config.Cfg.IsRemote {
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
	hasConnLevel := false
	hasSchemaLevel := false
	hasTableOrColumnForSchema := false
	for _, p := range powerDetails {
		if p.ConnId != connId {
			continue
		}
		switch p.Level {
		case "conn":
			hasConnLevel = true
		case "schema":
			if p.SchemaName != nil && *p.SchemaName == schemaName {
				hasSchemaLevel = true
			}
		case "table", "column":
			if p.SchemaName != nil && *p.SchemaName == schemaName {
				hasTableOrColumnForSchema = true
			}
		}
	}
	if hasConnLevel && !hasTableOrColumnForSchema {
		return
	}
	if hasSchemaLevel || hasTableOrColumnForSchema {
		return
	}
	logger.PanicErr(errors.New("无权访问此Schema"))
}

func CheckTableAccess(connId, schemaName, tableName, authorization string) {
	if !config.Cfg.IsRemote {
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
	hasConnLevel := false
	hasSchemaLevel := false
	hasTableOrColumnForSchema := false
	hasTableMatch := false
	for _, p := range powerDetails {
		if p.ConnId != connId {
			continue
		}
		switch p.Level {
		case "conn":
			hasConnLevel = true
		case "schema":
			if p.SchemaName != nil && *p.SchemaName == schemaName {
				hasSchemaLevel = true
			}
		case "table":
			if p.SchemaName != nil && *p.SchemaName == schemaName {
				hasTableOrColumnForSchema = true
				if p.TableName != nil && *p.TableName == tableName {
					hasTableMatch = true
				}
			}
		case "column":
			if p.SchemaName != nil && *p.SchemaName == schemaName && p.TableName != nil && *p.TableName == tableName {
				hasTableOrColumnForSchema = true
				hasTableMatch = true
			}
		}
	}
	if hasConnLevel && !hasTableOrColumnForSchema {
		return
	}
	if hasSchemaLevel && !hasTableOrColumnForSchema {
		return
	}
	if hasTableMatch {
		return
	}
	logger.PanicErr(errors.New("无权访问此表"))
}

func CheckColumnAccess(connId, schemaName, tableName, columnName, authorization string) {
	if !config.Cfg.IsRemote {
		return
	}

	userPower := GetUserPower(authorization)
	param := &PowerCheckParam{
		ConnId:     connId,
		SchemaName: schemaName,
		TableName:  tableName,
		ColumnName: columnName,
	}
	if !CheckPower(userPower, param) {
		logger.PanicErr(errors.New("无权访问此字段"))
	}
}