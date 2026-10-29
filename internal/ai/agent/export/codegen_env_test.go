package export

import (
	"strings"
	"testing"
)

// TestBuildCodegenEnvWhitelist 验证子进程不继承父进程敏感环境变量，只注入必要变量。
func TestBuildCodegenEnvWhitelist(t *testing.T) {
	t.Setenv("SECRET_SENTINEL_XYZ", "do-not-leak")
	db := &CodegenDBInfo{
		DBType:   "mysql",
		URL:      "db.example.com:3306/app",
		User:     "reader",
		Password: "p@ssw0rd",
		Schema:   "app",
	}
	env := buildCodegenEnv(t.TempDir(), db)
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "SECRET_SENTINEL_XYZ=") {
		t.Fatal("codegen env must not inherit arbitrary parent secrets")
	}
	if !strings.Contains(joined, "WEBSQL_DB_PASSWORD=p@ssw0rd") {
		t.Fatal("codegen env should inject the explicit DB password")
	}
	if !strings.Contains(joined, "WEBSQL_DB_URL=db.example.com:3306/app") {
		t.Fatal("codegen env should inject the explicit DB URL")
	}
}

// TestContainsCodegenSecrets 验证产物泄密扫描覆盖密码与带凭据的 URL。
func TestContainsCodegenSecrets(t *testing.T) {
	db := &CodegenDBInfo{
		URL:      "mysql://reader:p@ssw0rd@db.example.com/app",
		Password: "p@ssw0rd",
	}
	if !containsCodegenSecrets([]byte("col\np@ssw0rd\n"), db) {
		t.Fatal("password in output must be detected")
	}
	if !containsCodegenSecrets([]byte(db.URL), db) {
		t.Fatal("credential-bearing URL in output must be detected")
	}
	if containsCodegenSecrets([]byte("normal report content"), db) {
		t.Fatal("normal output must not be flagged")
	}
}
