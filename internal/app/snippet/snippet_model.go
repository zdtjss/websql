package snippet

// UncategorizedSentinel 是"未分类"在前端与后端之间传递的哨兵值。
// 前端选中"未分类"时以此值作为 category 查询参数，
// 后端识别后转换为 (category IS NULL OR category = '') 条件。
const UncategorizedSentinel = "__uncategorized__"

// UncategorizedLabel 是展示给用户的"未分类"名称，用于分类统计返回。
const UncategorizedLabel = "未分类"

// Snippet 对应管理库表 t_sql_snippet，存储用户收藏的 SQL 片段。
// tags 在数据库中以逗号分隔字符串存储，前端按逗号拆分用于展示与过滤。
type Snippet struct {
	Id          string  `json:"id" db:"id"`
	UserId      *string `json:"userId,omitempty" db:"user_id"`
	Title       string  `json:"title" db:"title"`
	Description string  `json:"description" db:"description"`
	SqlContent  string  `json:"sqlContent" db:"sql_content"`
	Category    string  `json:"category" db:"category"`
	Tags        string  `json:"tags" db:"tags"`
	DbType      string  `json:"dbType" db:"db_type"`
	ConnId      string  `json:"connId" db:"conn_id"`
	SchemaName  string  `json:"schemaName" db:"schema_name"`
	CreatedAt   *string `json:"createdAt,omitempty" db:"created_at"`
	UpdatedAt   *string `json:"updatedAt,omitempty" db:"updated_at"`
}

// SnippetSave 新增/更新收藏时前端提交的结构。
// Id 为空表示新增，非空表示更新（仅创建者可更新）。
// Tags 为前端传入的逗号分隔字符串。
type SnippetSave struct {
	Id          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	SqlContent  string `json:"sqlContent"`
	Category    string `json:"category"`
	Tags        string `json:"tags"`
	DbType      string `json:"dbType"`
	ConnId      string `json:"connId"`
	SchemaName  string `json:"schemaName"`
}

// SnippetImportItem 导入文件中的单条数据结构，与导出格式保持一致。
type SnippetImportItem struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	SqlContent  string `json:"sqlContent"`
	Category    string `json:"category"`
	Tags        string `json:"tags"`
	DbType      string `json:"dbType"`
	ConnId      string `json:"connId"`
	SchemaName  string `json:"schemaName"`
}

// SnippetImportReq 导入请求体。
type SnippetImportReq struct {
	Items []SnippetImportItem `json:"items"`
}

// SnippetExportData 导出文件根结构，包含元信息便于追溯。
type SnippetExportData struct {
	ExportedAt string              `json:"exportedAt"`
	Count      int                 `json:"count"`
	Items      []SnippetExportItem `json:"items"`
}

// SnippetExportItem 导出单条数据，去除用户 id 等私有字段。
type SnippetExportItem struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	SqlContent  string `json:"sqlContent"`
	Category    string `json:"category"`
	Tags        string `json:"tags"`
	DbType      string `json:"dbType"`
	ConnId      string `json:"connId"`
	SchemaName  string `json:"schemaName"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}
