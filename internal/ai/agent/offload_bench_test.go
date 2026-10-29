package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkReadOffloadWindow(b *testing.B) {
	root := b.TempDir()
	b.Setenv("REDUCTION_OFFLOAD_DIR", root)
	ctx := WithRunIdentity(context.Background(), RunIdentity{UserID: "u1", SessionID: "s1"})
	dir := filepath.Join(offloadRootForContext(ctx), "trunc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.Fatal(err)
	}
	var sb strings.Builder
	for i := 0; i < 10_000; i++ {
		fmt.Fprintf(&sb, "[%d,\"value-%d\"]\n", i, i)
	}
	path := filepath.Join(dir, "bench.jsonl")
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	read := NewReadOffloadFunc()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := read(ctx, &ReadOffloadInput{Path: path, Offset: 100, Limit: 100}); err != nil {
			b.Fatal(err)
		}
	}
}
