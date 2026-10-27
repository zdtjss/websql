package conn

import (
	"io"
	"net"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestParseDBTarget(t *testing.T) {
	cases := []struct {
		name    string
		rawURL  string
		dbType  string
		wantH   string
		wantP   int
		wantErr bool
	}{
		{"mysql tcp 形式", "tcp(10.0.0.1:3306)/demo", "mysql", "10.0.0.1", 3306, false},
		{"mysql 裸地址", "10.0.0.1:3306/demo", "mysql", "10.0.0.1", 3306, false},
		{"oracle 形式", "10.0.0.2:1521/ORCL", "oracle", "10.0.0.2", 1521, false},
		{"带参数的 tcp", "tcp(db.internal:3307)/demo?charset=utf8mb4", "mariadb", "db.internal", 3307, false},
		{"sqlite 不支持隧道", "./nway.sqlite3.db", "sqlite", "", 0, true},
		{"无法解析", "not-a-valid-dsn", "mysql", "", 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, p, err := parseDBTarget(tc.rawURL, tc.dbType)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际成功: %s:%d", h, p)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if h != tc.wantH || p != tc.wantP {
				t.Fatalf("期望 %s:%d，实际 %s:%d", tc.wantH, tc.wantP, h, p)
			}
		})
	}
}

func TestRewriteURLForTunnel(t *testing.T) {
	cases := []struct {
		name    string
		rawURL  string
		dbType  string
		local   string
		want    string
		wantErr bool
	}{
		{"mysql tcp", "tcp(10.0.0.1:3306)/demo?charset=utf8mb4", "mysql", "127.0.0.1:50001",
			"tcp(127.0.0.1:50001)/demo?charset=utf8mb4", false},
		{"mysql 裸地址", "10.0.0.1:3306/demo", "mysql", "127.0.0.1:50002", "127.0.0.1:50002/demo", false},
		{"oracle", "10.0.0.2:1521/ORCL", "oracle", "127.0.0.1:50003", "127.0.0.1:50003/ORCL", false},
		{"无法改写", "", "mysql", "127.0.0.1:50004", "", true},
		{"本地地址为空", "tcp(a:1)/d", "mysql", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rewriteURLForTunnel(tc.rawURL, tc.dbType, tc.local)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际得到 %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("改写失败: %v", err)
			}
			if got != tc.want {
				t.Fatalf("期望 %q，实际 %q", tc.want, got)
			}
		})
	}
}

func TestSSHTunnelConfigValidate(t *testing.T) {
	valid := &SSHTunnelConfig{Host: "bastion", Port: 22, User: "ops", AuthType: SSHAuthPassword, Password: "p"}
	if err := valid.validate(); err != nil {
		t.Fatalf("合法配置校验失败: %v", err)
	}

	bad := []*SSHTunnelConfig{
		{Host: "", User: "u", AuthType: SSHAuthPassword, Password: "p"},
		{Host: "h", User: "", AuthType: SSHAuthPassword, Password: "p"},
		{Host: "h", User: "u", AuthType: SSHAuthPassword, Password: ""},
		{Host: "h", User: "u", AuthType: SSHAuthKey, KeyPath: ""},
		{Host: "h", User: "u", AuthType: "token"},
		{Host: "h", User: "u", Port: 70000, AuthType: SSHAuthPassword, Password: "p"},
	}
	for i, cfg := range bad {
		if err := cfg.validate(); err == nil {
			t.Errorf("第 %d 组非法配置未被拦截: %+v", i, cfg)
		}
	}
}

// fakeSSHClient 用本地 echo 服务替代真实 SSH 通道，验证转发链路。
type fakeSSHClient struct {
	target string
	closed bool
}

func (f *fakeSSHClient) Dial(_, _ string) (net.Conn, error) { return net.Dial("tcp", f.target) }
func (f *fakeSSHClient) SendRequest(string, bool, []byte) (bool, []byte, error) {
	return true, nil, nil
}
func (f *fakeSSHClient) Close() error {
	f.closed = true
	return nil
}

// TestOpenEphemeralTunnel_Forwards 验证「建隧道 → 改写连接串 → 数据双向转发」整条链路。
func TestOpenEphemeralTunnel_Forwards(t *testing.T) {
	// 本地 echo 服务，代替内网数据库
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动 echo 服务失败: %v", err)
	}
	defer echo.Close()
	go func() {
		for {
			c, acceptErr := echo.Accept()
			if acceptErr != nil {
				return
			}
			go func(conn net.Conn) { defer conn.Close(); _, _ = io.Copy(conn, conn) }(c)
		}
	}()

	origDial := dialSSH
	fake := &fakeSSHClient{target: echo.Addr().String()}
	dialSSH = func(*ssh.ClientConfig, string) (sshClient, error) { return fake, nil }
	defer func() { dialSSH = origDial }()

	cfg := &SSHTunnelConfig{Host: "bastion", Port: 22, User: "ops", AuthType: SSHAuthPassword, Password: "p"}
	forwarded, closeFn, err := OpenEphemeralTunnel(cfg, "mysql", "tcp(10.0.0.9:3306)/demo")
	if err != nil {
		t.Fatalf("建隧道失败: %v", err)
	}

	m := mysqlTCPAddrRe.FindStringSubmatch(forwarded)
	if m == nil {
		t.Fatalf("改写后的连接串不符合预期: %s", forwarded)
	}
	if !strings.HasSuffix(m[2], "/demo") {
		t.Fatalf("库名在改写中丢失: %s", forwarded)
	}

	client, err := net.Dial("tcp", m[1])
	if err != nil {
		t.Fatalf("连接本地隧道端口失败: %v", err)
	}
	defer client.Close()

	payload := []byte("ping")
	if _, err := client.Write(payload); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("读取回显失败: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("回显不一致: %q", string(buf))
	}

	closeFn()
	if !fake.closed {
		t.Error("关闭隧道时应关闭 SSH 连接")
	}
}

// TestEnsureTunnel_NoConfig 未配置 SSH 时不应建立隧道（返回空地址）。
func TestEnsureTunnel_NoConfig(t *testing.T) {
	addr, err := EnsureTunnel("not-exist-conn", "mysql", "tcp(127.0.0.1:3306)/demo")
	if err != nil {
		t.Fatalf("未配置 SSH 时不应报错: %v", err)
	}
	if addr != "" {
		t.Fatalf("未配置 SSH 时应返回空地址，实际 %q", addr)
	}
}
