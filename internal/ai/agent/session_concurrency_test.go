package agent

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// ─── 会话运行互斥（同一会话禁止并发 Run/Resume） ─────────────

func TestSessionRunMutex(t *testing.T) {
	sess := &Session{ID: "s1", UserID: "u1"}

	if !sess.TryBeginRun() {
		t.Fatal("首次应进入运行态")
	}
	if sess.TryBeginRun() {
		t.Error("运行中再次 TryBeginRun 应失败（并发运行会让消息交错、破坏 tool_calls 配对）")
	}
	if !sess.IsRunning() {
		t.Error("IsRunning 应为 true")
	}
	sess.EndRun()
	if !sess.TryBeginRun() {
		t.Error("EndRun 之后应可再次进入运行态")
	}
	sess.EndRun()
}

// BeginRunWait 用于 resume：容忍「中断已推送、RunStream 尚在收尾」的窗口
func TestSessionBeginRunWait(t *testing.T) {
	sess := &Session{ID: "s1", UserID: "u1"}

	if !sess.BeginRunWait(time.Second) {
		t.Fatal("空闲会话应立即进入运行态")
	}

	start := time.Now()
	if sess.BeginRunWait(150 * time.Millisecond) {
		t.Error("运行中的会话在超时后不应进入运行态")
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("应等待至超时，实际等待 %v", elapsed)
	}

	sess.EndRun()
	if !sess.BeginRunWait(time.Second) {
		t.Error("运行结束后应可进入运行态")
	}
	sess.EndRun()
}

// ─── summarization 双信号检测 ───────────────────────────────

func TestHasSummarizationMarker(t *testing.T) {
	msgs := []*schema.Message{
		{Role: schema.System, Content: "sys"},
		{Role: schema.User, Content: "历史摘要"},
	}
	if hasSummarizationMarker(msgs) {
		t.Error("无标记时不应命中")
	}

	msgs[1].Extra = map[string]any{summarizationContentTypeKey: summarizationContentTypeSummary}
	if !hasSummarizationMarker(msgs) {
		t.Error("summary 标记应命中")
	}

	// 其他 content type（如 skills 保留消息）不应命中
	msgs[1].Extra = map[string]any{summarizationContentTypeKey: "skills"}
	if hasSummarizationMarker(msgs) {
		t.Error("非 summary 的 content type 不应命中")
	}
}

// ─── agentsmd 注入消息不落会话 ──────────────────────────────

func TestSchemaMessageToSession_SkipsAgentsMD(t *testing.T) {
	msg := &schema.Message{Role: schema.User, Content: "AGENTS.md 内容"}
	if sm := schemaMessageToSession(msg); sm == nil {
		t.Fatal("普通 user 消息应可转换")
	}

	msg.Extra = map[string]any{agentsMDExtraKey: true}
	if sm := schemaMessageToSession(msg); sm != nil {
		t.Error("agentsmd 注入消息不应持久化（否则重复注入并污染标题）")
	}
}

// ─── 会话搜索 LIKE 转义 ────────────────────────────────────

func TestEscapeLikeKeyword(t *testing.T) {
	cases := map[string]string{
		"销售":      "销售",
		"100%":    `100\%`,
		"a_b":     `a\_b`,
		`back\sl`: `back\\sl`,
	}
	for in, want := range cases {
		if got := escapeLikeKeyword(in); got != want {
			t.Errorf("escapeLikeKeyword(%q) = %q, want %q", in, got, want)
		}
	}
}

// ─── ShouldRetry：provider 结构化错误优先 ───────────────────

func TestShouldRetry_ProviderAPIError(t *testing.T) {
	fn := buildShouldRetryFunc()
	ctx := context.Background()

	// 401：鉴权失败不重试
	r := fn(ctx, &adk.RetryContext{Err: &openai.APIError{HTTPStatusCode: 401, Message: "unauthorized"}})
	if r.Retry {
		t.Error("401 不应重试")
	}
	// 429：重试且退避更长
	r = fn(ctx, &adk.RetryContext{Err: &openai.APIError{HTTPStatusCode: 429, Message: "rate limited"}})
	if !r.Retry || r.Backoff < 3*time.Second {
		t.Errorf("429 应重试且退避 ≥3s，实际 %+v", r)
	}
	// 503：重试
	r = fn(ctx, &adk.RetryContext{Err: &openai.APIError{HTTPStatusCode: 503, Message: "overloaded"}})
	if !r.Retry {
		t.Error("503 应重试")
	}
}
