package admin

import (
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"websql/internal/pkg/appctx"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
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
		response.WriteErr(c, 200, 400, "缺少 id 参数")
		return
	}

	prompt := &Prompt{}
	err := getDB().Get(prompt, "select id, title, content, created_by, role_id, schemas, tables, created_at, updated_at from t_prompt where id = ?", id)
	if err != nil {
		log.Printf("查询提示词详情失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

	sharedToIds := []string{}
	getDB().Select(&sharedToIds, "select shared_to from t_prompt_share where prompt_id = ?", id)
	prompt.SharedUserIds = sharedToIds

	if len(sharedToIds) > 0 {
		users := []struct {
			Id        string `db:"id"`
			Name      string `db:"name"`
			LoginName string `db:"login_name"`
		}{}
		getDB().Select(&users, "select id, name, login_name from t_user where id in (?)", sharedToIds)
		prompt.SharedUsers = make([]SharedUser, 0, len(users))
		for _, u := range users {
			prompt.SharedUsers = append(prompt.SharedUsers, SharedUser{
				Id:        u.Id,
				Name:      u.Name,
				LoginName: u.LoginName,
			})
		}
	}

	response.WriteOK(c, prompt)
}

func SavePrompt(c *gin.Context) {
	userId := getCurrentUserId(c)
	req := &PromptSave{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, req); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}

	if req.Title == "" {
		response.WriteErr(c, 200, 400, "标题不能为空")
		return
	}
	if req.Content == "" {
		response.WriteErr(c, 200, 400, "内容不能为空")
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	tx, _ := getDB().Beginx()
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
			req.Id, req.Title, req.Content, userId, roleId, schemasVal, tablesVal, now, now)
		if err != nil {
			log.Printf("保存提示词失败: %v", err)
			response.WriteErr(c, 200, 500, "操作失败")
			return
		}
	} else {
		_, err := tx.Exec("update t_prompt set title = ?, content = ?, role_id = ?, schemas = ?, tables = ?, updated_at = ? where id = ?",
			req.Title, req.Content, roleId, schemasVal, tablesVal, now, req.Id)
		if err != nil {
			log.Printf("更新提示词失败: %v", err)
			response.WriteErr(c, 200, 500, "操作失败")
			return
		}
	}

	tx.Exec("delete from t_prompt_share where prompt_id = ?", req.Id)

	if len(req.SharedUserIds) > 0 {
		for _, sharedTo := range req.SharedUserIds {
			_, err := tx.Exec("insert into t_prompt_share (id, prompt_id, shared_by, shared_to) values (?, ?, ?, ?)",
				idgen.RandomStr(), req.Id, userId, sharedTo)
			if err != nil {
				log.Printf("保存提示词分享失败: %v", err)
				response.WriteErr(c, 200, 500, "操作失败")
				return
			}
		}
	}

	err := tx.Commit()
	if err != nil {
		log.Printf("保存提示词失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, "保存成功")
}

func DelPrompt(c *gin.Context) {
	userId := getCurrentUserId(c)
	id := c.Query("id")
	if id == "" {
		response.WriteErr(c, 200, 400, "缺少 id 参数")
		return
	}

	prompt := &Prompt{}
	err := getDB().Get(prompt, "select id, created_by, role_id from t_prompt where id = ?", id)
	if err != nil {
		response.WriteErr(c, 200, 400, "提示词不存在")
		return
	}

	isCreator := prompt.CreatedBy != nil && *prompt.CreatedBy == userId
	isRoleOwner := prompt.RoleId != nil && *prompt.RoleId != ""
	if !isCreator && !isRoleOwner {
		response.WriteErr(c, 200, 400, "无权删除此提示词")
		return
	}

	tx, _ := getDB().Beginx()
	defer tx.Rollback()

	tx.Exec("delete from t_prompt_share where prompt_id = ?", id)
	_, err = tx.Exec("delete from t_prompt where id = ?", id)
	if err != nil {
		log.Printf("删除提示词失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("删除提示词失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, "删除成功")
}