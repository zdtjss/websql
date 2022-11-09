package sqlite

import (
	"go-web/utils"
	"log"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestReadFromDB(t *testing.T) {
	userList := ReadFromDB()
	log.Println(string(utils.ToJsonString(userList)))
}
