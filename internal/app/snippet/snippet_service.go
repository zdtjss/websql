package snippet

import (
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"websql/internal/database"
	"websql/internal/pkg/idgen"
)

// SnippetService 封装 SQL 收藏夹的业务逻辑：校验、创建者鉴权、导入导出转换。
type SnippetService struct {
	repo SnippetRepo
}

// NewSnippetService 创建 SnippetService 实例。
func NewSnippetService(repo SnippetRepo) *SnippetService {
	return &SnippetService{repo: repo}
}

// 默认实例，保持与 conn 包一致的 lazy init 模式。
// database.Mngtdb 在 InitMngtDbConn() 之后才可用，必须延迟初始化。
var (
	defaultRepo    SnippetRepo
	defaultService *SnippetService
	defaultOnce    sync.Once
)

// ensureDefault 初始化默认 Repo/Service，并确保表已创建。
func ensureDefault() {
	defaultOnce.Do(func() {
		defaultRepo = NewSnippetRepo(database.Mngtdb)
		defaultService = NewSnippetService(defaultRepo)
		if err := defaultRepo.EnsureTable(); err != nil {
			log.Printf("[Snippet] 自动建表失败: %v", err)
		}
	})
}

// 业务错误，Handler 通过 errors.Is 判断。
var (
	ErrSnippetNotFound = errors.New("收藏不存在或无权操作")
	ErrTitleRequired   = errors.New("标题不能为空")
	ErrSqlRequired     = errors.New("SQL 内容不能为空")
)

// List 列表查询。
func (s *SnippetService) List(userId, keyword, category, tag string) ([]*Snippet, error) {
	return s.repo.List(userId, keyword, category, tag)
}

// Save 新增或更新。req.Id 为空表示新增；非空表示更新（仅创建者可改）。
// 返回保存后的完整对象。
func (s *SnippetService) Save(req *SnippetSave, userId string) (*Snippet, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, ErrTitleRequired
	}
	sqlContent := strings.TrimSpace(req.SqlContent)
	if sqlContent == "" {
		return nil, ErrSqlRequired
	}
	now := time.Now().Format("2006-01-02 15:04:05")

	if req.Id != "" {
		// 更新：先校验存在性与归属
		exist, err := s.repo.FindById(req.Id)
		if err != nil || exist == nil || exist.UserId == nil || *exist.UserId != userId {
			return nil, ErrSnippetNotFound
		}
		sn := buildSnippet(req, exist)
		sn.Id = req.Id
		sn.UserId = exist.UserId
		sn.CreatedAt = exist.CreatedAt
		sn.UpdatedAt = &now
		if err := s.repo.Update(sn); err != nil {
			log.Printf("[Snippet] 更新失败: %v", err)
			return nil, err
		}
		return s.repo.FindById(req.Id)
	}

	// 新增
	uid := userId
	sn := buildSnippet(req, nil)
	sn.Id = idgen.RandomStr()
	sn.UserId = &uid
	sn.CreatedAt = &now
	sn.UpdatedAt = &now
	if err := s.repo.Insert(sn); err != nil {
		log.Printf("[Snippet] 新增失败: %v", err)
		return nil, err
	}
	return sn, nil
}

// buildSnippet 根据请求构建 Snippet 对象，复用分类/标签清洗逻辑。
func buildSnippet(req *SnippetSave, exist *Snippet) *Snippet {
	category := strings.TrimSpace(req.Category)
	tags := cleanTags(req.Tags)
	return &Snippet{
		Title:       strings.TrimSpace(req.Title),
		Description: strings.TrimSpace(req.Description),
		SqlContent:  strings.TrimSpace(req.SqlContent),
		Category:    category,
		Tags:        tags,
		DbType:      strings.TrimSpace(req.DbType),
		ConnId:      strings.TrimSpace(req.ConnId),
		SchemaName:  strings.TrimSpace(req.SchemaName),
	}
}

// cleanTags 清洗标签字符串：去除空白、去重、重新以逗号拼接。
func cleanTags(raw string) string {
	tagList := splitTags(raw)
	if len(tagList) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(tagList))
	unique := make([]string, 0, len(tagList))
	for _, t := range tagList {
		if seen[t] {
			continue
		}
		seen[t] = true
		unique = append(unique, t)
	}
	return strings.Join(unique, ",")
}

// Delete 删除收藏（仅创建者可删）。
func (s *SnippetService) Delete(id, userId string) error {
	exist, err := s.repo.FindById(id)
	if err != nil || exist == nil || exist.UserId == nil || *exist.UserId != userId {
		return ErrSnippetNotFound
	}
	return s.repo.Delete(id, userId)
}

// Export 导出当前用户全部收藏为前端可下载的结构。
func (s *SnippetService) Export(userId string) (*SnippetExportData, error) {
	list, err := s.repo.ListByUserId(userId)
	if err != nil {
		return nil, err
	}
	items := make([]SnippetExportItem, 0, len(list))
	for _, sn := range list {
		item := SnippetExportItem{
			Title:       sn.Title,
			Description: sn.Description,
			SqlContent:  sn.SqlContent,
			Category:    sn.Category,
			Tags:        sn.Tags,
			DbType:      sn.DbType,
			ConnId:      sn.ConnId,
			SchemaName:  sn.SchemaName,
		}
		if sn.CreatedAt != nil {
			item.CreatedAt = *sn.CreatedAt
		}
		if sn.UpdatedAt != nil {
			item.UpdatedAt = *sn.UpdatedAt
		}
		items = append(items, item)
	}
	return &SnippetExportData{
		ExportedAt: time.Now().Format("2006-01-02 15:04:05"),
		Count:      len(items),
		Items:      items,
	}, nil
}

// Import 批量导入收藏，跳过 SQL 内容为空的条目，返回成功导入数量。
func (s *SnippetService) Import(items []SnippetImportItem, userId string) (int, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	uid := userId
	count := 0
	for _, it := range items {
		title := strings.TrimSpace(it.Title)
		sqlContent := strings.TrimSpace(it.SqlContent)
		if title == "" || sqlContent == "" {
			continue
		}
		sn := &Snippet{
			Id:          idgen.RandomStr(),
			UserId:      &uid,
			Title:       title,
			Description: strings.TrimSpace(it.Description),
			SqlContent:  sqlContent,
			Category:    strings.TrimSpace(it.Category),
			Tags:        cleanTags(it.Tags),
			DbType:      strings.TrimSpace(it.DbType),
			ConnId:      strings.TrimSpace(it.ConnId),
			SchemaName:  strings.TrimSpace(it.SchemaName),
			CreatedAt:   &now,
			UpdatedAt:   &now,
		}
		if err := s.repo.Insert(sn); err != nil {
			log.Printf("[Snippet] 导入条目失败 title=%s: %v", title, err)
			continue
		}
		count++
	}
	return count, nil
}

// Categories 返回当前用户已有分类及每个分类的条数，用于前端分类树展示。
func (s *SnippetService) Categories(userId string) ([]CategoryStat, error) {
	list, err := s.repo.ListByUserId(userId)
	if err != nil {
		return nil, err
	}
	stat := make(map[string]int)
	for _, sn := range list {
		cat := sn.Category
		if cat == "" {
			cat = UncategorizedLabel
		}
		stat[cat]++
	}
	result := make([]CategoryStat, 0, len(stat))
	for name, cnt := range stat {
		result = append(result, CategoryStat{Name: name, Count: cnt})
	}
	return result, nil
}

// CategoryStat 分类统计项。
type CategoryStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// AllTags 返回当前用户全部标签列表（去重），用于前端标签过滤下拉。
func (s *SnippetService) AllTags(userId string) ([]string, error) {
	list, err := s.repo.ListByUserId(userId)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	tags := make([]string, 0)
	for _, sn := range list {
		for _, t := range splitTags(sn.Tags) {
			if !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
	}
	return tags, nil
}
