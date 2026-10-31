package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/adk"
)

// retry_policy_test.go 冻结 F-27/成本相关的重试边界：
// 运行被取消或超时时绝不重试。此前 context.Canceled 会被判为可重试错误，
// 导致模型流走「撤回本次输出」分支，已推送给用户的正文既不重发也不落库
// （界面有回复、重开历史却消失），同时白费额度并可能重复执行已调用工具。

func TestShouldRetry_CanceledAndTimeoutAreNotRetryable(t *testing.T) {
	fn := buildShouldRetryFunc()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{"context.Canceled 不重试", context.Background(), context.Canceled},
		{"包装后的 context.Canceled 不重试", context.Background(), fmt.Errorf("[NodeRunError] %w", context.Canceled)},
		{"context.DeadlineExceeded 不重试", context.Background(), context.DeadlineExceeded},
		{"运行 ctx 已结束时不重试（错误文本不含关键字）", canceledCtx, errors.New("upstream aborted")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := fn(tc.ctx, &adk.RetryContext{Err: tc.err, RetryAttempt: 1})
			if decision.Retry {
				t.Fatalf("取消/超时不得重试，err=%v", tc.err)
			}
		})
	}
}

// TestShouldRetry_TransientErrorsStillRetry 确认收紧没有误伤正常重试场景。
func TestShouldRetry_TransientErrorsStillRetry(t *testing.T) {
	fn := buildShouldRetryFunc()
	for _, msg := range []string{"unexpected EOF", "connection reset by peer", "too many requests"} {
		t.Run(msg, func(t *testing.T) {
			decision := fn(context.Background(), &adk.RetryContext{Err: errors.New(msg), RetryAttempt: 1})
			if !decision.Retry {
				t.Fatalf("瞬时错误 %q 仍应重试", msg)
			}
		})
	}
}
