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

	user := findByLoginName(name)
	if user != nil && user.Pwd == Md5sum(pwd) {
		power := findUserPower(user.Id)
		key := Md5sum(fmt.Sprint(utils.RandomInt64()))
		w.Header().Set("Authentication", key)
		store.Add(key, UserPower{UserId: user.Id, Power: power})
		utils.WriteJson(w, map[string]any{"name": user.Name, "isAdmin": user.Id == 1})
	} else {
		logutils.PanicErr(errors.New("用户名或密码不正确"))
	}
}

func Logout(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Authentication")
	store.Remove(key)
	utils.WriteJson(w, "退出成功")
}

func CheckAdminPower(r *http.Request) {
	authorization := r.Header.Get("Authorization")
	var userPower = new(UserPower)
	store.Get(authorization, userPower)
	if userPower.UserId != 1 {
		logutils.PanicErr(errors.New("无权访问"))
	}
}
