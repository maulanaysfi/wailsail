package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// APIError is a non-2xx response from the API server.
type APIError struct {
	Code    int
	Reason  string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("api server returned HTTP %d", e.Code)
}

// IsGone reports whether err means the requested resourceVersion expired.
func IsGone(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && (ae.Code == http.StatusGone || ae.Reason == string(metav1.StatusReasonExpired))
}

func statusError(code int, body []byte) error {
	var st metav1.Status
	if json.Unmarshal(body, &st) == nil && st.Kind == "Status" {
		c := int(st.Code)
		if c == 0 {
			c = code
		}
		return &APIError{Code: c, Reason: string(st.Reason), Message: st.Message}
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return &APIError{Code: code, Message: msg}
}

// get performs a raw GET against the API server using the context's
// authenticated transport.
func (c *Clients) get(ctx context.Context, path string, query url.Values, accept string) (*http.Response, error) {
	base, _, err := rest.DefaultServerUrlFor(c.Config)
	if err != nil {
		return nil, err
	}
	u := *base
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, statusError(resp.StatusCode, body)
	}
	return resp, nil
}

type watchOptions struct {
	labelSelector string
	fieldSelector string
}

func (o watchOptions) apply(q url.Values) {
	if o.labelSelector != "" {
		q.Set("labelSelector", o.labelSelector)
	}
	if o.fieldSelector != "" {
		q.Set("fieldSelector", o.fieldSelector)
	}
}

// listTable lists a collection as a server-printed table, following
// pagination.
func (c *Clients) listTable(ctx context.Context, ref ResourceRef, namespace string, opts watchOptions) (*tableResult, error) {
	var all *tableResult
	cont := ""
	for {
		q := url.Values{"limit": {"500"}}
		if ref.Kind == "Pod" {
			q.Set("includeObject", "Object")
		}
		opts.apply(q)
		if cont != "" {
			q.Set("continue", cont)
		}
		resp, err := c.get(ctx, ref.Path(namespace, ""), q, AcceptTable)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		page, err := parseTable(body, all)
		if err != nil {
			return nil, err
		}
		if all == nil {
			all = page
		} else {
			all.Rows = append(all.Rows, page.Rows...)
			all.ResourceVersion = page.ResourceVersion
		}
		cont = page.Continue
		if cont == "" {
			return all, nil
		}
	}
}

// WatchEvent is the payload pushed to the frontend for a resource watch.
// A Reset event replaces the whole table; otherwise Upserts/Deletes are
// applied incrementally (Deletes contains row keys).
type WatchEvent struct {
	Reset   bool     `json:"reset,omitempty"`
	Columns []Column `json:"columns,omitempty"`
	Upserts []Row    `json:"upserts,omitempty"`
	Deletes []string `json:"deletes,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// batcher coalesces watch events so the UI is updated at most every flush
// interval regardless of how chatty the cluster is.
type batcher struct {
	mu      sync.Mutex
	reset   bool
	columns []Column
	upserts map[string]Row
	deletes map[string]struct{}
	errMsg  string
	dirty   bool
}

func newBatcher() *batcher {
	return &batcher{upserts: map[string]Row{}, deletes: map[string]struct{}{}}
}

func (b *batcher) doReset(cols []Column, rows []Row) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reset, b.columns, b.dirty, b.errMsg = true, cols, true, ""
	b.upserts = make(map[string]Row, len(rows))
	b.deletes = map[string]struct{}{}
	for _, r := range rows {
		b.upserts[r.Key] = r
	}
}

func (b *batcher) upsert(r Row) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.deletes, r.Key)
	b.upserts[r.Key] = r
	b.dirty = true
}

func (b *batcher) remove(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.upserts, key)
	if !b.reset {
		b.deletes[key] = struct{}{}
	}
	b.dirty = true
}

func (b *batcher) fail(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.errMsg, b.dirty = msg, true
}

// take returns the pending event, if any, and clears the buffer.
func (b *batcher) take() (WatchEvent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.dirty {
		return WatchEvent{}, false
	}
	ev := WatchEvent{Reset: b.reset, Error: b.errMsg}
	if b.reset {
		ev.Columns = b.columns
	}
	ev.Upserts = make([]Row, 0, len(b.upserts))
	for _, r := range b.upserts {
		ev.Upserts = append(ev.Upserts, r)
	}
	for k := range b.deletes {
		ev.Deletes = append(ev.Deletes, k)
	}
	b.reset, b.errMsg, b.dirty = false, "", false
	b.upserts = map[string]Row{}
	b.deletes = map[string]struct{}{}
	return ev, true
}

// flushInterval is how often batched watch events are delivered.
const flushInterval = 150 * time.Millisecond

// runWatch lists then watches a collection until ctx is cancelled, relisting
// when the resourceVersion expires and backing off on errors.
func (c *Clients) runWatch(ctx context.Context, ref ResourceRef, namespace string, opts watchOptions, emit func(WatchEvent)) {
	if isHelmReleaseRef(ref) {
		c.runHelmWatch(ctx, namespace, opts, emit)
		return
	}
	b := newBatcher()
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(flushInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				close(done)
				return
			case <-t.C:
				if ev, ok := b.take(); ok {
					emit(ev)
				}
			}
		}
	}()
	defer func() { <-done }()

	var table *tableResult
	rv := ""
	backoff := time.Second
	sleep := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
		return true
	}

	for ctx.Err() == nil {
		if rv == "" {
			t, err := c.listTable(ctx, ref, namespace, opts)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				b.fail(err.Error())
				if !sleep() {
					return
				}
				continue
			}
			table, rv = t, t.ResourceVersion
			b.doReset(t.Columns, t.Rows)
			backoff = time.Second
		}
		newRV, err := c.watchTable(ctx, ref, namespace, rv, opts, table, b)
		if ctx.Err() != nil {
			return
		}
		switch {
		case err == nil:
			rv = newRV
		case IsGone(err):
			rv = ""
		default:
			b.fail(err.Error())
			rv = ""
			if !sleep() {
				return
			}
		}
	}
}

type rawWatchEvent struct {
	Type   string          `json:"type"`
	Object json.RawMessage `json:"object"`
}

// watchTable consumes one watch connection. It returns the last observed
// resourceVersion when the server closes the stream normally.
func (c *Clients) watchTable(ctx context.Context, ref ResourceRef, namespace, rv string, opts watchOptions, table *tableResult, b *batcher) (string, error) {
	q := url.Values{
		"watch":               {"1"},
		"resourceVersion":     {rv},
		"allowWatchBookmarks": {"true"},
		"timeoutSeconds":      {"300"},
	}
	if ref.Kind == "Pod" {
		q.Set("includeObject", "Object")
	}
	opts.apply(q)
	resp, err := c.get(ctx, ref.Path(namespace, ""), q, AcceptTable)
	if err != nil {
		return rv, err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	for {
		var ev rawWatchEvent
		if err := dec.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return rv, nil
			}
			return rv, err
		}
		if ev.Type == "ERROR" {
			return rv, statusError(http.StatusInternalServerError, ev.Object)
		}
		res, err := parseTable(ev.Object, table)
		if err != nil {
			continue
		}
		if res.ResourceVersion != "" {
			rv = res.ResourceVersion
		}
		switch ev.Type {
		case "ADDED", "MODIFIED":
			for _, r := range res.Rows {
				b.upsert(r)
			}
		case "DELETED":
			for _, r := range res.Rows {
				b.remove(r.Key)
			}
		}
	}
}
