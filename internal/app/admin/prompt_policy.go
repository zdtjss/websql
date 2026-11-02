package admin

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// 提示词库写入约束。
//
// 提示词会进入系统上下文，必须限制体积；角色提示词影响同角色所有用户，
// 只能由管理员维护。权限判定保持为纯函数，便于单测覆盖，handler 只负责取数。
const (
	promptMaxTitleRunes   = 100
	promptMaxContentRunes = 8000
	promptMaxSharedUsers  = 50
)

var (
	errPromptTitleEmpty   = errors.New("标题不能为空")
	errPromptTitleTooLong = errors.New("标题过长")
	errPromptContentEmpty = errors.New("内容不能为空")
	errPromptContentLong  = errors.New("内容过长")
	errPromptShareTooMany = errors.New("分享用户过多")
)

// isRolePrompt 判断是否为角色提示词（role_id 非空）。
func isRolePrompt(p *Prompt) bool {
	return p != nil && p.RoleId != nil && strings.TrimSpace(*p.RoleId) != ""
}

// canReadPrompt 判断用户能否读取提示词正文：
// 管理员、创建者、同角色用户、被分享用户四者之一。
func canReadPrompt(p *Prompt, userID string, roleIDs []string, sharedToMe bool) bool {
	if p == nil || userID == "" {
		return false
	}
	if isUserAdmin(userID) {
		return true
	}
	if p.CreatedBy != nil && *p.CreatedBy == userID {
		return true
	}
	if isRolePrompt(p) && containsRoleID(roleIDs, *p.RoleId) {
		return true
	}
	return sharedToMe
}

// canWritePrompt 判断用户能否更新提示词：
// 管理员可写；角色提示词仅管理员可写；个人提示词仅创建者可写。
func canWritePrompt(p *Prompt, userID string, roleIDs []string) bool {
	if p == nil || userID == "" {
		return false
	}
	if isUserAdmin(userID) {
		return true
	}
	if isRolePrompt(p) {
		return false
	}
	return p.CreatedBy != nil && *p.CreatedBy == userID
}

// canDeletePrompt 判断用户能否删除提示词：
// 管理员可删；角色提示词仅管理员可删；个人提示词仅创建者可删。
func canDeletePrompt(p *Prompt, userID string) bool {
	if p == nil || userID == "" {
		return false
	}
	if isUserAdmin(userID) {
		return true
	}
	if isRolePrompt(p) {
		return false
	}
	return p.CreatedBy != nil && *p.CreatedBy == userID
}

func containsRoleID(roleIDs []string, roleID string) bool {
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return false
	}
	for _, id := range roleIDs {
		if strings.TrimSpace(id) == roleID {
			return true
		}
	}
	return false
}

// normalizePromptTitle 去空白并校验标题长度。
func normalizePromptTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" {
		return "", errPromptTitleEmpty
	}
	if utf8.RuneCountInString(title) > promptMaxTitleRunes {
		return "", errPromptTitleTooLong
	}
	return title, nil
}

// normalizePromptContent 去空白并校验正文长度。
func normalizePromptContent(raw string) (string, error) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return "", errPromptContentEmpty
	}
	if utf8.RuneCountInString(content) > promptMaxContentRunes {
		return "", errPromptContentLong
	}
	return content, nil
}

// effectiveSharedUserIDs 角色提示词按角色分发，不支持再单独分享给个别用户，
// 传入的分享列表一律忽略；个人提示词原样返回。
func effectiveSharedUserIDs(rolePrompt bool, ids []string) []string {
	if rolePrompt {
		return nil
	}
	return ids
}

// excludeUserID 从分享列表中移除指定用户，避免产生“分享给自己”的无意义记录。
func excludeUserID(ids []string, userID string) []string {
	if len(ids) == 0 || userID == "" {
		return ids
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == userID {
			continue
		}
		out = append(out, id)
	}
	return out
}

// normalizePromptSharedUserIDs 去空白、去重并校验分享数量。
func normalizePromptSharedUserIDs(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) > promptMaxSharedUsers {
		return nil, errPromptShareTooMany
	}
	return out, nil
}
