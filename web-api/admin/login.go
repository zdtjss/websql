package admin

import (
	"crypto/md5"
	"fmt"
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
		if user.Pwd == pwd {
			power := findUserPower(user.Id)
			key := randomKey()
			store.StoreItem(key, power)
			w.Header().Set("Authentication", key)
			utils.WriteJson(w, "登录成功")
		}
	} else {
		utils.WriteJson(w, "登录失败")
	}
}

func randomKey() string {
	h := md5.New()
	h.Write([]byte(fmt.Sprint(utils.RandomInt64())))
	return fmt.Sprint(h.Sum(nil))
}
