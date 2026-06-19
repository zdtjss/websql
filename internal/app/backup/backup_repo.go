package backup

import (
	"sync"

	"github.com/jmoiron/sqlx"
)

// BackupRepo 定义备份数据访问接口，所有针对 t_backup 的 SQL 查询均在此实现
type BackupRepo interface {
	EnsureBackupTable()
	InsertBackupRecord(record *BackupRecord) error
	InsertBackupRecordShort(record *BackupRecord) error
	FindBackups(connId, schema string) ([]BackupRecord, error)
	FindBackupById(id string) (BackupRecord, error)
	DeleteBackupRecord(id string) error
}

type backupRepo struct {
	db *sqlx.DB
}

// NewBackupRepo 创建 BackupRepo 实例，接受 *sqlx.DB 以便未来依赖注入
func NewBackupRepo(db *sqlx.DB) BackupRepo {
	return &backupRepo{db: db}
}

var migrateOnce sync.Once

func (r *backupRepo) EnsureBackupTable() {
	migrateOnce.Do(func() {
		if r.db == nil {
			return
		}
		var hasNameCol bool
		row := r.db.QueryRow("SELECT COUNT(*) > 0 FROM pragma_table_info('t_backup') WHERE name='name'")
		if err := row.Scan(&hasNameCol); err != nil {
			var colCount int
			row2 := r.db.QueryRow("SELECT COUNT(*) FROM information_schema.columns WHERE table_name='t_backup' AND column_name='name'")
			if err2 := row2.Scan(&colCount); err2 != nil {
				return
			}
			hasNameCol = colCount > 0
		}
		if hasNameCol {
			return
		}
		r.db.Exec("DROP TABLE IF EXISTS t_backup")
		r.db.Exec(`CREATE TABLE t_backup (
			id TEXT PRIMARY KEY,
			name TEXT,
			conn_id TEXT,
			schema_name TEXT,
			db_type TEXT,
			size_bytes INTEGER DEFAULT 0,
			backup_type TEXT DEFAULT 'full',
			encrypted INTEGER DEFAULT 0,
			created_at TEXT,
			description TEXT,
			status TEXT DEFAULT 'completed',
			file_path TEXT
		)`)
	})
}

func (r *backupRepo) InsertBackupRecord(record *BackupRecord) error {
	_, err := r.db.Exec(
		"INSERT INTO t_backup (id, name, conn_id, schema_name, db_type, size_bytes, backup_type, encrypted, created_at, description, status, file_path) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)",
		record.Id, record.Name, record.ConnId, record.Schema, record.DbType, record.Size, record.Type, record.Encrypted, record.CreatedAt, record.Description, record.Status, record.FilePath,
	)
	return err
}

func (r *backupRepo) InsertBackupRecordShort(record *BackupRecord) error {
	_, err := r.db.Exec(
		"INSERT INTO t_backup (id, name, conn_id, schema_name, db_type, size_bytes, backup_type, encrypted, created_at, description) VALUES (?,?,?,?,?,?,?,?,?,?)",
		record.Id, record.Name, record.ConnId, record.Schema, record.DbType, record.Size, record.Type, record.Encrypted, record.CreatedAt, record.Description,
	)
	return err
}

func (r *backupRepo) FindBackups(connId, schema string) ([]BackupRecord, error) {
	var records []BackupRecord
	var err error
	if connId != "" && schema != "" {
		err = r.db.Select(&records, "SELECT id,name,conn_id,schema_name,db_type,size_bytes,backup_type,encrypted,created_at,description,COALESCE(status,'completed') status, COALESCE(file_path,'') file_path FROM t_backup WHERE conn_id=? AND schema_name=? ORDER BY created_at DESC", connId, schema)
	} else if connId != "" {
		err = r.db.Select(&records, "SELECT id,name,conn_id,schema_name,db_type,size_bytes,backup_type,encrypted,created_at,description,COALESCE(status,'completed') status, COALESCE(file_path,'') file_path FROM t_backup WHERE conn_id=? ORDER BY created_at DESC", connId)
	} else {
		err = r.db.Select(&records, "SELECT id,name,conn_id,schema_name,db_type,size_bytes,backup_type,encrypted,created_at,description,COALESCE(status,'completed') status, COALESCE(file_path,'') file_path FROM t_backup ORDER BY created_at DESC")
	}
	return records, err
}

func (r *backupRepo) FindBackupById(id string) (BackupRecord, error) {
	var record BackupRecord
	err := r.db.Get(&record, "SELECT * FROM t_backup WHERE id=?", id)
	return record, err
}

func (r *backupRepo) DeleteBackupRecord(id string) error {
	_, err := r.db.Exec("DELETE FROM t_backup WHERE id=?", id)
	return err
}
