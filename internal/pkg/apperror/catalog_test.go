package apperror

import (
	"errors"
	"strings"
	"testing"
)

// TestFromDatabase 错误分类：每类驱动原文都应映射到稳定错误码与可读文案。
func TestFromDatabase(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantCode string
	}{
		{"nil", nil, ""},
		{"超时", errors.New("context deadline exceeded"), CodeDBTimeout},
		{"取消", errors.New("query execution was interrupted"), CodeDBTimeout},
		{"认证", errors.New("Error 1045: Access denied for user 'root'@'10.0.0.1'"), CodeDBAuth},
		{"库不存在", errors.New("Error 1049: Unknown database 'demo'"), CodeDBUnknownDB},
		{"权限", errors.New("Error 1142: SELECT command denied to user 'app'@'%'"), CodeDBPermission},
		{"死锁", errors.New("Error 1213: Deadlock found when trying to get lock"), CodeDBLockWait},
		{"重复键", errors.New("Error 1062: Duplicate entry '1' for key 'PRIMARY'"), CodeDBDuplicate},
		{"表不存在", errors.New("Error 1146: Table 'demo.t_user' doesn't exist"), CodeDBObjectAbsent},
		{"数据超长", errors.New("Error 1406: Data too long for column 'name'"), CodeDBDataRange},
		{"连接拒绝", errors.New("dial tcp 10.0.0.1:3306: connect: connection refused"), CodeDBConnRefused},
		{"语法", errors.New("Error 1064: You have an error in your SQL syntax"), CodeDBSyntax},
		{"不支持", errors.New("feature not supported in this version"), CodeDBUnsupported},
		{"未分类", errors.New("some totally unknown driver failure"), CodeDBUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FromDatabase(tc.err)
			if tc.err == nil {
				if got != nil {
					t.Fatalf("nil 错误应返回 nil，实际 %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("应返回应用错误")
			}
			if got.Code != tc.wantCode {
				t.Fatalf("code 期望 %s，实际 %s（msg=%s）", tc.wantCode, got.Code, got.Msg)
			}
			if got.Msg == "" || got.Hint == "" {
				t.Errorf("应同时给出描述与建议，实际 msg=%q hint=%q", got.Msg, got.Hint)
			}
			// 面向用户的文案不得包含驱动原文里的对象名/主机等细节
			if strings.Contains(got.Msg, "Error 1") || strings.Contains(got.Msg, "tcp ") {
				t.Errorf("用户文案不应包含驱动原文: %q", got.Msg)
			}
			if got.Cause == nil {
				t.Error("应保留原始错误用于日志")
			}
		})
	}
}

// TestAs 错误链中应能取回应用错误。
func TestAs(t *testing.T) {
	origin := errors.New("Error 1045: Access denied for user 'root'")
	appErr := FromDatabase(origin)
	wrapped := Wrap(appErr.Code, appErr.Msg, appErr.Hint, appErr)

	got, ok := As(wrapped)
	if !ok || got == nil {
		t.Fatal("应从错误链中取回应用错误")
	}
	if got.Code != CodeDBAuth {
		t.Errorf("code 期望 %s，实际 %s", CodeDBAuth, got.Code)
	}
}
