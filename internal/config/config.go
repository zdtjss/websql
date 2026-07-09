package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	logutils "websql/internal/logger"
)

var (
	// Deprecated: 使用 Get()，将在阶段 4 移除
	Cfg *Config

	// activeCfg 由 Container 通过 SetActive 注入，为 nil 时 Get() 回退到 Cfg。
	activeCfg *Config
)

// SetActive 由 Container 在启动阶段调用，设置全局活跃配置。
func SetActive(cfg *Config) { activeCfg = cfg }

// Get 返回活跃配置。优先返回 Container 注入的实例，未注入时回退到 Cfg（迁移期兼容）。
func Get() *Config {
	if activeCfg != nil {
		return activeCfg
	}
	return Cfg
}

func ReadConfig() *Config {
	configFile := FindFile("config.json")
	log.Printf("使用配置文件 %s", configFile)
	fileData, err := os.ReadFile(configFile)
	logutils.PanicErr(err)
	var config Config
	err = json.Unmarshal(fileData, &config)
	logutils.PanicErr(err)
	return &config
}

// ParseFromBytes 从字节切片解析配置，供桌面版从 //go:embed 内嵌的配置加载。
func ParseFromBytes(data []byte) (*Config, error) {
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// TryReadConfig 与 ReadConfig 等价但找不到配置文件时返回 error 而非 panic。
// 用于桌面 dev 模式等可执行文件与 config.json 不在同一目录层级的场景。
func TryReadConfig() (*Config, error) {
	configFile, err := TryFindFile("config.json")
	if err != nil {
		return nil, err
	}
	log.Printf("使用配置文件 %s", configFile)
	fileData, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	var config Config
	if err := json.Unmarshal(fileData, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// TryFindFile 与 FindFile 等价但查找失败返回 error 而非 panic。
// 相比 FindFile 额外查找可执行文件目录下的 bin/ 子目录与当前工作目录，
// 以兼容桌面 dev 模式（二进制位于 cmd/desktop/，而 config.json 位于 cmd/desktop/bin/）。
func TryFindFile(fileName string) (string, error) {
	exec, err := os.Executable()
	if err != nil {
		return "", err
	}
	execDir := filepath.Dir(exec)
	candidates := []string{
		filepath.Join(execDir, "..", fileName), // 生产模式：exe 在 bin/，config 在父目录
		filepath.Join(execDir, fileName),       // exe 同级
		filepath.Join(execDir, "bin", fileName), // dev 模式：exe 在 cmd/desktop/，config 在 bin/
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, fileName)) // 工作目录
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("配置文件 %s 未找到（已查找: %v）", fileName, candidates)
}

func ReadSql(fileName string) string {
	configFile := FindFile(fileName)
	fileData, err := os.ReadFile(configFile)
	logutils.PanicErr(err)
	return string(fileData)
}

func FindFile(fileName string) string {
	exec, err := os.Executable()
	logutils.PanicErr(err)
	configFile := filepath.Join(filepath.Dir(exec), "..", fileName)
	_, err = os.Stat(configFile)
	if err != nil {
		configFile = filepath.Join(filepath.Dir(exec), fileName)
		_, err = os.Stat(configFile)
		logutils.PanicErr(err)
	}
	return configFile
}

type Config struct {
	// true：远程模式，有严格的权限管理；false 本地模式，没有权限管?
	IsRemote bool `json:"isRemote"`
	// true：桌面模式（Wails），由桌面入口设置
	IsDesktop bool `json:"-"`
	DB        struct {
		DriverName     string `json:"type"`
		DataSourceName string `json:"dsn"`
		MaxOpenConns   int    `json:"maxOpenConns"`
		MaxIdleConns   int    `json:"maxIdleConns"`
		ConnMaxLifeMin int    `json:"connMaxLifeMin"`
	} `json:"db"`
	Redis struct {
		Addr     string `json:"addr"`
		Password string `json:"password"`
		DB       int    `json:"db"`
	} `json:"redis"`
	Https struct {
		Enable       bool   `json:"enable"`
		Organization string `json:"organization"`
		CommonName   string `json:"commonName"`
	} `json:"https"`
	OutterUser string   `json:"outterUser"`
	AllowedIP  []string `json:"allowedIP"`
	Security   struct {
		// AESKey 为 16/24/32 字节密钥的 base64 编码，用于加解密连接密码与备份内容。
		// 留空时回退到内置默认密钥（仅用于兼容存量数据，不安全，生产环境务必配置）。
		AESKey string `json:"aesKey"`
	} `json:"security"`
	AI struct {
		Provider       string  `json:"provider"`
		BaseURL        string  `json:"baseUrl"`
		Model          string  `json:"model"`
		ApiKey         string  `json:"apiKey"`
		Temperature    float32 `json:"temperature"`
		EnableThinking bool    `json:"enableThinking"`
	} `json:"ai"`
}
