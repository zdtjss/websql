package ai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go-web/config"
	"go-web/utils"
	admin "go-web/web-api/admin"

	"github.com/gin-gonic/gin"
)

// HandleSaveConfig saves the AI configuration.
func HandleSaveConfig(c *gin.Context) {
	var cfg admin.AIConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败：" + err.Error()})
		return
	}
	if err := SaveAIConfig(cfg); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "保存配置失败：" + err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 200, "data": "保存成功"})
}

// HandleGetConfig returns the AI configuration with the apiKey masked.
func HandleGetConfig(c *gin.Context) {
	cfg, err := GetAIConfig()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "获取配置失败：" + err.Error()})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 200, "data": nil})
		return
	}
	c.JSON(200, gin.H{"code": 200, "data": cfg})
}

// HandleTestConfig tests the AI connection by sending a simple ping message.
func HandleTestConfig(c *gin.Context) {
	cfg, err := GetAIConfigRaw()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "获取配置失败：" + err.Error()})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 500, "msg": "请先配置 AI 服务"})
		return
	}

	_, err = CallAI(cfg, []ChatMessage{{Role: "user", Content: "hi"}})
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "连接失败：" + err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 200, "msg": "连接成功"})
}

// HandleGenerateSql generates SQL based on user question and table context.
func HandleGenerateSql(c *gin.Context) {
	var req GenerateSqlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败：" + err.Error()})
		return
	}

	cfg, err := GetAIConfigRaw()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "获取 AI 配置失败：" + err.Error()})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 500, "msg": "请先配置 AI 服务"})
		return
	}

	tableSchema := ""
	if req.ConnId != "" {
		tableSchema, _ = fetchTableSchemas(req.ConnId, req.TableContext)
	}

	prompt := BuildGenerateSqlPrompt(req, tableSchema)
	sql, err := CallAI(cfg, []ChatMessage{{Role: "user", Content: prompt}})
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "AI 生成失败：" + err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 200, "data": strings.TrimSpace(sql)})
}

// HandleGenerateSqlStream streams SQL generation.
func HandleGenerateSqlStream(c *gin.Context) {
	var req GenerateSqlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败：" + err.Error()})
		return
	}

	cfg, err := GetAIConfigRaw()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "获取 AI 配置失败：" + err.Error()})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 500, "msg": "请先配置 AI 服务"})
		return
	}

	tableSchema := ""
	if req.ConnId != "" {
		tableSchema, _ = fetchTableSchemas(req.ConnId, req.TableContext)
	}

	prompt := BuildGenerateSqlPrompt(req, tableSchema)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(200, gin.H{"code": 500, "msg": "不支持流式响应"})
		return
	}

	err = StreamAI(cfg, []ChatMessage{{Role: "user", Content: prompt}}, func(chunk StreamChunk) {
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	})

	if err != nil {
		data, _ := json.Marshal(StreamChunk{Type: "error", Content: err.Error()})
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
	}
}

// HandleChat handles chat requests.
func HandleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败：" + err.Error()})
		return
	}

	cfg, err := GetAIConfigRaw()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "获取 AI 配置失败：" + err.Error()})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 500, "msg": "请先配置 AI 服务"})
		return
	}

	prompt := BuildChatPrompt(req)
	response, err := CallAI(cfg, []ChatMessage{{Role: "user", Content: prompt}})
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "AI 响应失败：" + err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 200, "data": response})
}

func fetchTableSchemas(connId string, tables []string) (string, error) {
	cfgList := []admin.ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", connId)
	if err != nil {
		return "", err
	}
	if len(cfgList) == 0 {
		return "", fmt.Errorf("连接不存在：%s", connId)
	}

	cfg := &cfgList[0]

	// 处理可能为 nil 的字段
	name := ""
	if cfg.Name != nil {
		name = *cfg.Name
	}
	user := ""
	if cfg.User != nil {
		user = *cfg.User
	}
	pwd := ""
	if cfg.Pwd != nil {
		pwd = utils.AESDecode(*cfg.Pwd)
	}
	url := ""
	if cfg.Url != nil {
		url = *cfg.Url
	}

	conn := config.GetConn(&config.DBParam{Id: cfg.Id, Name: name, DbType: cfg.DbType, User: user, Pwd: pwd, Url: url})

	var sb strings.Builder
	for _, table := range tables {
		rows, err := conn.Query(fmt.Sprintf("SHOW CREATE TABLE `%s`", table))
		if err != nil {
			continue
		}
		for rows.Next() {
			var tableName, createTable string
			if err := rows.Scan(&tableName, &createTable); err == nil {
				sb.WriteString(createTable)
				sb.WriteString(";\n\n")
			}
		}
		rows.Close()
	}
	return sb.String(), nil
}
