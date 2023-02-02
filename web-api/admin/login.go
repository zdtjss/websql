package admin

import (
	"go-web/utils"
	"go-web/utils/store"
	"net/http"
)

func Login(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	name := r.FormValue("name")
	pwd := r.FormValue("password")

	if name != "" {
		user := findByLoginName(name)
		if user.Pwd == pwd {
			power := findUserPower(user.Id)
			store.StoreItem(string(user.Id), power)
			utils.WriteJson(w, "登录成功")
		}
	} else {
		utils.WriteJson(w, "登录失败")
	}
}
