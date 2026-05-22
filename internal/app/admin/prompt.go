package admin

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"

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
	userId, _ := c.Get("userId")
	if userId == nil {
		return ""
	}
	return userId.(string)
}

func PromptList(c *gin.Context) {
	userId := getCurrentUserId(c)

	userRoleIds := []string{}
	database.Mngtdb.Select(&userRoleIds, "select role_id from t_user_role where user_id = ?", userId)

	prompts := []*Prompt{}

	if len(userRoleIds) > 0 {
		placeholders := strings.Repeat("?,", len(userRoleIds))
		placeholders = placeholders[:len(placeholders)-1]
		args := []any{userId, userId}
		for _, rid := range userRoleIds {
			args = append(args, rid)
		}

		err := database.Mngtdb.Select(&prompts,
			`select p.id, p.title, p.content, p.created_by, p.role_id, p.schemas, p.tables, p.created_at, p.updated_at
			from t_prompt p
			where p.created_by = ?
			union
			select p.id, p.title, p.content, p.created_by, p.role_id, p.schemas, p.tables, p.created_at, p.updated_at
			from t_prompt p
			inner join t_prompt_share ps on p.id = ps.prompt_id
			where ps.shared_to = ?
			union
			select p.id, p.title, p.content, p.created_by, p.role_id, p.schemas, p.tables, p.created_at, p.updated_at
			from t_prompt p
			where p.role_id in (`+placeholders+`)
			order by updated_at desc`,
			args...)
		logger.PanicErr(err)
	} else {
		err := database.Mngtdb.Select(&prompts,
			`select p.id, p.title, p.content, p.created_by, p.role_id, p.schemas, p.tables, p.created_at, p.updated_at
			from t_prompt p
			where p.created_by = ?
			union
			select p.id, p.title, p.content, p.created_by, p.role_id, p.schemas, p.tables, p.created_at, p.updated_at
			from t_prompt p
			inner join t_prompt_share ps on p.id = ps.prompt_id
			where ps.shared_to = ?
			order by updated_at desc`,
			userId, userId)
		logger.PanicErr(err)
	}

	if prompts == nil {
		prompts = []*Prompt{}
	}

	for _, p := range prompts {
		p.CurrentUserId = userId
		if p.RoleId != nil && *p.RoleId != "" {
			p.IsRolePrompt = true
			var roleName string
			err := database.Mngtdb.Get(&roleName, "select name from t_role where id = ?", *p.RoleId)
			if err == nil && roleName != "" {
				p.RoleName = roleName
			}
		}
		p.IsShared = p.CreatedBy != nil && *p.CreatedBy != userId
		if p.IsShared && !p.IsRolePrompt {
			var sharerName string
			err := database.Mngtdb.Get(&sharerName, "select name from t_user where id = ?", *p.CreatedBy)
			if err == nil && sharerName != "" {
				p.SharedByName = sharerName + " 分享"
			} else {
				p.SharedByName = "他人分享"
			}
		}
	}

	jsonutil.WriteJson(c.Writer, prompts)
}

func PromptListByRole(c *gin.Context) {
	CheckAdminPower(c)
	roleId := c.Query("roleId")
	if roleId == "" {
		jsonutil.WriteJson(c.Writer, []*Prompt{})
		return
	}

	prompts := []*Prompt{}
	err := database.Mngtdb.Select(&prompts,
		`select id, title, content, created_by, role_id, schemas, tables, created_at, updated_at
		from t_prompt
		where role_id = ?
		order by updated_at desc`, roleId)
	logger.PanicErr(err)

	if prompts == nil {
		prompts = []*Prompt{}
	}

	for _, p := range prompts {
		if p.CreatedBy != nil && *p.CreatedBy != "" {
			var creatorName string
			err := database.Mngtdb.Get(&creatorName, "select name from t_user where id = ?", *p.CreatedBy)
			if err == nil && creatorName != "" {
				p.SharedByName = creatorName
			}
		}
	}

	jsonutil.WriteJson(c.Writer, prompts)
}

func PromptDetail(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		logger.PanicErr(errors.New("缺少 id 参数"))
	}

	prompt := &Prompt{}
	err := database.Mngtdb.Get(prompt, "select id, title, content, created_by, role_id, schemas, tables, created_at, updated_at from t_prompt where id = ?", id)
	logger.PanicErr(err)

	sharedToIds := []string{}
	database.Mngtdb.Select(&sharedToIds, "select shared_to from t_prompt_share where prompt_id = ?", id)
	prompt.SharedUserIds = sharedToIds

	if len(sharedToIds) > 0 {
		users := []struct {
			Id        string `db:"id"`
			Name      string `db:"name"`
			LoginName string `db:"login_name"`
		}{}
		database.Mngtdb.Select(&users, "select id, name, login_name from t_user where id in (?)", sharedToIds)
		prompt.SharedUsers = make([]SharedUser, 0, len(users))
		for _, u := range users {
			prompt.SharedUsers = append(prompt.SharedUsers, SharedUser{
				Id:        u.Id,
				Name:      u.Name,
				LoginName: u.LoginName,
			})
		}
	}

	jsonutil.WriteJson(c.Writer, prompt)
}

func SavePrompt(c *gin.Context) {
	userId := getCurrentUserId(c)
	req := &PromptSave{}
	jsonutil.UnmarshalJson(c.Request.Body, req)

	if req.Title == "" {
		logger.PanicErr(errors.New("标题不能为空"))
	}
	if req.Content == "" {
		logger.PanicErr(errors.New("内容不能为空"))
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	tx, _ := database.Mngtdb.Beginx()
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
		logger.PanicErrf("保存提示词失败", err)
	} else {
		_, err := tx.Exec("update t_prompt set title = ?, content = ?, role_id = ?, schemas = ?, tables = ?, updated_at = ? where id = ?",
			req.Title, req.Content, roleId, schemasVal, tablesVal, now, req.Id)
		logger.PanicErrf("更新提示词失败", err)
	}

	tx.Exec("delete from t_prompt_share where prompt_id = ?", req.Id)

	if len(req.SharedUserIds) > 0 {
		for _, sharedTo := range req.SharedUserIds {
			_, err := tx.Exec("insert into t_prompt_share (id, prompt_id, shared_by, shared_to) values (?, ?, ?, ?)",
				idgen.RandomStr(), req.Id, userId, sharedTo)
			logger.PanicErrf("保存提示词分享失败", err)
		}
	}

	err := tx.Commit()
	logger.PanicErrf("保存提示词失败", err)
	jsonutil.WriteJson(c.Writer, "保存成功")
}

func DelPrompt(c *gin.Context) {
	userId := getCurrentUserId(c)
	id := c.Query("id")
	if id == "" {
		logger.PanicErr(errors.New("缺少 id 参数"))
	}

	prompt := &Prompt{}
	err := database.Mngtdb.Get(prompt, "select id, created_by, role_id from t_prompt where id = ?", id)
	if err != nil {
		logger.PanicErr(errors.New("提示词不存在"))
	}

	isCreator := prompt.CreatedBy != nil && *prompt.CreatedBy == userId
	isRoleOwner := prompt.RoleId != nil && *prompt.RoleId != ""
	if !isCreator && !isRoleOwner {
		logger.PanicErr(errors.New("无权删除此提示词"))
	}

	tx, _ := database.Mngtdb.Beginx()
	defer tx.Rollback()

	tx.Exec("delete from t_prompt_share where prompt_id = ?", id)
	_, err = tx.Exec("delete from t_prompt where id = ?", id)
	logger.PanicErrf("删除提示词失败", err)

	err = tx.Commit()
	logger.PanicErrf("删除提示词失败", err)
	jsonutil.WriteJson(c.Writer, "删除成功")
}