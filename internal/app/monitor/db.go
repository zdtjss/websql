package monitor

import (
	"time"

	"websql/internal/database"
	"websql/internal/pkg/dbaccess"

	"github.com/jmoiron/sqlx"
)

var holder = &dbaccess.Holder{}

func Init(db *sqlx.DB) { holder.Init(db) }

func getDB() *sqlx.DB { return holder.Get() }

func execWithRetry(query string, args ...any) error {
	return database.RetryOnBusy(func() error {
		_, err := getDB().Exec(query, args...)
		return err
	}, 3, 50*time.Millisecond)
}
