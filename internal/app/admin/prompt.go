package admin

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"websql/internal/pkg/appctx"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type Prompt struct {
	Id            string             `json:"id" db:"id"`
	Title         string             `json:"title" db:"title"`
	Content       string             `json:"content" db:"content"`
	CreatedBy     *string            `json:"createdBy" db:"created_by"`
	RoleId        *string            `json:"roleId" db:"role_id"`
	ConnSchemas   ConnSchemasJSON    `json:"connSchemas,omitempty" db:"schemas"`
	Tables        PromptTableRefJSON `json:"tables,omitempty" db:"tables"`
	RoleName      string             `json:"roleName,omitempty" db:"-"`
	CreatedAt     *string            `json:"createdAt,omitempty" db:"created_at"`
	UpdatedAt     *string            `json:"updatedAt,omitempty" db:"updated_at"`
	CurrentUserId string             `json:"currentUserId" db:"-"`
	IsShared      bool               `json:"isShared" db:"-"`
	IsRolePrompt  bool               `json:"isRolePrompt" db:"-"`
	CanEdit       bool               `json:"canEdit" db:"-"`
	CanDelete     bool               `json:"canDelete" db:"-"`
	SharedByName  string             `json:"sharedByName,omitempty" db:"-"`
	SharedUserIds []string           `json:"sharedUserIds,omitempty" db:"-"`
	SharedUsers   []SharedUser       `json:"sharedUsers,omitempty" db:"-"`
}

type ConnSchemaRef struct {
	ConnId string `json:"connId"`
	Schema string `json:"schema"`
}

type ConnSchemasJSON []ConnSchemaRef

func (cs *ConnSchemasJSON) Scan(src any) error {
	if src == nil {
		*cs = nil
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return errors.New("unsupported type for ConnSchemasJSON")
	}
	if len(b) == 0 {
		*cs = nil
		return nil
	}
	return json.Unmarshal(b, cs)
}

type PromptTableRef struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
}

type PromptTableRefJSON []PromptTableRef

func (tr *PromptTableRefJSON) Scan(src any) error {
	if src == nil {
		*tr = nil
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return errors.New("unsupported type for PromptTableRefJSON")
	}
	if len(b) == 0 {
		*tr = nil
		return nil
	}
	raw := string(b)
	var oldFormat []string
	if err := json.Unmarshal(b, &oldFormat); err == nil {
		result := make([]PromptTableRef, len(oldFormat))
		for i, s := range oldFormat {
			result[i] = PromptTableRef{Name: s}
		}
		*tr = result
		return nil
	}
	return json.Unmarshal([]byte(raw), tr)
}

type StringArrayJSON []string

func (sa *StringArrayJSON) Scan(src any) error {
	if src == nil {
		*sa = nil
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return errors.New("unsupported type for StringArrayJSON")
	}
	if len(b) == 0 {
		*sa = nil
		return nil
	}
	return json.Unmarshal(b, sa)
}

type PromptSave struct {
	Id            string           `json:"id"`
	Title         string           `json:"title"`
	Content       string           `json:"content"`
	RoleId        string           `json:"roleId"`
	ConnSchemas   []ConnSchemaRef  `json:"connSchemas"`
	Tables        []PromptTableRef `json:"tables"`
	SharedUserIds []string         `json:"sharedUserIds"`
}

type PromptShare struct {
	Id       string `json:"id" db:"id"`
	PromptId string `json:"promptId" db:"prompt_id"`
	SharedBy string `json:"sharedBy" db:"shared_by"`
	SharedTo string `json:"sharedTo" db:"shared_to"`
}

func getCurrentUserId(c *gin.Context) string {
	return appctx.Ctx.GetUserID(c)
}

func PromptList(c *gin.Context) {
	userId := getCurrentUserId(c)

	tab := strings.TrimSpace(c.Query("tab"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "0"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset := (page - 1) * pageSize

	userRoleIds := []string{}
	getDB().Select(&userRoleIds, "select role_id from t_user_role where user_id = ?", userId)

	keywordCond := ""
	if keyword != "" {
		keywordCond = " and p.title like ?"
	}

	tabCond := ""
	switch tab {
	case "mine":
		tabCond = " and (t.role_id is null or t.role_id = '')"
	case "system":
		tabCond = " and (t.role_id is not null and t.role_id <> '')"
	}

	var innerSql string
	var countSql string
	var args []any
	var countArgs []any

	if len(userRoleIds) > 0 {
		rolePlaceholders := strings.Repeat("?,", len(userRoleIds))
		rolePlaceholders = rolePlaceholders[:len(rolePlaceholders)-1]
		innerSql = `
			select p.id, p.title, p.content, p.created_by, p.role_id, p.schemas, p.tables, p.created_at, p.updated_at
			from t_prompt p
			where p.created_by = ?` + keywordCond + `
			union
			select p.id, p.title, p.content, p.created_by, p.role_id, p.schemas, p.tables, p.created_at, p.updated_at
			from t_prompt p
			inner join t_prompt_share ps on p.id = ps.prompt_id
			where ps.shared_to = ?` + keywordCond + `
			union
			select p.id, p.title, p.content, p.created_by, p.role_id, p.schemas, p.tables, p.created_at, p.updated_at
			from t_prompt p
			where p.role_id in (` + rolePlaceholders + `)` + keywordCond
		args = []any{userId}
		countArgs = []any{userId}
		if keyword != "" {
			args = append(args, "%"+keyword+"%")
			countArgs = append(countArgs, "%"+keyword+"%")
		}
		args = append(args, userId)
		countArgs = append(countArgs, userId)
		if keyword != "" {
			args = append(args, "%"+keyword+"%")
			countArgs = append(countArgs, "%"+keyword+"%")
		}
		for _, rid := range userRoleIds {
			args = append(args, rid)
			countArgs = append(countArgs, rid)
		}
		if keyword != "" {
			args = append(args, "%"+keyword+"%")
			countArgs = append(countArgs, "%"+keyword+"%")
		}
	} else {
		innerSql = `
			select p.id, p.title, p.content, p.created_by, p.role_id, p.schemas, p.tables, p.created_at, p.updated_at
			from t_prompt p
			where p.created_by = ?` + keywordCond + `
			union
			select p.id, p.title, p.content, p.created_by, p.role_id, p.schemas, p.tables, p.created_at, p.updated_at
			from t_prompt p
			inner join t_prompt_share ps on p.id = ps.prompt_id
			where ps.shared_to = ?` + keywordCond
		args = []any{userId}
		countArgs = []any{userId}
		if keyword != "" {
			args = append(args, "%"+keyword+"%")
			countArgs = append(countArgs, "%"+keyword+"%")
		}
		args = append(args, userId)
		countArgs = append(countArgs, userId)
		if keyword != "" {
			args = append(args, "%"+keyword+"%")
			countArgs = append(countArgs, "%"+keyword+"%")
		}
	}

	var countResult int
	countSql = "select count(*) from (" + innerSql + ") t where 1=1" + tabCond
	if err := getDB().Get(&countResult, countSql, countArgs...); err != nil {
		log.Printf("查询提示词数量失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

	pagedSql := "select * from (" + innerSql + ") t where 1=1" + tabCond +
		" order by updated_at desc limit ? offset ?"
	args = append(args, pageSize, offset)

	prompts := []*Prompt{}
	if err := getDB().Select(&prompts, pagedSql, args...); err != nil {
		log.Printf("查询提示词列表失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

	if prompts == nil {
		prompts = []*Prompt{}
	}

	for _, p := range prompts {
		p.CurrentUserId = userId
		if p.RoleId != nil && *p.RoleId != "" {
			p.IsRolePrompt = true
			var roleName string
			err := getDB().Get(&roleName, "select name from t_role where id = ?", *p.RoleId)
			if err == nil && roleName != "" {
				p.RoleName = roleName
			}
		}
		p.IsShared = p.CreatedBy != nil && *p.CreatedBy != userId
		p.CanEdit = canWritePrompt(p, userId, userRoleIds)
		p.CanDelete = canDeletePrompt(p, userId)
		if p.IsShared && !p.IsRolePrompt {
			var sharerName string
			err := getDB().Get(&sharerName, "select name from t_user where id = ?", *p.CreatedBy)
			if err == nil && sharerName != "" {
				p.SharedByName = sharerName + " 分享"
			} else {
				p.SharedByName = "他人分享"
			}
		}
	}

	response.WriteOK(c, gin.H{
		"items":    prompts,
		"total":    countResult,
		"page":     page,
		"pageSize": pageSize,
	})
}

func PromptListByRole(c *gin.Context) {
	if !CheckAdminPower(c) {
		return
	}
	roleId := c.Query("roleId")
	if roleId == "" {
		response.WriteOK(c, []*Prompt{})
		return
	}

	prompts := []*Prompt{}
	err := getDB().Select(&prompts,
		`select id, title, content, created_by, role_id, schemas, tables, created_at, updated_at
		from t_prompt
		where role_id = ?
		order by updated_at desc`, roleId)
	if err != nil {
		log.Printf("查询角色提示词失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

	if prompts == nil {
		prompts = []*Prompt{}
	}

	for _, p := range prompts {
		if p.CreatedBy != nil && *p.CreatedBy != "" {
			var creatorName string
			err := getDB().Get(&creatorName, "select name from t_user where id = ?", *p.CreatedBy)
			if err == nil && creatorName != "" {
				p.SharedByName = creatorName
			}
		}
	}

	response.WriteOK(c, prompts)
}

func PromptDetail(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		response.WriteErr(c, http.StatusBadRequest, 400, "缺少 id 参数")
		return
	}

	userId := getCurrentUserId(c)

	prompt := &Prompt{}
	err := getDB().Get(prompt, "select id, title, content, created_by, role_id, schemas, tables, created_at, updated_at from t_prompt where id = ?", id)
	if err != nil {
		response.WriteErr(c, http.StatusBadRequest, 400, "提示词不存在")
		return
	}

	// 读取权限：管理员 / 创建者 / 同角色 / 被分享用户。
	// 不满足时不能返回正文与分享名单，避免通过 id 枚举他人提示词。
	roleIDs := promptUserRoleIDs(userId)
	sharedToMe := promptSharedToMe(id, userId)
	if !canReadPrompt(prompt, userId, roleIDs, sharedToMe) {
		response.WriteErr(c, http.StatusForbidden, 403, "无权查看该提示词")
		return
	}

	// 分享名单只对可写用户（创建者/管理员）展示，避免泄露其他接收者。
	if canWritePrompt(prompt, userId, roleIDs) {
		sharedToIds := []string{}
		getDB().Select(&sharedToIds, "select shared_to from t_prompt_share where prompt_id = ?", id)
		prompt.SharedUserIds = sharedToIds

		if len(sharedToIds) > 0 {
			users := []struct {
				Id        string `db:"id"`
				Name      string `db:"name"`
				LoginName string `db:"login_name"`
			}{}
			// IN (?) 必须经 sqlx.In 展开切片参数，否则会被当成单个不支持的类型。
			query, args, inErr := sqlx.In("select id, name, login_name from t_user where id in (?)", sharedToIds)
			if inErr == nil {
				if err := getDB().Select(&users, getDB().Rebind(query), args...); err != nil {
					log.Printf("查询分享用户失败: %v", err)
				}
			}
			prompt.SharedUsers = make([]SharedUser, 0, len(users))
			for _, u := range users {
				prompt.SharedUsers = append(prompt.SharedUsers, SharedUser{
					Id:        u.Id,
					Name:      u.Name,
					LoginName: u.LoginName,
				})
			}
		}
	}

	response.WriteOK(c, prompt)
}

// promptUserRoleIDs 返回当前用户的角色 ID（用于判断角色提示词可读）。
func promptUserRoleIDs(userId string) []string {
	roleIDs := []string{}
	if err := getDB().Select(&roleIDs, "select role_id from t_user_role where user_id = ?", userId); err != nil {
		log.Printf("查询用户角色失败: %v", err)
		return nil
	}
	return roleIDs
}

// promptSharedToMe 判断提示词是否分享给当前用户。
func promptSharedToMe(promptId, userId string) bool {
	var count int
	if err := getDB().Get(&count, "select count(*) from t_prompt_share where prompt_id = ? and shared_to = ?", promptId, userId); err != nil {
		log.Printf("查询提示词分享关系失败: %v", err)
		return false
	}
	return count > 0
}

// promptRoleExists 校验角色 ID 是否存在，避免写入孤儿角色 ID。
func promptRoleExists(roleId string) bool {
	var count int
	if err := getDB().Get(&count, "select count(*) from t_role where id = ?", roleId); err != nil {
		log.Printf("校验角色失败: %v", err)
		return false
	}
	return count > 0
}

// promptUsersExist 校验分享目标用户是否全部存在。
func promptUsersExist(userIds []string) bool {
	if len(userIds) == 0 {
		return true
	}
	// IN (?) 必须经 sqlx.In 展开切片参数，否则查询会直接失败并误判为“用户不存在”。
	query, args, err := sqlx.In("select count(*) from t_user where id in (?)", userIds)
	if err != nil {
		log.Printf("构造分享用户查询失败: %v", err)
		return false
	}
	var count int
	if err := getDB().Get(&count, getDB().Rebind(query), args...); err != nil {
		log.Printf("校验分享用户失败: %v", err)
		return false
	}
	return count == len(userIds)
}

func SavePrompt(c *gin.Context) {
	userId := getCurrentUserId(c)
	req := &PromptSave{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, req); err != nil {
		response.WriteErr(c, http.StatusBadRequest, 400, "请求参数解析失败")
		return
	}

	title, err := normalizePromptTitle(req.Title)
	if err != nil {
		response.WriteErr(c, http.StatusBadRequest, 400, err.Error())
		return
	}
	content, err := normalizePromptContent(req.Content)
	if err != nil {
		response.WriteErr(c, http.StatusBadRequest, 400, err.Error())
		return
	}
	sharedUserIds, err := normalizePromptSharedUserIDs(req.SharedUserIds)
	if err != nil {
		response.WriteErr(c, http.StatusBadRequest, 400, err.Error())
		return
	}
	req.RoleId = strings.TrimSpace(req.RoleId)
	// 分享给自己没有意义；角色提示词按角色分发，不接受单独分享。
	sharedUserIds = excludeUserID(sharedUserIds, userId)
	sharedUserIds = effectiveSharedUserIDs(req.RoleId != "", sharedUserIds)

	isAdmin := isUserAdmin(userId)
	roleIDs := promptUserRoleIDs(userId)

	if req.Id == "" {
		// 新增：角色提示词仅管理员可建，普通用户只能建个人提示词。
		if req.RoleId != "" && !isAdmin {
			response.WriteErr(c, http.StatusForbidden, 403, "无权创建角色提示词")
			return
		}
		if req.RoleId != "" && !promptRoleExists(req.RoleId) {
			response.WriteErr(c, http.StatusBadRequest, 400, "角色不存在")
			return
		}
	} else {
		existing := &Prompt{}
		if err := getDB().Get(existing, "select id, created_by, role_id from t_prompt where id = ?", req.Id); err != nil {
			response.WriteErr(c, http.StatusBadRequest, 400, "提示词不存在")
			return
		}
		if !canWritePrompt(existing, userId, roleIDs) {
			response.WriteErr(c, http.StatusForbidden, 403, "无权修改该提示词")
			return
		}
		// 普通用户不得把个人提示词改成角色提示词，也不得改动既有角色归属。
		if !isAdmin && req.RoleId != "" {
			response.WriteErr(c, http.StatusForbidden, 403, "无权修改角色提示词")
			return
		}
		if req.RoleId != "" && !promptRoleExists(req.RoleId) {
			response.WriteErr(c, http.StatusBadRequest, 400, "角色不存在")
			return
		}
	}

	if len(sharedUserIds) > 0 && !promptUsersExist(sharedUserIds) {
		response.WriteErr(c, http.StatusBadRequest, 400, "分享用户不存在")
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	tx, err := getDB().Beginx()
	if err != nil {
		log.Printf("开始提示词事务失败: %v", err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "操作失败")
		return
	}
	defer tx.Rollback()

	var roleId any
	if req.RoleId != "" {
		roleId = req.RoleId
	}

	var schemasVal any
	if len(req.ConnSchemas) > 0 {
		csBytes, err := json.Marshal(req.ConnSchemas)
		if err == nil {
			schemasVal = string(csBytes)
		}
	}

	var tablesVal any
	if len(req.Tables) > 0 {
		tBytes, err := json.Marshal(req.Tables)
		if err == nil {
			tablesVal = string(tBytes)
		}
	}

	if req.Id == "" {
		req.Id = idgen.RandomStr()
		_, err := tx.Exec("insert into t_prompt (id, title, content, created_by, role_id, schemas, tables, created_at, updated_at) values (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			req.Id, title, content, userId, roleId, schemasVal, tablesVal, now, now)
		if err != nil {
			log.Printf("保存提示词失败: %v", err)
			response.WriteErr(c, http.StatusInternalServerError, 500, "操作失败")
			return
		}
	} else {
		_, err := tx.Exec("update t_prompt set title = ?, content = ?, role_id = ?, schemas = ?, tables = ?, updated_at = ? where id = ?",
			title, content, roleId, schemasVal, tablesVal, now, req.Id)
		if err != nil {
			log.Printf("更新提示词失败: %v", err)
			response.WriteErr(c, http.StatusInternalServerError, 500, "操作失败")
			return
		}
	}

	if _, err := tx.Exec("delete from t_prompt_share where prompt_id = ?", req.Id); err != nil {
		log.Printf("清理提示词分享失败: %v", err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "操作失败")
		return
	}

	for _, sharedTo := range sharedUserIds {
		_, err := tx.Exec("insert into t_prompt_share (id, prompt_id, shared_by, shared_to) values (?, ?, ?, ?)",
			idgen.RandomStr(), req.Id, userId, sharedTo)
		if err != nil {
			log.Printf("保存提示词分享失败: %v", err)
			response.WriteErr(c, http.StatusInternalServerError, 500, "操作失败")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("保存提示词失败: %v", err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "操作失败")
		return
	}
	response.WriteOK(c, "保存成功")
}

func DelPrompt(c *gin.Context) {
	userId := getCurrentUserId(c)
	id := c.Query("id")
	if id == "" {
		response.WriteErr(c, http.StatusBadRequest, 400, "缺少 id 参数")
		return
	}

	prompt := &Prompt{}
	err := getDB().Get(prompt, "select id, created_by, role_id from t_prompt where id = ?", id)
	if err != nil {
		response.WriteErr(c, http.StatusBadRequest, 400, "提示词不存在")
		return
	}

	// 角色提示词仅管理员可删；个人提示词仅创建者或管理员可删。
	if !canDeletePrompt(prompt, userId) {
		response.WriteErr(c, http.StatusForbidden, 403, "无权删除该提示词")
		return
	}

	tx, err := getDB().Beginx()
	if err != nil {
		log.Printf("开始提示词事务失败: %v", err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "操作失败")
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec("delete from t_prompt_share where prompt_id = ?", id); err != nil {
		log.Printf("删除提示词分享失败: %v", err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "操作失败")
		return
	}
	_, err = tx.Exec("delete from t_prompt where id = ?", id)
	if err != nil {
		log.Printf("删除提示词失败: %v", err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "操作失败")
		return
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("删除提示词失败: %v", err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "操作失败")
		return
	}
	response.WriteOK(c, "删除成功")
}
