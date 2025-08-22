package admin

import (
	"errors"
	"fmt"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	"go-web/utils/store"

	"github.com/gin-gonic/gin"
)

func Login(c *gin.Context) {
	loginName := c.GetString("name")
	pwd := c.GetString("password")
	key := c.GetString("key")
	loginType := c.GetString("loginType")

	var user *User
	switch loginType {
	case "pwd":
		user = findByLoginName(loginName)
		if user == nil || user.Pwd != Md5sum(pwd) {
			logutils.PanicErr(errors.New("用户名或密码不正确"))
		}
		user.LoginName = loginName
	case "bio":
		user = findByBio(key)
		if user == nil {
			logutils.PanicErr(errors.New("无效的指纹/面容信息"))
		}
	case "token":
		user = findByToken(key)
		if user == nil {
			logutils.PanicErr(errors.New("传入的登录信息无效"))
		}
	}
	power := findUserPower(user.Id)
	token := Md5sum(utils.RandomStr())
	if loginType == "token" {
		token = key
	}
	c.Header("Authentication", token)
	store.Add(formatStoreKey(token), UserPower{UserId: user.Id, Power: power})
	user.Pwd = ""
	store.Add(formatStoreKey(token+"_user"), user)
	utils.WriteJson(c.Writer, map[string]any{"id": user.Id, "name": user.Name, "isAdmin": user.Id == config.AdminId, "authentication": token})
}

func Logout(c *gin.Context) {
	key := c.GetHeader("Authentication")
	store.Remove(formatStoreKey(key))
	utils.WriteJson(c.Writer, "退出成功")
}

func GetUserPower(authorization string) *UserPower {
	var userPower = new(UserPower)
	store.Get(formatStoreKey(authorization), userPower)
	return userPower
}

func GetUser(authorization string) *User {
	var user = new(User)
	store.Get(formatStoreKey(authorization+"_user"), user)
	return user
}

func CheckAdminPower(c *gin.Context) {
	// 非远程模式下不做权限管理
	if !config.Cfg.IsRemote {
		return
	}
	authorization := c.GetHeader("Authorization")
	var userPower = GetUserPower(authorization)
	if userPower.UserId != config.AdminId {
		logutils.PanicErr(errors.New("无权访问"))
	}
}

func formatStoreKey(key string) string {
	return fmt.Sprintf("USER:POWER:%s", key)
}
