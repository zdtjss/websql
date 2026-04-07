package ai

import (
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

// HandleTestConfig tests the AI connection.
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
