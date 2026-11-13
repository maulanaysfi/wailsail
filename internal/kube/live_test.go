package kube

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveCluster exercises listing and watching against a real cluster.
// It only runs when WAILSAIL_LIVE_CONTEXT names a kubeconfig context.
func TestLiveCluster(t *testing.T) {
	ctxName := os.Getenv("WAILSAIL_LIVE_CONTEXT")
	if ctxName == "" {
		t.Skip("set WAILSAIL_LIVE_CONTEXT to run")
	}
	m := NewManager()
	c, err := m.For(ctxName)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	pods := ResourceRef{Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true}
	tbl, err := c.listTable(ctx, pods, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("columns=%v rows=%d rv=%s", tbl.Columns, len(tbl.Rows), tbl.ResourceVersion)
	if len(tbl.Rows) > 0 {
		t.Logf("first row=%+v", tbl.Rows[0])
	}
	got := 0
	wctx, wcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer wcancel()
	c.runWatch(wctx, pods, "", func(ev WatchEvent) {
		got++
		t.Logf("event reset=%v upserts=%d deletes=%d err=%q", ev.Reset, len(ev.Upserts), len(ev.Deletes), ev.Error)
	})
	if got == 0 {
		t.Fatal("expected at least the initial reset event")
	}
}
