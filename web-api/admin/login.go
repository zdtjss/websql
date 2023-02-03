package admin

import (
	"errors"
	"fmt"
	"go-web/logutils"
	"go-web/utils"
	"go-web/utils/store"
	"net/http"
)

func Login(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	name := r.PostForm.Get("name")
	pwd := r.PostForm.Get("password")

	if name != "" {
		user := findByLoginName(name)
		if user.Pwd == Md5sum(pwd) {
			power := findUserPower(user.Id)
			key := Md5sum(fmt.Sprint(utils.RandomInt64()))
			w.Header().Set("Authentication", key)
			store.StoreItem(key, UserPower{UserId: user.Id, Power: power})
			utils.WriteJson(w, map[string]any{"name": user.Name, "isAdmin": user.Id == 1})
		} else {
			logutils.Panicln(errors.New("用户名或密码不正确"))
		}
	} else {
		logutils.Panicln(errors.New("登录失败"))
	}
}

func Logout(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Authentication")
	store.RemoveItem(key)
	utils.WriteJson(w, "退出成功")
}

func CheckPower(r *http.Request) {
	authorization := r.Header.Get("Authorization")
	userPower := store.GetItem(authorization)
	if userPower == nil || userPower.(UserPower).UserId != 1 {
		logutils.Panicln(errors.New("无权访问"))
	}
}
