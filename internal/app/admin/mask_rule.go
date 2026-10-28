package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"websql/internal/pkg/idgen"
)

// 数据脱敏规则：管理员对「已授权列」配置的数据保护策略。
//
// 注意：脱敏规则不是权限等级，也不改变 PowerDetail。未授权列只能拒绝或删除，
// 不能用打码代替授权。规则优先级：
//  1. 未授权表/列：拒绝或删除（最高）；
//  2. 本表配置的 partial/tokenize/redact；
//  3. 内置敏感信息规则（密钥、身份证、手机号、邮箱）；
//  4. 无规则：保持原值。
const (
	MaskActionPartial  = "partial"
	MaskActionTokenize = "tokenize"
	MaskActionRedact   = "redact"

	// 脱敏 HMAC 密钥在 t_system_config 中的键名与长度约束
	maskKeyConfigKey    = "ai.maskKey"
	maskKeyVersionKey   = "ai.maskKeyVersion"
	maskKeyMinBytes     = 32
	maskRuleMaxTextLen  = 256
	maskRuleMaxNumRules = 10000
)

// MaskRuleRecord 一条已配置的列级脱敏规则。
type MaskRuleRecord struct {
	ID         string    `json:"id" db:"id"`
	ConnID     string    `json:"connId" db:"conn_id"`
	SchemaName string    `json:"schemaName" db:"schema_name"`
	TableName  string    `json:"tableName" db:"table_name"`
	ColumnName string    `json:"columnName" db:"column_name"`
	Action     string    `json:"action" db:"action"`
	Enabled    int       `json:"enabled" db:"enabled"`
	UpdatedAt  time.Time `json:"updatedAt" db:"updated_at"`
}

// ValidMaskAction 校验脱敏动作是否合法。
func ValidMaskAction(action string) bool {
	switch action {
	case MaskActionPartial, MaskActionTokenize, MaskActionRedact:
		return true
	}
	return false
}

// ListMaskRules 返回全部生效中的脱敏规则（按资源路径稳定排序，便于生成摘要）。
func ListMaskRules() ([]*MaskRuleRecord, error) {
	db := getDB()
	rules := []*MaskRuleRecord{}
	err := db.Select(&rules, `
		select id, conn_id, schema_name, table_name, column_name, action, enabled, updated_at
		from t_data_mask_rule
		where enabled = 1
		order by conn_id, schema_name, table_name, column_name`)
	if err != nil {
		return nil, err
	}
	return rules, nil
}

// 脱敏规则变更回调：由 agent 注册以立即刷新本地规则快照（避免最长 5s 的旧规则窗口）。
var (
	maskRuleChangedMu        sync.Mutex
	maskRuleChangedCallbacks []func()
)

// OnMaskRulesChanged 注册脱敏规则变更回调。
func OnMaskRulesChanged(fn func()) {
	if fn == nil {
		return
	}
	maskRuleChangedMu.Lock()
	maskRuleChangedCallbacks = append(maskRuleChangedCallbacks, fn)
	maskRuleChangedMu.Unlock()
}

func notifyMaskRulesChanged() {
	maskRuleChangedMu.Lock()
	callbacks := make([]func(), len(maskRuleChangedCallbacks))
	copy(callbacks, maskRuleChangedCallbacks)
	maskRuleChangedMu.Unlock()
	for _, fn := range callbacks {
		fn()
	}
}

// SaveMaskRule 新增或更新一条脱敏规则（按 conn+schema+table+column 唯一）。
//
// 校验失败时整条拒绝，不做部分修正：规则错误会直接影响数据出域方式，
// 必须让管理员显式修正后重新提交。
func SaveMaskRule(rule *MaskRuleRecord) error {
	if rule == nil {
		return errors.New("脱敏规则不能为空")
	}
	rule.ConnID = strings.TrimSpace(rule.ConnID)
	rule.SchemaName = strings.TrimSpace(rule.SchemaName)
	rule.TableName = strings.TrimSpace(rule.TableName)
	rule.ColumnName = strings.TrimSpace(rule.ColumnName)
	rule.Action = strings.ToLower(strings.TrimSpace(rule.Action))

	if rule.ConnID == "" || rule.TableName == "" || rule.ColumnName == "" {
		return errors.New("脱敏规则缺少连接、表或列")
	}
	if len(rule.ConnID) > maskRuleMaxTextLen || len(rule.SchemaName) > maskRuleMaxTextLen ||
		len(rule.TableName) > maskRuleMaxTextLen || len(rule.ColumnName) > maskRuleMaxTextLen {
		return errors.New("脱敏规则的资源标识过长")
	}
	if !ValidMaskAction(rule.Action) {
		return fmt.Errorf("未知脱敏动作 %q", rule.Action)
	}
	if rule.Enabled != 0 && rule.Enabled != 1 {
		return errors.New("脱敏规则启用状态无效")
	}

	db := getDB()
	if count, err := countMaskRules(); err != nil {
		return err
	} else if rule.ID == "" && count >= maskRuleMaxNumRules {
		return errors.New("脱敏规则数量超过限制")
	}

	// 先按唯一键更新，未命中再插入：避免依赖各数据库不同的 UPSERT 语法。
	// 放在事务内，防止并发提交产生重复规则。
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		update t_data_mask_rule
		set action = ?, enabled = ?, updated_at = ?
		where conn_id = ? and schema_name = ? and table_name = ? and column_name = ?`,
		rule.Action, rule.Enabled, time.Now(),
		rule.ConnID, rule.SchemaName, rule.TableName, rule.ColumnName)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		if rule.ID == "" {
			rule.ID = idgen.RandomStr()
		}
		if _, err := tx.Exec(`
			insert into t_data_mask_rule
				(id, conn_id, schema_name, table_name, column_name, action, enabled, updated_at)
			values (?, ?, ?, ?, ?, ?, ?, ?)`,
			rule.ID, rule.ConnID, rule.SchemaName, rule.TableName, rule.ColumnName,
			rule.Action, rule.Enabled, time.Now()); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	notifyMaskRulesChanged()
	return nil
}

// DeleteMaskRule 删除一条脱敏规则（按 id）。
func DeleteMaskRule(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("缺少脱敏规则 ID")
	}
	_, err := getDB().Exec("delete from t_data_mask_rule where id = ?", id)
	if err != nil {
		return err
	}
	notifyMaskRulesChanged()
	return nil
}

func countMaskRules() (int, error) {
	var count int
	if err := getDB().Get(&count, "select count(1) from t_data_mask_rule"); err != nil {
		return 0, err
	}
	return count, nil
}

// MaskRulesDigest 计算当前生效规则的摘要（排序后 sha256 前 16 位）。
//
// 用于消息安全元数据中的 MaskVersion：规则变化后旧 token 与旧脱敏视图不可混用。
func MaskRulesDigest() (string, error) {
	rules, err := ListMaskRules()
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		parts = append(parts, strings.Join([]string{
			r.ConnID, r.SchemaName, r.TableName, strings.ToLower(r.ColumnName), r.Action,
		}, "|"))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])[:16], nil
}

// EnsureMaskKey 返回（必要时生成）脱敏 tokenize 使用的 HMAC 密钥及版本号。
//
// 密钥保存在管理库 t_system_config，仅在服务端使用：不进入 Prompt、日志与会话。
// 版本号随密钥轮换递增，用于隔离旧 token 历史。
func EnsureMaskKey() ([]byte, string, error) {
	db := getDB()

	var encoded string
	err := db.Get(&encoded, "select config_value from t_system_config where config_key = ?", maskKeyConfigKey)
	switch {
	case err == nil && strings.TrimSpace(encoded) != "":
		key, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if decErr != nil || len(key) < maskKeyMinBytes {
			return nil, "", errors.New("脱敏密钥格式无效，请管理员重置 ai.maskKey")
		}
		return key, maskKeyVersion(), nil
	case err != nil && !isNoRowsErr(err):
		return nil, "", err
	}

	// 首次启动生成 32 字节随机密钥
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, "", err
	}
	encoded = base64.StdEncoding.EncodeToString(key)
	if _, err := db.Exec(`
		insert into t_system_config (id, config_key, config_value, config_type, remark)
		values (?, ?, ?, 'ai', '脱敏 tokenize HMAC 密钥（服务端专用）')`,
		idgen.RandomStr(), maskKeyConfigKey, encoded); err != nil {
		// 并发初始化冲突：回读一次，以库内值为准
		var existing string
		if readErr := db.Get(&existing, "select config_value from t_system_config where config_key = ?", maskKeyConfigKey); readErr == nil && existing != "" {
			if k, decErr := base64.StdEncoding.DecodeString(existing); decErr == nil && len(k) >= maskKeyMinBytes {
				return k, maskKeyVersion(), nil
			}
		}
		return nil, "", err
	}
	return key, maskKeyVersion(), nil
}

func maskKeyVersion() string {
	var version string
	if err := getDB().Get(&version, "select config_value from t_system_config where config_key = ?", maskKeyVersionKey); err != nil || strings.TrimSpace(version) == "" {
		return "v1"
	}
	return strings.TrimSpace(version)
}

func isNoRowsErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no rows") || strings.Contains(msg, "sql: no rows")
}
