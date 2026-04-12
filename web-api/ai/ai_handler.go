package ai

import (
	admin "go-web/web-api/admin"
	"log"

	"github.com/gin-gonic/gin"
)

func HandleSaveConfig(c *gin.Context) {
	var cfg admin.AIConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		log.Printf("[AI] 参数解析失败 - err=%v\n", err)
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败"})
		return
	}
	if err := SaveAIConfig(cfg); err != nil {
		log.Printf("[AI] 保存配置失败 - err=%v\n", err)
		c.JSON(200, gin.H{"code": 500, "msg": "保存配置失败"})
		return
	}
	c.JSON(200, gin.H{"code": 200, "data": "保存成功"})
}

func HandleGetConfig(c *gin.Context) {
	cfg, err := GetAIConfig()
	if err != nil {
		log.Printf("[AI] 获取配置失败 - err=%v\n", err)
		c.JSON(200, gin.H{"code": 500, "msg": "获取配置失败"})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 200, "data": nil})
		return
	}
	c.JSON(200, gin.H{"code": 200, "data": cfg})
}

func HandleTestConfig(c *gin.Context) {
	cfg, err := GetAIConfigRaw()
	if err != nil {
		log.Printf("[AI] 获取原始配置失败 - err=%v\n", err)
		c.JSON(200, gin.H{"code": 500, "msg": "获取配置失败"})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 500, "msg": "请先配置 AI 服务"})
		return
	}

	_, err = CallAI(cfg, []ChatMessage{{Role: "user", Content: "hi"}})
	if err != nil {
		log.Printf("[AI] 连接测试失败 - err=%v\n", err)
		c.JSON(200, gin.H{"code": 500, "msg": "AI 服务连接失败，请检查配置和网络"})
		return
	}
	c.JSON(200, gin.H{"code": 200, "msg": "连接成功"})
}
