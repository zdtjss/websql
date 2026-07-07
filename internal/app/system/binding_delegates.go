package system

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// GetAllSystemConfigByService 返回所有系统配置，含旧 AI 配置迁移逻辑。
// 业务来自 GetAllSystemConfigHandler handler。
func GetAllSystemConfigByService() *SystemConfigAll {
	cfg := &SystemConfigAll{
		OutterUser:      GetSystemConfigValue("system.outterUser"),
		SelectedModelId: GetSystemConfigValue("ai.selectedModelId"),
		RedisAddr:       GetSystemConfigValue("system.redisAddr"),
		RedisPassword:   GetSystemConfigValue("system.redisPassword"),
		DefaultHomepage: GetSystemConfigValue("system.defaultHomepage"),
	}
	redisDBStr := GetSystemConfigValue("system.redisDB")
	if redisDBStr != "" {
		fmt.Sscanf(redisDBStr, "%d", &cfg.RedisDB)
	}

	modelListJSON := GetSystemConfigValue("ai.modelList")
	if modelListJSON != "" && modelListJSON != "[]" {
		var modelList []AIModelItem
		err := json.Unmarshal([]byte(modelListJSON), &modelList)
		if err == nil {
			cfg.AIModelList = modelList
		} else {
			cfg.AIModelList = []AIModelItem{}
		}
	} else {
		// 旧配置迁移
		provider := GetSystemConfigValue("ai.provider")
		baseURL := GetSystemConfigValue("ai.baseUrl")
		model := GetSystemConfigValue("ai.model")
		apiKey := GetSystemConfigValue("ai.apiKey")
		temperatureStr := GetSystemConfigValue("ai.temperature")
		enableThinkingStr := GetSystemConfigValue("ai.enableThinking")

		if provider != "" || baseURL != "" || model != "" {
			temperature := float32(0.7)
			if temperatureStr != "" {
				fmt.Sscanf(temperatureStr, "%f", &temperature)
			}
			enableThinking := enableThinkingStr == "true"

			migratedModel := AIModelItem{
				Id:             "migrated_" + fmt.Sprintf("%d", nowUnix()),
				Provider:       provider,
				BaseURL:        baseURL,
				Model:          model,
				ApiKey:         apiKey,
				Temperature:    temperature,
				EnableThinking: enableThinking,
				IsDefault:      true,
			}
			cfg.AIModelList = []AIModelItem{migratedModel}
			cfg.SelectedModelId = migratedModel.Id

			migrateAIConfigToModelList(cfg.AIModelList, cfg.SelectedModelId)
		} else {
			cfg.AIModelList = []AIModelItem{}
		}
	}

	ipStr := GetSystemConfigValue("system.allowedIP")
	if ipStr != "" {
		var ips []string
		err := json.Unmarshal([]byte(ipStr), &ips)
		if err == nil {
			cfg.AllowedIP = ips
		}
	}

	return cfg
}

// nowUnix 返回当前 Unix 时间戳，service 内独立函数避免直接依赖 time 包。
func nowUnix() int64 {
	return time.Now().Unix()
}

// SaveAllSystemConfigByService 保存所有系统配置。
// 业务来自 SaveAllSystemConfigHandler handler。
func SaveAllSystemConfigByService(cfg *SystemConfigAll) {
	if cfg == nil {
		return
	}
	modelListJSON, _ := json.Marshal(cfg.AIModelList)
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.modelList", ConfigValue: string(modelListJSON), ConfigType: "ai", Remark: "AI 模型配置列表",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.selectedModelId", ConfigValue: cfg.SelectedModelId, ConfigType: "ai", Remark: "当前选中的模型ID",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.outterUser", ConfigValue: cfg.OutterUser, ConfigType: "system", Remark: "外部用户认证接口 URL",
	})
	ipJSON, _ := json.Marshal(cfg.AllowedIP)
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.allowedIP", ConfigValue: string(ipJSON), ConfigType: "system", Remark: "允许的 IP 地址列表",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.redisAddr", ConfigValue: cfg.RedisAddr, ConfigType: "system", Remark: "Redis 地址",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.redisPassword", ConfigValue: cfg.RedisPassword, ConfigType: "system", Remark: "Redis 密码",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.redisDB", ConfigValue: fmt.Sprintf("%d", cfg.RedisDB), ConfigType: "system", Remark: "Redis 数据库编号",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.defaultHomepage", ConfigValue: cfg.DefaultHomepage, ConfigType: "system", Remark: "默认首页",
	})
}

// GetAIModelListByService 返回 AI 模型列表（精简版）。
// 业务来自 GetAIModelListHandler handler。
func GetAIModelListByService() *AIModelListResponse {
	selectedModelId := GetSystemConfigValue("ai.selectedModelId")

	modelListJSON := GetSystemConfigValue("ai.modelList")
	if modelListJSON == "" || modelListJSON == "[]" {
		return &AIModelListResponse{
			AIModelList:     []AIModelBrief{},
			SelectedModelId: selectedModelId,
		}
	}

	var modelList []AIModelItem
	err := json.Unmarshal([]byte(modelListJSON), &modelList)
	if err != nil {
		return &AIModelListResponse{
			AIModelList:     []AIModelBrief{},
			SelectedModelId: selectedModelId,
		}
	}

	briefList := make([]AIModelBrief, 0, len(modelList))
	for _, m := range modelList {
		briefList = append(briefList, AIModelBrief{
			Id:       m.Id,
			Provider: m.Provider,
			Model:    m.Model,
		})
	}

	return &AIModelListResponse{
		AIModelList:     briefList,
		SelectedModelId: selectedModelId,
	}
}

// SaveAIModelByService 新增或更新 AI 模型。
// 业务来自 SaveAIModelHandler handler。
// 返回保存后的模型（含生成的新 ID）。
func SaveAIModelByService(model *AIModelItem) AIModelItem {
	if model == nil {
		return AIModelItem{}
	}
	if model.Id == "" {
		model.Id = "model_" + idgen.RandomStr()
	}

	modelList := getModelListFromDB()
	found := false
	for i, m := range modelList {
		if m.Id == model.Id {
			modelList[i] = *model
			found = true
			break
		}
	}
	if !found {
		modelList = append(modelList, *model)
	}
	saveModelListToDB(modelList)
	return *model
}

// DeleteAIModelByService 删除 AI 模型，删除选中模型时自动选中第一个。
// 业务来自 DeleteAIModelHandler handler。
// 返回 (newList, error)。
func DeleteAIModelByService(id string) ([]AIModelItem, error) {
	if id == "" {
		return nil, errors.New("模型 ID 不能为空")
	}

	modelList := getModelListFromDB()
	newList := make([]AIModelItem, 0, len(modelList))
	for _, m := range modelList {
		if m.Id != id {
			newList = append(newList, m)
		}
	}
	saveModelListToDB(newList)

	selectedId := GetSystemConfigValue("ai.selectedModelId")
	if selectedId == id && len(newList) > 0 {
		SaveSystemConfig(&SystemConfigSave{
			ConfigKey: "ai.selectedModelId", ConfigValue: newList[0].Id, ConfigType: "ai", Remark: "当前选中的模型ID",
		})
		LoadSystemConfigToMemory()
	} else if selectedId == id {
		SaveSystemConfig(&SystemConfigSave{
			ConfigKey: "ai.selectedModelId", ConfigValue: "", ConfigType: "ai", Remark: "当前选中的模型ID",
		})
	}

	return newList, nil
}

// SelectAIModelByService 选中指定模型并 reload 内存配置。
// 业务来自 SelectAIModelHandler handler。
func SelectAIModelByService(id string) error {
	if id == "" {
		return errors.New("模型 ID 不能为空")
	}
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.selectedModelId", ConfigValue: id, ConfigType: "ai", Remark: "当前选中的模型ID",
	})
	LoadSystemConfigToMemory()
	return nil
}

// TestOutterUserByService 测试外部用户认证接口可用性。
// 业务来自 TestOutterUserHandler handler。
// 返回 (message, statusCode, error)。
func TestOutterUserByService() (string, int, error) {
	url := GetOutterUserFromDB()
	if url == "" {
		return "", 400, errors.New("未配置外部用户认证接口")
	}

	resp, err := http.Get(url)
	if err != nil {
		return "", 500, errors.New("接口调用失败，请检查网络和配置")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return "接口调用成功", 200, nil
	}
	return "", resp.StatusCode, errors.New("接口返回异常状态码")
}

// migrateAIConfigToModelList 把旧版 AI 配置迁移到模型列表。
// 提取自 GetAllSystemConfigHandler 内联逻辑，供 service 复用。
func migrateAIConfigToModelList(modelList []AIModelItem, selectedId string) {
	modelListJSON, _ := json.Marshal(modelList)
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.modelList", ConfigValue: string(modelListJSON), ConfigType: "ai", Remark: "AI 模型配置列表",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.selectedModelId", ConfigValue: selectedId, ConfigType: "ai", Remark: "当前选中的模型ID",
	})
	if db := getDB(); db != nil {
		db.Exec("DELETE FROM t_system_config WHERE config_key IN ('ai.provider', 'ai.baseUrl', 'ai.model', 'ai.apiKey', 'ai.temperature', 'ai.enableThinking')")
	}
}

// 下面是 HTTP handler 薄包装层，调用 service 函数。

// getAllSystemConfigHandlerFromService 是 GetAllSystemConfigHandler 的 service 化版本。
func getAllSystemConfigHandlerFromService(c *gin.Context) {
	cfg := GetAllSystemConfigByService()
	response.WriteOK(c, cfg)
}

// saveAllSystemConfigHandlerFromService 是 SaveAllSystemConfigHandler 的 service 化版本。
func saveAllSystemConfigHandlerFromService(c *gin.Context) {
	cfg := &SystemConfigAll{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, cfg); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}
	SaveAllSystemConfigByService(cfg)
	response.WriteOK(c, "")
}

// saveAIModelHandlerFromService 是 SaveAIModelHandler 的 service 化版本。
func saveAIModelHandlerFromService(c *gin.Context) {
	var model AIModelItem
	if err := jsonutil.UnmarshalJson(c.Request.Body, &model); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}
	saved := SaveAIModelByService(&model)
	response.WriteOK(c, saved)
}

// deleteAIModelHandlerFromService 是 DeleteAIModelHandler 的 service 化版本。
func deleteAIModelHandlerFromService(c *gin.Context) {
	var req struct {
		Id string `json:"id"`
	}
	if err := jsonutil.UnmarshalJson(c.Request.Body, &req); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}
	_, err := DeleteAIModelByService(req.Id)
	if err != nil {
		response.WriteErr(c, http.StatusOK, 400, err.Error())
		return
	}
	response.WriteOK(c, "")
}

// selectAIModelHandlerFromService 是 SelectAIModelHandler 的 service 化版本。
func selectAIModelHandlerFromService(c *gin.Context) {
	var req struct {
		Id string `json:"id"`
	}
	if err := jsonutil.UnmarshalJson(c.Request.Body, &req); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}
	if err := SelectAIModelByService(req.Id); err != nil {
		response.WriteErr(c, http.StatusOK, 400, err.Error())
		return
	}
	response.WriteOK(c, "")
}

// getAIModelListHandlerFromService 是 GetAIModelListHandler 的 service 化版本。
func getAIModelListHandlerFromService(c *gin.Context) {
	resp := GetAIModelListByService()
	response.WriteOK(c, resp)
}

// testOutterUserHandlerFromService 是 TestOutterUserHandler 的 service 化版本。
func testOutterUserHandlerFromService(c *gin.Context) {
	msg, code, err := TestOutterUserByService()
	if err != nil {
		response.WriteErr(c, http.StatusOK, code, err.Error())
		return
	}
	response.WriteOK(c, msg)
}
