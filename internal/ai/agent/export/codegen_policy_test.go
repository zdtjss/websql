package export

import "testing"

func TestValidateCodegenSource_AllowsReadOnlyCode(t *testing.T) {
	code := `import websql_db

rows = websql_db.query("SELECT region, SUM(amount) AS total FROM orders GROUP BY region")
with open("report.csv", "w", encoding="utf-8") as f:
    f.write("region,total\n")
    for r in rows:
        f.write("%s,%s\n" % (r["region"], r["total"]))
`
	if err := validateCodegenSource(code); err != nil {
		t.Fatalf("合法只读代码被拒绝：%v", err)
	}
}

func TestValidateCodegenSource_RejectsBannedImports(t *testing.T) {
	cases := map[string]string{
		"裸 MySQL 驱动": `import pymysql`,
		"裸 SQLite 驱动": `import sqlite3`,
		"from 裸驱动":    `from oracledb import connect`,
		"网络模块":        `import socket`,
		"进程模块":        `import subprocess`,
		"原生调用":        `from ctypes import cdll`,
		"逗号导入":        `import os, socket`,
	}
	for name, code := range cases {
		if err := validateCodegenSource(code); err == nil {
			t.Errorf("%s：应被拒绝但通过了", name)
		}
	}
}

func TestValidateCodegenSource_RejectsDangerousCalls(t *testing.T) {
	cases := map[string]string{
		"os.system":      `import os; os.system("whoami")`,
		"eval":           `eval("1+1")`,
		"exec":           `exec("x=1")`,
		"__import__":     `__import__("socket")`,
		"shutil.rmtree":  `import shutil; shutil.rmtree("/")`,
		"os.remove":      `import os; os.remove("a.txt")`,
		"importlib 动态":  `import importlib; importlib.import_module("socket")`,
	}
	for name, code := range cases {
		if err := validateCodegenSource(code); err == nil {
			t.Errorf("%s：应被拒绝但通过了", name)
		}
	}
}

func TestValidateCodegenSource_RejectsEmpty(t *testing.T) {
	if err := validateCodegenSource("   \n\t "); err == nil {
		t.Error("空代码应被拒绝")
	}
}

func TestValidateCodegenOutputRel(t *testing.T) {
	valid := []string{"report.pdf", "sub/report.xlsx", "./out.csv"}
	for _, in := range valid {
		if _, err := validateCodegenOutputRel(in); err != nil {
			t.Errorf("%q 应被接受，却被拒绝：%v", in, err)
		}
	}

	invalid := []string{
		"", "../evil.txt", "..", "a/../../evil.txt",
		"/etc/passwd", `C:\Windows\x.txt`, `\\server\share\x.txt`,
	}
	for _, in := range invalid {
		if _, err := validateCodegenOutputRel(in); err == nil {
			t.Errorf("%q 应被拒绝但通过了", in)
		}
	}
}

func TestFirstIdentifier(t *testing.T) {
	cases := map[string]string{
		"os":                "os",
		"os.path as p":      "os",
		"pymysql":           "pymysql",
		" mysql ":           "mysql",
		"oracledb as odbc":  "oracledb",
	}
	for in, want := range cases {
		if got := firstIdentifier(in); got != want {
			t.Errorf("firstIdentifier(%q)=%q, want %q", in, got, want)
		}
	}
}