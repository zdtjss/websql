package admin

import (
	"log"

	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// MaskRuleSaveRequest 脱敏规则保存请求。
type MaskRuleSaveRequest struct {
	ID         string `json:"id"`
	ConnID     string `json:"connId"`
	SchemaName string `json:"schemaName"`
	TableName  string `json:"tableName"`
	ColumnName string `json:"columnName"`
	Action     string `json:"action"`
	Enabled    *int   `json:"enabled"`
}

// ListMaskRulesHandler 查询全部生效中的脱敏规则。
func ListMaskRulesHandler(c *gin.Context) {
	rules, err := ListMaskRules()
	if err != nil {
		log.Printf("[MaskRule] 查询失败 - err=%v", err)
		response.WriteErr(c, 200, 500, "查询脱敏规则失败")
		return
	}
	response.WriteOK(c, rules)
}

// SaveMaskRuleHandler 新增或更新一条脱敏规则。
func SaveMaskRuleHandler(c *gin.Context) {
	if !CheckAdminPower(c) {
		return
	}
	req := &MaskRuleSaveRequest{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, req); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}
	enabled := 1
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	rule := &MaskRuleRecord{
		ID:         req.ID,
		ConnID:     req.ConnID,
		SchemaName: req.SchemaName,
		TableName:  req.TableName,
		ColumnName: req.ColumnName,
		Action:     req.Action,
		Enabled:    enabled,
	}
	if err := SaveMaskRule(rule); err != nil {
		log.Printf("[MaskRule] 保存失败 - err=%v", err)
		response.WriteErr(c, 200, 400, err.Error())
		return
	}
	response.WriteOK(c, gin.H{"id": rule.ID})
}

// DeleteMaskRuleHandler 删除一条脱敏规则。
func DeleteMaskRuleHandler(c *gin.Context) {
	if !CheckAdminPower(c) {
		return
	}
	if err := DeleteMaskRule(c.Query("id")); err != nil {
		log.Printf("[MaskRule] 删除失败 - err=%v", err)
		response.WriteErr(c, 200, 500, "删除脱敏规则失败")
		return
	}
	response.WriteOK(c, "")
}
