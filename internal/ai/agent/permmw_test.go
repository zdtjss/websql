package agent

import (
	"errors"
	"fmt"
	"testing"
)

// permmw_test.go 覆盖 F-27 的工具状态上报判定：
// 只有权限/能力拒绝才能上报 denied，SQL 运行错误不得被说成「权限拒绝」。

func TestIsPermissionDenial(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil 不是拒绝", nil, false},
		{"权限错误是拒绝", &PermissionError{Message: "无权访问所选连接", Objects: []string{"c2"}}, true},
		{"包装后的权限错误仍是拒绝", fmt.Errorf("工具执行失败: %w", &PermissionError{Message: "能力不足"}), true},
		{"安全上下文缺失是拒绝", fmt.Errorf("build run security: %w", ErrSecurityScope), true},
		{"SQL 运行错误不是拒绝", errors.New("SQL logic error: no such table: t_history (1)"), false},
		{"语法错误不是拒绝", errors.New("near \"SETT\": syntax error"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPermissionDenial(tc.err); got != tc.want {
				t.Fatalf("isPermissionDenial(%v) = %v，期望 %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestExecToolStatusLabels 冻结上报给前端的终态字面量，
// 避免与 web-src/src/views/ai/utils/execOutcome.ts 的映射漂移。
func TestExecToolStatusLabels(t *testing.T) {
	labels := map[string]bool{
		"executed":  true,
		"denied":    true,
		"cancelled": true,
	}
	for _, status := range []string{toolStatusExecuted, toolStatusDenied, toolStatusCancelled} {
		if !labels[status] {
			t.Fatalf("未知的 tool_status 取值 %q：前端 execOutcome 映射需同步", status)
		}
	}
}
