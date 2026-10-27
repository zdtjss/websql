package conn

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/crypto"
	"websql/internal/pkg/idgen"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/jmoiron/sqlx"
)

// SSH 隧道。
//
// 目标：内网/跳板机后的数据库也能连。做法是在本机 127.0.0.1 上开一个随机端口，
// 把流量经 SSH 转发到数据库真实地址，再把连接串里的主机端口替换成本地地址。
//
// 配置持久化在管理库 t_system_config（key = conn.ssh.<connId>，JSON），
// 因此无需给 t_conn 加列，SQLite 与 MySQL 管理库都不用迁移；口令用 AES 加密后落库。
//
// 安全约定：
//   - 配置了 knownHostsPath 时严格校验主机密钥；未配置时接受任意主机密钥并打警告（TOFU）。
//   - 隧道建立失败时**不会**回退为直连，避免“以为走了跳板机其实没走”。

const sshConfigKeyPrefix = "conn.ssh."

// SSH 认证方式
const (
	SSHAuthPassword = "password"
	SSHAuthKey      = "key"
)

// SSHTunnelConfig SSH 跳板机配置。
type SSHTunnelConfig struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	User           string `json:"user"`
	AuthType       string `json:"authType"`
	Password       string `json:"password,omitempty"`
	KeyPath        string `json:"keyPath,omitempty"`
	KeyPassphrase  string `json:"keyPassphrase,omitempty"`
	KnownHostsPath string `json:"knownHostsPath,omitempty"`
}

func (c *SSHTunnelConfig) normalize() {
	if c == nil {
		return
	}
	c.AuthType = strings.ToLower(strings.TrimSpace(c.AuthType))
	if c.AuthType == "" {
		c.AuthType = SSHAuthPassword
	}
	if c.Port <= 0 {
		c.Port = 22
	}
	c.Host = strings.TrimSpace(c.Host)
	c.User = strings.TrimSpace(c.User)
}

func (c *SSHTunnelConfig) validate() error {
	if c == nil {
		return fmt.Errorf("SSH 配置为空")
	}
	c.normalize()
	if c.Host == "" {
		return fmt.Errorf("SSH 主机不能为空")
	}
	if c.User == "" {
		return fmt.Errorf("SSH 用户名不能为空")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("SSH 端口非法: %d", c.Port)
	}
	switch c.AuthType {
	case SSHAuthPassword:
		if c.Password == "" {
			return fmt.Errorf("SSH 密码认证需要填写密码")
		}
	case SSHAuthKey:
		if c.KeyPath == "" {
			return fmt.Errorf("SSH 密钥认证需要填写私钥路径")
		}
	default:
		return fmt.Errorf("不支持的 SSH 认证方式: %s", c.AuthType)
	}
	return nil
}

func (c *SSHTunnelConfig) addr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// ===== 连接串地址解析与改写（纯函数，便于单测） =====

var (
	mysqlTCPAddrRe = regexp.MustCompile(`^tcp\(([^)]+)\)(.*)$`)
	hostPortRe     = regexp.MustCompile(`^/*([^/?:]+):(\d+)(.*)$`)
)

// parseDBTarget 从连接串中解析数据库真实可达的 host:port。
//
//	mysql/mariadb: tcp(10.0.0.1:3306)/db 或 10.0.0.1:3306/db
//	oracle:        10.0.0.2:1521/ORCL
func parseDBTarget(rawURL, dbType string) (host string, port int, err error) {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return "", 0, fmt.Errorf("连接串为空")
	}
	if dbType == "sqlite" || dbType == "sqlite3" {
		return "", 0, fmt.Errorf("SQLite 为本地文件数据库，不需要 SSH 隧道")
	}

	addrPart := raw
	if m := mysqlTCPAddrRe.FindStringSubmatch(raw); m != nil {
		addrPart = m[1]
	}
	if h, p, splitErr := net.SplitHostPort(addrPart); splitErr == nil && isAllDigits(p) {
		portNum, convErr := strconv.Atoi(p)
		if convErr != nil {
			return "", 0, fmt.Errorf("端口非法: %s", p)
		}
		return h, portNum, nil
	}
	if m := hostPortRe.FindStringSubmatch(raw); m != nil {
		portNum, convErr := strconv.Atoi(m[2])
		if convErr != nil {
			return "", 0, fmt.Errorf("端口非法: %s", m[2])
		}
		return m[1], portNum, nil
	}
	return "", 0, fmt.Errorf("无法从连接串解析主机端口（示例 tcp(10.0.0.1:3306)/db）: %s", raw)
}

// isAllDigits 判断端口字符串是否全为数字（net.SplitHostPort 不校验数字）。
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// rewriteURLForTunnel 把连接串中的主机端口替换为本地隧道地址。
func rewriteURLForTunnel(rawURL, dbType, localAddr string) (string, error) {
	raw := strings.TrimSpace(rawURL)
	if localAddr == "" {
		return "", fmt.Errorf("本地隧道地址为空")
	}
	if m := mysqlTCPAddrRe.FindStringSubmatch(raw); m != nil {
		return "tcp(" + localAddr + ")" + m[2], nil
	}
	if m := hostPortRe.FindStringSubmatch(raw); m != nil {
		return localAddr + m[3], nil
	}
	return "", fmt.Errorf("无法改写连接串为隧道地址: %s", raw)
}

// ===== 配置持久化 =====

var sshConfigCache = struct {
	sync.RWMutex
	m map[string]*SSHTunnelConfig
}{m: make(map[string]*SSHTunnelConfig)}

func sshConfigKey(connID string) string { return sshConfigKeyPrefix + connID }

// LoadSSHConfig 读取连接的 SSH 配置（含明文口令，仅供建链使用，不得写日志）。
// 结果带进程内缓存，保存/删除时失效。
func LoadSSHConfig(connID string) *SSHTunnelConfig {
	if connID == "" {
		return nil
	}
	sshConfigCache.RLock()
	cached, ok := sshConfigCache.m[connID]
	sshConfigCache.RUnlock()
	if ok {
		return cached
	}

	db := getDBOrNil()
	if db == nil {
		return nil
	}
	var raw string
	if err := db.Get(&raw, "select config_value from t_system_config where config_key = ?", sshConfigKey(connID)); err != nil {
		sshConfigCache.Lock()
		sshConfigCache.m[connID] = nil // 缓存“未配置”，避免每次请求都查库
		sshConfigCache.Unlock()
		return nil
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	cfg := &SSHTunnelConfig{}
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		logger.PrintErrf("解析 SSH 配置失败: %s", err, connID)
		return nil
	}
	// 口令加密存储：解密失败按未配置处理，避免用错误口令反复重试
	if cfg.Password != "" {
		if plain, decErr := crypto.AESDecode(cfg.Password); decErr == nil {
			cfg.Password = plain
		} else {
			logger.PrintErrf("SSH 口令解密失败: %s", decErr, connID)
			cfg.Password = ""
		}
	}
	if cfg.KeyPassphrase != "" {
		if plain, decErr := crypto.AESDecode(cfg.KeyPassphrase); decErr == nil {
			cfg.KeyPassphrase = plain
		} else {
			cfg.KeyPassphrase = ""
		}
	}

	sshConfigCache.Lock()
	sshConfigCache.m[connID] = cfg
	sshConfigCache.Unlock()
	return cfg
}

// SaveSSHConfig 保存 SSH 配置（口令加密）。传 nil 表示删除。
func SaveSSHConfig(connID string, cfg *SSHTunnelConfig) error {
	if connID == "" {
		return fmt.Errorf("连接 ID 为空")
	}
	db := getDB()
	if db == nil {
		return fmt.Errorf("管理库未初始化")
	}

	invalidateSSHConfig(connID)
	CloseTunnel(connID)

	if cfg == nil {
		return deleteSSHConfigRow(db, connID)
	}
	// 编辑连接时前端不返回已有口令：密码认证且未填口令时沿用旧值
	if cfg.AuthType == SSHAuthPassword && strings.TrimSpace(cfg.Password) == "" {
		if old := LoadSSHConfig(connID); old != nil && old.Password != "" {
			cfg.Password = old.Password
		}
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	toStore := *cfg
	if toStore.Password != "" {
		enc, err := crypto.AESEncode(toStore.Password)
		if err != nil {
			return fmt.Errorf("SSH 口令加密失败: %w", err)
		}
		toStore.Password = enc
	}
	if toStore.KeyPassphrase != "" {
		if enc, err := crypto.AESEncode(toStore.KeyPassphrase); err == nil {
			toStore.KeyPassphrase = enc
		}
	}
	payload, err := json.Marshal(toStore)
	if err != nil {
		return err
	}

	if err := deleteSSHConfigRow(db, connID); err != nil {
		return err
	}
	return database.RetryOnBusy(func() error {
		_, execErr := db.Exec(
			"insert into t_system_config (id, config_key, config_value, config_type, remark) values (?, ?, ?, ?, ?)",
			idgen.RandomStr(), sshConfigKey(connID), string(payload), "conn", "连接 SSH 隧道配置",
		)
		return execErr
	}, 3, 50*time.Millisecond)
}

// DeleteSSHConfig 删除连接时清理 SSH 配置。
func DeleteSSHConfig(connID string) {
	db := getDBOrNil()
	if db == nil || connID == "" {
		return
	}
	invalidateSSHConfig(connID)
	CloseTunnel(connID)
	if err := deleteSSHConfigRow(db, connID); err != nil {
		logger.PrintErrf("清理 SSH 配置失败: %s", err, connID)
	}
}

func deleteSSHConfigRow(db *sqlx.DB, connID string) error {
	return database.RetryOnBusy(func() error {
		_, err := db.Exec("delete from t_system_config where config_key = ?", sshConfigKey(connID))
		return err
	}, 3, 50*time.Millisecond)
}

func invalidateSSHConfig(connID string) {
	sshConfigCache.Lock()
	delete(sshConfigCache.m, connID)
	sshConfigCache.Unlock()
}

// ===== 隧道进程管理 =====

// sshDialer 抽象 SSH 建链，便于单测替换为假实现。
type sshClient interface {
	Dial(network, addr string) (net.Conn, error)
	SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error)
	Close() error
}

var dialSSH = func(cfg *ssh.ClientConfig, addr string) (sshClient, error) {
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, err
	}
	return client, nil
}

type tunnelEntry struct {
	listener net.Listener
	client   sshClient
	local    string
	target   string
}

var tunnelMgr = struct {
	sync.Mutex
	m map[string]*tunnelEntry
}{m: make(map[string]*tunnelEntry)}

// buildSSHClientConfig 组装 SSH 客户端配置（认证方式 + 主机密钥校验策略）。
func buildSSHClientConfig(cfg *SSHTunnelConfig) (*ssh.ClientConfig, error) {
	authMethods := make([]ssh.AuthMethod, 0, 2)
	switch cfg.AuthType {
	case SSHAuthPassword:
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	case SSHAuthKey:
		keyBytes, err := os.ReadFile(cfg.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("读取 SSH 私钥失败: %w", err)
		}
		var signer ssh.Signer
		if cfg.KeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(cfg.KeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(keyBytes)
		}
		if err != nil {
			return nil, fmt.Errorf("解析 SSH 私钥失败: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	var hostKeyCallback ssh.HostKeyCallback
	if cfg.KnownHostsPath != "" {
		cb, err := knownhosts.New(cfg.KnownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("加载 known_hosts 失败: %w", err)
		}
		hostKeyCallback = cb
	} else {
		// 未配置 known_hosts 时接受任意主机密钥：与常见 GUI 工具一致，
		// 但会记录警告，便于管理员补配置。
		log.Printf("[SSH] 未配置 knownHostsPath，跳过主机密钥校验（%s）", cfg.addr())
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	return &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         15 * time.Second,
	}, nil
}

// EnsureTunnel 确保连接对应的隧道已建立，返回本地监听地址（形如 127.0.0.1:52341）。
// 未配置 SSH 时返回空字符串；建链失败返回错误（调用方不得回退为直连）。
func EnsureTunnel(connID, dbType, rawURL string) (string, error) {
	cfg := LoadSSHConfig(connID)
	if cfg == nil {
		return "", nil
	}

	host, port, err := parseDBTarget(rawURL, dbType)
	if err != nil {
		return "", err
	}
	target := net.JoinHostPort(host, strconv.Itoa(port))

	tunnelMgr.Lock()
	entry, ok := tunnelMgr.m[connID]
	tunnelMgr.Unlock()
	if ok && entry.target == target && tunnelAlive(entry) {
		return entry.local, nil
	}
	if ok {
		closeEntry(connID, entry)
	}

	clientCfg, err := buildSSHClientConfig(cfg)
	if err != nil {
		return "", err
	}
	client, err := dialSSH(clientCfg, cfg.addr())
	if err != nil {
		return "", fmt.Errorf("SSH 建链失败（%s）: %w", cfg.addr(), err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = client.Close()
		return "", fmt.Errorf("本地隧道端口监听失败: %w", err)
	}

	newEntry := &tunnelEntry{listener: listener, client: client, local: listener.Addr().String(), target: target}
	go serveTunnel(connID, newEntry)

	tunnelMgr.Lock()
	tunnelMgr.m[connID] = newEntry
	tunnelMgr.Unlock()

	log.Printf("[SSH] 隧道已建立 connId=%s, %s -> %s", connID, newEntry.local, target)
	return newEntry.local, nil
}

// tunnelAlive 用 keepalive 探测 SSH 连接是否仍然可用。
func tunnelAlive(entry *tunnelEntry) bool {
	if entry == nil || entry.client == nil {
		return false
	}
	_, _, err := entry.client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}

// serveTunnel 接受本地连接并转发到目标数据库地址。
func serveTunnel(connID string, entry *tunnelEntry) {
	for {
		local, err := entry.listener.Accept()
		if err != nil {
			return // 监听器已关闭
		}
		go func(local net.Conn) {
			remote, dialErr := entry.client.Dial("tcp", entry.target)
			if dialErr != nil {
				logger.PrintErrf("SSH 隧道转发失败: %s", dialErr, connID)
				_ = local.Close()
				return
			}
			pipeBoth(local, remote)
		}(local)
	}
}

// pipeBoth 双向复制数据，任一方向结束后关闭两侧。
func pipeBoth(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		buf := make([]byte, 32*1024)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}

func closeEntry(connID string, entry *tunnelEntry) {
	if entry == nil {
		return
	}
	if entry.listener != nil {
		_ = entry.listener.Close()
	}
	if entry.client != nil {
		_ = entry.client.Close()
	}
	tunnelMgr.Lock()
	if current, ok := tunnelMgr.m[connID]; ok && current == entry {
		delete(tunnelMgr.m, connID)
	}
	tunnelMgr.Unlock()
}

// CloseTunnel 关闭指定连接的隧道（配置变更/连接删除时调用）。
func CloseTunnel(connID string) {
	tunnelMgr.Lock()
	entry, ok := tunnelMgr.m[connID]
	tunnelMgr.Unlock()
	if ok {
		closeEntry(connID, entry)
	}
}

// CloseAllTunnels 关闭全部隧道（服务退出时调用）。
func CloseAllTunnels() {
	tunnelMgr.Lock()
	entries := make(map[string]*tunnelEntry, len(tunnelMgr.m))
	for k, v := range tunnelMgr.m {
		entries[k] = v
	}
	tunnelMgr.Unlock()
	for k, v := range entries {
		closeEntry(k, v)
	}
}

// OpenEphemeralTunnel 用给定配置临时建一条隧道，返回改写后的连接串与关闭函数。
// 用于「测试连接」时连接尚未保存、没有 connId 的场景。
func OpenEphemeralTunnel(cfg *SSHTunnelConfig, dbType, rawURL string) (string, func(), error) {
	if err := cfg.validate(); err != nil {
		return "", nil, err
	}
	host, port, err := parseDBTarget(rawURL, dbType)
	if err != nil {
		return "", nil, err
	}
	target := net.JoinHostPort(host, strconv.Itoa(port))

	clientCfg, err := buildSSHClientConfig(cfg)
	if err != nil {
		return "", nil, err
	}
	client, err := dialSSH(clientCfg, cfg.addr())
	if err != nil {
		return "", nil, fmt.Errorf("SSH 建链失败（%s）: %w", cfg.addr(), err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = client.Close()
		return "", nil, fmt.Errorf("本地隧道端口监听失败: %w", err)
	}

	entry := &tunnelEntry{listener: listener, client: client, local: listener.Addr().String(), target: target}
	go serveTunnel("ephemeral", entry)

	forwarded, err := rewriteURLForTunnel(rawURL, dbType, entry.local)
	if err != nil {
		_ = listener.Close()
		_ = client.Close()
		return "", nil, err
	}
	return forwarded, func() {
		_ = listener.Close()
		_ = client.Close()
	}, nil
}

// ResolveConnURL 在建立数据库连接前解析真实连接串：
// 配置了 SSH 隧道时返回本地转发地址，否则原样返回。
func ResolveConnURL(connID, dbType, rawURL string) (string, error) {
	local, err := EnsureTunnel(connID, dbType, rawURL)
	if err != nil {
		return "", err
	}
	if local == "" {
		return rawURL, nil
	}
	return rewriteURLForTunnel(rawURL, dbType, local)
}
