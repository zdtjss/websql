package agent

import (
	"context"
	"errors"
	"testing"
)

// exec_status_test.go 覆盖 exec_sql 的 tool_status 三态映射。
//
// 起因（代码审核 L1）：常量 toolStatusCancelled 声明后生产代码从不上报，
// 前端 toolStatus === 'cancelled' 成为死分支；中断只能落到「状态未知」。
// 现在改为由 execToolStatusForErr 统一裁决，本用例把四条分支钉住。

func TestExecToolStatusForErr(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want string
	}{
		{"成功上报 executed", ctx, nil, toolStatusExecuted},
		{"权限拒绝上报 denied", ctx, &PermissionError{Message: "无权访问该表"}, toolStatusDenied},
		{"安全作用域缺失上报 denied", ctx, ErrSecurityScope, toolStatusDenied},
		{"显式取消上报 cancelled", ctx, context.Canceled, toolStatusCancelled},
		{"超时上报 cancelled", ctx, context.DeadlineExceeded, toolStatusCancelled},
		{"SQL 运行错误不上报", ctx, errors.New("no such table: t"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := execToolStatusForErr(c.ctx, c.err); got != c.want {
				t.Fatalf("execToolStatusForErr(%v) = %q，期望 %q", c.err, got, c.want)
			}
		})
	}

	// 运行上下文已取消时，即便错误本身不带 context.Canceled 也要归为 cancelled。
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := execToolStatusForErr(cctx, errors.New("driver: bad connection")); got != toolStatusCancelled {
		t.Fatalf("运行上下文已取消时应上报 cancelled，实际 %q", got)
	}
}
