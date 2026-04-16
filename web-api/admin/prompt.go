package admin

import (
	"errors"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type Prompt struct {
	Id            string       `json:"id" db:"id"`
	Title         string       `json:"title" db:"title"`
	Content       string       `json:"content" db:"content"`
	CreatedBy     *string      `json:"createdBy" db:"created_by"`
	RoleId        *string      `json:"roleId" db:"role_id"`
	RoleName      string       `json:"roleName,omitempty" db:"-"`
	CreatedAt     *string      `json:"createdAt,omitempty" db:"created_at"`
	UpdatedAt     *string      `json:"updatedAt,omitempty" db:"updated_at"`
	CurrentUserId string       `json:"currentUserId" db:"-"`
	IsShared      bool         `json:"isShared" db:"-"`
	IsRolePrompt  bool         `json:"isRolePrompt" db:"-"`
	SharedByName  string       `json:"sharedByName,omitempty" db:"-"`
	SharedUserIds []string     `json:"sharedUserIds,omitempty" db:"-"`
	SharedUsers   []SharedUser `json:"sharedUsers,omitempty" db:"-"`
}

type SharedUser struct {
	Id        string `json:"id" db:"id"`
	Name      string `json:"name" db:"name"`
	LoginName string `json:"loginName" db:"login_name"`
}

type PromptSave struct {
	Id            string   `json:"id"`
	Title         string   `json:"title"`
	Content       string   `json:"content"`
	RoleId        string   `json:"roleId"`
	SharedUserIds []string `json:"sharedUserIds"`
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
	config.Mngtdb.Select(&userRoleIds, "select role_id from t_user_role where user_id = ?", userId)

	prompts := []*Prompt{}

	if len(userRoleIds) > 0 {
		placeholders := strings.Repeat("?,", len(userRoleIds))
		placeholders = placeholders[:len(placeholders)-1]
		args := []interface{}{userId, userId}
		for _, rid := range userRoleIds {
			args = append(args, rid)
		}

		err := config.Mngtdb.Select(&prompts,
			`select p.id, p.title, p.content, p.created_by, p.role_id, p.created_at, p.updated_at
			from t_prompt p
			where p.created_by = ?
			union
			select p.id, p.title, p.content, p.created_by, p.role_id, p.created_at, p.updated_at
			from t_prompt p
			inner join t_prompt_share ps on p.id = ps.prompt_id
			where ps.shared_to = ?
			union
			select p.id, p.title, p.content, p.created_by, p.role_id, p.created_at, p.updated_at
			from t_prompt p
			where p.role_id in (`+placeholders+`)
			order by updated_at desc`,
			args...)
		logutils.PanicErr(err)
	} else {
		err := config.Mngtdb.Select(&prompts,
			`select p.id, p.title, p.content, p.created_by, p.role_id, p.created_at, p.updated_at
			from t_prompt p
			where p.created_by = ?
			union
			select p.id, p.title, p.content, p.created_by, p.role_id, p.created_at, p.updated_at
			from t_prompt p
			inner join t_prompt_share ps on p.id = ps.prompt_id
			where ps.shared_to = ?
			order by updated_at desc`,
			userId, userId)
		logutils.PanicErr(err)
	}

	if prompts == nil {
		prompts = []*Prompt{}
	}

	for _, p := range prompts {
		p.CurrentUserId = userId
		if p.RoleId != nil && *p.RoleId != "" {
			p.IsRolePrompt = true
			var roleName string
			err := config.Mngtdb.Get(&roleName, "select name from t_role where id = ?", *p.RoleId)
			if err == nil && roleName != "" {
				p.RoleName = roleName
			}
		}
		p.IsShared = p.CreatedBy != nil && *p.CreatedBy != userId
		if p.IsShared && !p.IsRolePrompt {
			var sharerName string
			err := config.Mngtdb.Get(&sharerName, "select name from t_user where id = ?", *p.CreatedBy)
			if err == nil && sharerName != "" {
				p.SharedByName = sharerName + " 分享"
			} else {
				p.SharedByName = "他人分享"
			}
		}
	}

	utils.WriteJson(c.Writer, prompts)
}

func PromptListByRole(c *gin.Context) {
	CheckAdminPower(c)
	roleId := c.Query("roleId")
	if roleId == "" {
		utils.WriteJson(c.Writer, []*Prompt{})
		return
	}

	prompts := []*Prompt{}
	err := config.Mngtdb.Select(&prompts,
		`select id, title, content, created_by, role_id, created_at, updated_at
		from t_prompt
		where role_id = ?
		order by updated_at desc`, roleId)
	logutils.PanicErr(err)

	if prompts == nil {
		prompts = []*Prompt{}
	}

	for _, p := range prompts {
		if p.CreatedBy != nil && *p.CreatedBy != "" {
			var creatorName string
			err := config.Mngtdb.Get(&creatorName, "select name from t_user where id = ?", *p.CreatedBy)
			if err == nil && creatorName != "" {
				p.SharedByName = creatorName
			}
		}
	}

	utils.WriteJson(c.Writer, prompts)
}

func PromptDetail(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		logutils.PanicErr(errors.New("缺少 id 参数"))
	}

	prompt := &Prompt{}
	err := config.Mngtdb.Get(prompt, "select * from t_prompt where id = ?", id)
	logutils.PanicErr(err)

	sharedToIds := []string{}
	config.Mngtdb.Select(&sharedToIds, "select shared_to from t_prompt_share where prompt_id = ?", id)
	prompt.SharedUserIds = sharedToIds

	if len(sharedToIds) > 0 {
		users := []struct {
			Id        string `db:"id"`
			Name      string `db:"name"`
			LoginName string `db:"login_name"`
		}{}
		config.Mngtdb.Select(&users, "select id, name, login_name from t_user where id in (?)", sharedToIds)
		prompt.SharedUsers = make([]SharedUser, 0, len(users))
		for _, u := range users {
			prompt.SharedUsers = append(prompt.SharedUsers, SharedUser{
				Id:        u.Id,
				Name:      u.Name,
				LoginName: u.LoginName,
			})
		}
	}

	utils.WriteJson(c.Writer, prompt)
}

func SavePrompt(c *gin.Context) {
	userId := getCurrentUserId(c)
	req := &PromptSave{}
	utils.UnmarshalJson(c.Request.Body, req)

	if req.Title == "" {
		logutils.PanicErr(errors.New("标题不能为空"))
	}
	if req.Content == "" {
		logutils.PanicErr(errors.New("内容不能为空"))
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	tx, _ := config.Mngtdb.Beginx()
	defer tx.Rollback()

	var roleId interface{} = nil
	if req.RoleId != "" {
		roleId = req.RoleId
	}

	if req.Id == "" {
		req.Id = utils.RandomStr()
		_, err := tx.Exec("insert into t_prompt (id, title, content, created_by, role_id, created_at, updated_at) values (?, ?, ?, ?, ?, ?, ?)",
			req.Id, req.Title, req.Content, userId, roleId, now, now)
		logutils.PanicErrf("保存提示词失败", err)
	} else {
		_, err := tx.Exec("update t_prompt set title = ?, content = ?, role_id = ?, updated_at = ? where id = ?",
			req.Title, req.Content, roleId, now, req.Id)
		logutils.PanicErrf("更新提示词失败", err)
	}

	tx.Exec("delete from t_prompt_share where prompt_id = ?", req.Id)

	if len(req.SharedUserIds) > 0 {
		for _, sharedTo := range req.SharedUserIds {
			_, err := tx.Exec("insert into t_prompt_share (id, prompt_id, shared_by, shared_to) values (?, ?, ?, ?)",
				utils.RandomStr(), req.Id, userId, sharedTo)
			logutils.PanicErrf("保存提示词分享失败", err)
		}
	}

	err := tx.Commit()
	logutils.PanicErrf("保存提示词失败", err)
	utils.WriteJson(c.Writer, "保存成功")
}

func DelPrompt(c *gin.Context) {
	userId := getCurrentUserId(c)
	id := c.Query("id")
	if id == "" {
		logutils.PanicErr(errors.New("缺少 id 参数"))
	}

	prompt := &Prompt{}
	err := config.Mngtdb.Get(prompt, "select id, created_by, role_id from t_prompt where id = ?", id)
	if err != nil {
		logutils.PanicErr(errors.New("提示词不存在"))
	}

	isCreator := prompt.CreatedBy != nil && *prompt.CreatedBy == userId
	isRoleOwner := prompt.RoleId != nil && *prompt.RoleId != ""
	if !isCreator && !isRoleOwner {
		logutils.PanicErr(errors.New("无权删除此提示词"))
	}

	tx, _ := config.Mngtdb.Beginx()
	defer tx.Rollback()

	tx.Exec("delete from t_prompt_share where prompt_id = ?", id)
	_, err = tx.Exec("delete from t_prompt where id = ?", id)
	logutils.PanicErrf("删除提示词失败", err)

	err = tx.Commit()
	logutils.PanicErrf("删除提示词失败", err)
	utils.WriteJson(c.Writer, "删除成功")
}
