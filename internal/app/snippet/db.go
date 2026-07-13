package snippet

import (
	"websql/internal/pkg/dbaccess"

	"github.com/jmoiron/sqlx"
)

var holder = &dbaccess.Holder{}

func Init(db *sqlx.DB) { holder.Init(db) }

func getDB() *sqlx.DB { return holder.Get() }
