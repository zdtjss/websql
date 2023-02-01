package webapi

import (
	"go-web/utils"
	"net/http"

	"github.com/gorilla/sessions"
)

var sessionStore = sessions.NewCookieStore([]byte("GSESSION"))

func getSession(r *http.Request) *sessions.Session {
	session, err := store.Get(r, "GSESSION")
	utils.Panicln(err)
	return session
}
