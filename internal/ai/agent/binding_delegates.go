package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	admin "websql/internal/app/admin"
	system "websql/internal/app/system"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/safego"
)

// resolveRequestParamsByAuth 是 resolveRequestParams 的 service 版本，
// 直接接收 authorization 字符串而非 *gin.Context，供桌面 binding 调用。
func resolveRequestParamsByAuth(req *ChatRequest, authorization string) (*requestParams, error) {
	cfg := system.GetSelectedModelConfig(req.ModelId)
	if cfg == nil {
		return nil, errors.New("未配置 AI 服务")
	}

	user := admin.GetUser(authorization)
	if user == nil || user.Id == "" {
		return nil, errors.New("未认证或认证已过期")
	}

	if req.UserID == "" {
		req.UserID = user.Id
	}

	connID := req.ConnID
	if connID == "" && len(req.Schemas) > 0 {
		connID = req.Schemas[0].ConnID
	}
	dbType, dbSchema, dbVersion := GetDBInfo(connID)
	if len(req.Schemas) > 0 && req.Schemas[0].Schema != "" {
		dbSchema = req.Schemas[0].Schema
	} else if req.Schema != "" {
		dbSchema = req.Schema
	}
	schemaNames := collectSchemaNames(connID, dbSchema, req)
	scope := BuildPermissionScope(user.Id, connID, schemaNames)

	return &requestParams{
		cfg:       cfg,
		user:      user,
		connID:    connID,
		dbType:    dbType,
		dbSchema:  dbSchema,
		dbVersion: dbVersion,
		schemas:   req.Schemas,
		scope:     scope,
		auditCtx: &ExecAuditCtx{
			ConnID:    connID,
			UserID:    user.Id,
			UserName:  user.Name,
			SessionID: req.SessionID,
		},
	}, nil
}

// ChatStreamByService 是 ChatStream 的 service 化版本，通过 emit 回调推送 StreamChunk。
// 业务来自 ChatStream handler。
// parentCtx 由调用方控制，用于取消时停止 agent。
// 30 分钟超时，5s 心跳，与 HTTP 模式一致。
// 错误通过 emit 推送 error 事件，不再区分 HTTP 状态码。
func (h *Handler) ChatStreamByService(parentCtx context.Context, req *ChatRequest, authorization string, emit func(StreamChunk)) {
	if len(req.InterruptIDs) > 0 && req.CheckPointID != "" {
		h.resumeStreamByService(parentCtx, req, authorization, emit)
		return
	}

	params, err := resolveRequestParamsByAuth(req, authorization)
	if err != nil {
		emit(StreamChunk{Type: "error", Content: err.Error()})
		emit(StreamChunk{Type: "done"})
		return
	}

	if params.scope.IsRemote && !params.scope.HasAnyAccess() {
		emit(StreamChunk{Type: "error", Content: "你没有此数据库连接的访问权限"})
		emit(StreamChunk{Type: "done"})
		return
	}

	runnerCtx, runnerCancel := context.WithTimeout(parentCtx, 30*time.Minute)
	defer runnerCancel()

	kaStop := make(chan struct{})
	safego.GoWithName("sse-keepalive", func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-kaStop:
				return
			case <-runnerCtx.Done():
				return
			case <-ticker.C:
				emit(StreamChunk{Type: "ping"})
			}
		}
	})

	safego.GoWithName("sse-ctx-watch", func() {
		select {
		case <-parentCtx.Done():
			runnerCancel()
		case <-runnerCtx.Done():
		}
	})

	sess, sessionID := h.prepareSession(req, runnerCancel)

	agent, agentErr := GetAgentFactory().GetOrCreate(runnerCtx, params.cfg, params.connID, params.dbType, params.dbSchema, params.dbVersion, req.Schemas, params.scope, params.auditCtx)
	if agentErr != nil {
		log.Printf("[Handler] 创建 Agent 失败 - err=%v\n", agentErr)
		close(kaStop)
		emit(StreamChunk{Type: "error", Content: "创建 Agent 失败，请稍后重试"})
		emit(StreamChunk{Type: "done"})
		return
	}

	runID := fmt.Sprintf("%s_%d", req.SessionID, time.Now().UnixNano())
	safego.GoWithName("agent-cancel-watch", func() {
		<-parentCtx.Done()
		agent.Cancel(runID)
	})

	_, runErr := agent.RunStream(runnerCtx, runID, *req, emit)
	close(kaStop)
	sess.ClearCancel()

	if runErr != nil {
		log.Printf("[Handler] Agent 执行失败 - err=%+v\n", runErr)
		if !errors.Is(runErr, context.DeadlineExceeded) && !errors.Is(runErr, context.Canceled) {
			emit(StreamChunk{Type: "error", Content: "AI 处理出错，请稍后重试"})
		}
	}

	emit(StreamChunk{Type: "done"})
	_ = sessionID
}

// resumeStreamByService 是 handleResumeExec 的 service 版本。
func (h *Handler) resumeStreamByService(parentCtx context.Context, req *ChatRequest, authorization string, emit func(StreamChunk)) {
	params, err := resolveRequestParamsByAuth(req, authorization)
	if err != nil {
		emit(StreamChunk{Type: "error", Content: err.Error()})
		emit(StreamChunk{Type: "done"})
		return
	}

	runnerCtx, runnerCancel := context.WithTimeout(parentCtx, 30*time.Minute)
	defer runnerCancel()

	kaStop := make(chan struct{})
	safego.GoWithName("sse-keepalive", func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-kaStop:
				return
			case <-runnerCtx.Done():
				return
			case <-ticker.C:
				emit(StreamChunk{Type: "ping"})
			}
		}
	})

	sess, _ := h.prepareSession(req, runnerCancel)

	agent, agentErr := GetAgentFactory().GetOrCreate(runnerCtx, params.cfg, params.connID, params.dbType, params.dbSchema, params.dbVersion, req.Schemas, params.scope, params.auditCtx)
	if agentErr != nil {
		log.Printf("[Handler] 创建 Agent 失败 - err=%v\n", agentErr)
		close(kaStop)
		emit(StreamChunk{Type: "error", Content: "恢复执行失败，请重新操作"})
		emit(StreamChunk{Type: "done"})
		return
	}

	targets := make(map[string]bool, len(req.InterruptIDs))
	for _, id := range req.InterruptIDs {
		targets[id] = req.Confirmed
	}

	if err := agent.ResumeStream(runnerCtx, req.CheckPointID, targets, emit, sess); err != nil {
		log.Printf("[Handler] resume failed - err=%v\n", err)
		emit(StreamChunk{Type: "error", Content: "resume failed: " + err.Error()})
	}
	sess.ClearCancel()
	close(kaStop)
	emit(StreamChunk{Type: "done"})
}

// UploadResult 上传文件的处理结果。
// 桌面 binding 把 fileID 返回给前端，前端在 ChatRequest.ExcelData.FileID 中引用。
type UploadResult struct {
	FileID      string     `json:"fileId"`
	FileName    string     `json:"fileName"`
	FileType    string     `json:"fileType"`
	Columns     []string   `json:"columns,omitempty"`
	TotalRows   int        `json:"totalRows,omitempty"`
	CharCount   int        `json:"charCount,omitempty"`
	TextPreview string     `json:"textPreview,omitempty"`
	Preview     [][]string `json:"preview,omitempty"`
}

// UploadExcelByService 是 HandleUploadExcel 的 service 化版本。
// 业务来自 HandleUploadExcel handler。
// reader 为文件内容流，filename 用于推断文件类型。
// 桌面 binding 通过 Wails runtime 把 *os.File 传入；HTTP handler 通过 multipart.File 传入。
func UploadExcelByService(reader io.Reader, filename string) (*UploadResult, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	ft, ok := classifyUploadExt(ext)
	if !ok {
		return nil, errors.New("仅支持 .xlsx/.xls/.csv/.md/.markdown 格式的文件")
	}

	fileID := idgen.RandomStr()
	diskPath := filepath.Join(uploadDir, fileID+ext)

	dst, err := os.Create(diskPath)
	if err != nil {
		return nil, errors.New("保存文件失败，请重试")
	}
	written, err := io.Copy(dst, reader)
	dst.Close()
	if err != nil {
		os.Remove(diskPath)
		return nil, errors.New("写入文件失败，请重试")
	}

	// Markdown 不做数据量限制；Excel/CSV 限制 20MB
	if ft != fileTypeMarkdown && written > int64(maxUploadSize) {
		os.Remove(diskPath)
		return nil, errors.New("文件大小不能超过 20MB")
	}

	// Markdown：读取全文
	if ft == fileTypeMarkdown {
		raw, err := os.ReadFile(diskPath)
		if err != nil {
			os.Remove(diskPath)
			return nil, errors.New("读取文件失败，请检查文件内容")
		}
		text := string(raw)
		charCount := utf8.RuneCountInString(text)
		uploadCache.Set(fileID, &uploadMeta{
			ID:        fileID,
			FileName:  filename,
			DiskPath:  diskPath,
			Type:      ft,
			CharCount: charCount,
		})
		log.Printf("[Upload] Markdown 已暂存 - id=%s, name=%s, chars=%d\n", fileID, filename, charCount)
		return &UploadResult{
			FileID:      fileID,
			FileName:    filename,
			FileType:    string(ft),
			CharCount:   charCount,
			TextPreview: markdownPreview(text),
		}, nil
	}

	// 表格类：Excel / CSV
	allRows, err := readTabularRows(diskPath, ft)
	if err != nil {
		os.Remove(diskPath)
		return nil, err
	}
	if len(allRows) < 2 {
		os.Remove(diskPath)
		return nil, errors.New("文件至少需要包含表头行和一行数据")
	}

	columns := allRows[0]
	var preview [][]string
	totalRows := 0
	for _, row := range allRows[1:] {
		hasValue := false
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				hasValue = true
				break
			}
		}
		if !hasValue {
			continue
		}
		totalRows++
		if len(preview) < 10 {
			padded := make([]string, len(columns))
			copy(padded, row)
			preview = append(preview, padded)
		}
	}

	uploadCache.Set(fileID, &uploadMeta{
		ID:        fileID,
		FileName:  filename,
		DiskPath:  diskPath,
		Type:      ft,
		Columns:   columns,
		TotalRows: totalRows,
	})

	log.Printf("[Upload] 表格文件已暂存 - id=%s, name=%s, type=%s, columns=%v, rows=%d\n",
		fileID, filename, ft, columns, totalRows)

	return &UploadResult{
		FileID:    fileID,
		FileName:  filename,
		FileType:  string(ft),
		Columns:   columns,
		TotalRows: totalRows,
		Preview:   preview,
	}, nil
}

// PreMatchColumnsRequest HandlePreMatchColumns 的入参集合。
type PreMatchColumnsRequest struct {
	FileID    string `json:"fileId"`
	ConnID    string `json:"connId"`
	TableName string `json:"tableName"`
}

// PreMatchColumnsResult 预匹配列结果。
type PreMatchColumnsResult struct {
	Matches      []PreMatchItem `json:"matches"`
	MatchedCount int            `json:"matchedCount"`
	TotalExcel   int            `json:"totalExcel"`
	TotalDB      int            `json:"totalDB"`
	TableColumns []string       `json:"tableColumns"`
}

// PreMatchItem 单列匹配信息。
type PreMatchItem struct {
	ExcelColumn string `json:"excelColumn"`
	DBColumn    string `json:"dbColumn"`
	Matched     bool   `json:"matched"`
}

// PreMatchColumnsByService 是 HandlePreMatchColumns 的 service 化版本。
// 业务来自 HandlePreMatchColumns handler。
func PreMatchColumnsByService(req *PreMatchColumnsRequest, authorization string) (*PreMatchColumnsResult, error) {
	if req.FileID == "" || req.ConnID == "" || req.TableName == "" {
		return nil, errors.New("fileId、connId、tableName 不能为空")
	}

	meta, ok := uploadCache.Get(req.FileID)
	if !ok {
		return nil, errors.New("上传文件不存在或已过期，请重新上传")
	}

	var preMatchUserId string
	if authorization != "" {
		if user := admin.GetUser(authorization); user != nil {
			preMatchUserId = user.Id
		}
	}
	conn, dbType := GetConn(req.ConnID, preMatchUserId)
	if conn == nil {
		return nil, errors.New("数据库连接不存在")
	}

	_, dbSchema, _ := GetDBInfo(req.ConnID)
	tableColumns, err := getTableColumns(conn, dbType, dbSchema, req.TableName)
	if err != nil || len(tableColumns) == 0 {
		return nil, fmt.Errorf("获取表 %s 的列信息失败", req.TableName)
	}

	mapping, _ := buildFinalMapping(meta.Columns, tableColumns, nil)

	matches := make([]PreMatchItem, 0, len(meta.Columns))
	for i, excelCol := range meta.Columns {
		if dbCol, ok := mapping[i]; ok {
			matches = append(matches, PreMatchItem{ExcelColumn: excelCol, DBColumn: dbCol, Matched: true})
		} else {
			matches = append(matches, PreMatchItem{ExcelColumn: excelCol, DBColumn: "", Matched: false})
		}
	}

	return &PreMatchColumnsResult{
		Matches:      matches,
		MatchedCount: len(mapping),
		TotalExcel:   len(meta.Columns),
		TotalDB:      len(tableColumns),
		TableColumns: tableColumns,
	}, nil
}

// GetSessionsByService 返回当前用户的会话列表（分页）。
// 业务来自 HandleGetSessions handler。
// authorization 用于解析用户 ID；keyword/page/pageSize 为查询参数。
func (h *Handler) GetSessionsByService(authorization, keyword string, page, pageSize int) (*SessionsListResult, error) {
	user := admin.GetUser(authorization)
	if user == nil || user.Id == "" {
		return nil, errors.New("未认证或认证已过期")
	}

	keyword = strings.TrimSpace(keyword)
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset := (page - 1) * pageSize

	total, err := countSessionsByUserID(user.Id, keyword)
	if err != nil {
		return nil, errors.New("获取会话列表失败")
	}

	sessions, err := listSessionsByUserIDPaged(user.Id, keyword, pageSize, offset)
	if err != nil {
		return nil, errors.New("获取会话列表失败")
	}

	metas := make([]SessionMeta, 0, len(sessions))
	for _, sess := range sessions {
		title := sess.Title
		if title == "" {
			title = "未命名会话"
		}
		metas = append(metas, SessionMeta{ID: sess.ID, Title: title, CreatedAt: sess.CreatedAt})
	}

	return &SessionsListResult{
		Sessions: metas,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// SessionsListResult 会话列表返回结构。
type SessionsListResult struct {
	Sessions []SessionMeta `json:"sessions"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
}

// GetSessionByService 返回指定会话的详情。
// 业务来自 HandleGetSession handler。
func (h *Handler) GetSessionByService(sessionID, authorization string) (*SessionDetail, error) {
	if sessionID == "" {
		return nil, errors.New("缺少 sessionId 参数")
	}
	user := admin.GetUser(authorization)
	if user == nil || user.Id == "" {
		return nil, errors.New("未认证或认证已过期")
	}
	return h.sessions.GetDetail(sessionID)
}

// DeleteSessionByService 删除指定会话。
// 业务来自 HandleDeleteSession handler。
func (h *Handler) DeleteSessionByService(sessionID, authorization string) error {
	if sessionID == "" {
		return errors.New("缺少 sessionId 参数")
	}
	user := admin.GetUser(authorization)
	if user == nil || user.Id == "" {
		return errors.New("未认证或认证已过期")
	}
	return h.sessions.Delete(sessionID)
}

// ParseSessionsPageParams 把字符串参数解析为 (page, pageSize)。
// 供桌面 binding 调用。
func ParseSessionsPageParams(pageStr, pageSizeStr string) (int, int) {
	page, _ := strconv.Atoi(pageStr)
	pageSize, _ := strconv.Atoi(pageSizeStr)
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}
