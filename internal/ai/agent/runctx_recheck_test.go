package agent

import (
	"testing"

	admin "websql/internal/app/admin"
)

// TestRecheckBeforeRun_UnavailableFailClosed 租约过期时不得回退旧 Scope 继续取数。
func TestRecheckBeforeRun_UnavailableFailClosed(t *testing.T) {
	admin.MarkPermissionRevisionStale()
	a := &SQLAgent{}
	sec := &RunSecurityContext{
		Profile:       ProfileWebRemote,
		UserID:        "u1",
		DefaultConnID: "c1",
		Revision:      0,
		Masker:        NewNoopMasker(),
	}
	got := a.recheckSecurityBeforeRun(ChatRequest{SessionID: "s1"}, sec)
	if got == nil {
		t.Fatal("复核不得返回 nil")
	}
	if !got.RevisionUnavailable {
		t.Fatal("租约过期应标记 RevisionUnavailable")
	}
	if got.DataToolsAllowed() {
		t.Fatal("修订号不可用时数据工具必须 fail-closed")
	}
}

// TestRecheckBeforeRun_UnchangedKeepsScope 修订号未变时沿用同一不可变上下文。
func TestRecheckBeforeRun_UnchangedKeepsScope(t *testing.T) {
	a := &SQLAgent{}
	sec := &RunSecurityContext{
		Profile:       ProfileDesktopLocal,
		UserID:        "u1",
		DefaultConnID: "c1",
		Revision:      admin.CurrentPermissionRevision(),
		Masker:        NewNoopMasker(),
	}
	got := a.recheckSecurityBeforeRun(ChatRequest{SessionID: "s1"}, sec)
	if got != sec {
		t.Fatal("修订号未变时应沿用同一 Scope 实例")
	}
	if got.RevisionUnavailable || !got.DataToolsAllowed() {
		t.Fatal("未变更时不得标记不可用")
	}
}
