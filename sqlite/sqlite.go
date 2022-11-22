package sqlite

import (
	"database/sql"
	"log"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func ReadFromDB() []*User {

	//打开数据库，如果不存在，则创建
	db, err := sql.Open("sqlite3", "./nway.sqlite3.db")
	checkErr(err)

	//创建表
	sql_table := `
    CREATE TABLE IF NOT EXISTS userinfo(
        uid INTEGER PRIMARY KEY AUTOINCREMENT,
        username VARCHAR(64) NULL,
        departname VARCHAR(64) NULL,
        created DATE NULL
    );
    `

	db.Exec(sql_table)

	// insert
	stmt, err := db.Prepare("INSERT INTO userinfo(username, departname, created) values(?,?,?)")
	checkErr(err)

	res, err := stmt.Exec("wangshubo", "国务院", "2017-04-21")
	checkErr(err)

	id, err := res.LastInsertId()
	checkErr(err)

	log.Println(id)

	// update
	stmt, err = db.Prepare("update userinfo set username=? where uid=?")
	checkErr(err)

	res, err = stmt.Exec("wangshubo_new"+strconv.FormatInt(time.Now().UnixNano(), 10), id)
	checkErr(err)

	affect, err := res.RowsAffected()
	checkErr(err)

	log.Println(affect)

	// query
	rows, err := db.Query("SELECT * FROM userinfo")
	checkErr(err)

	userList := []*User{}
	for rows.Next() {
		user := &User{}
		err = rows.Scan(&user.Uid, &user.Username, &user.Uepartment, &user.Created)
		checkErr(err)
		// user := &User{uid, username, department, created}
		userList = append(userList, user)
	}

	defer rows.Close()
	defer db.Close()

	return userList
}

type User struct {
	Uid        int       `json:"uid"`
	Username   string    `json:"username"`
	Uepartment string    `json:"uepartment"`
	Created    time.Time `json:"created"`
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}
