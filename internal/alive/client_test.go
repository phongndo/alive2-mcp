package alive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// flaky fails the first `failures` requests with fail, then answers ok.
func flaky(t *testing.T, failures int32, fail func(http.ResponseWriter)) (*Client, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= failures {
			fail(w)
			return
		}
		_, _ = w.Write([]byte(`{"sessions":[{"source":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "", ""), &calls
}

func dropConnection(w http.ResponseWriter) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func status(code int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.WriteHeader(code) }
}

func TestRetries(t *testing.T) {
	old := backoff
	backoff = time.Millisecond
	t.Cleanup(func() { backoff = old })

	tests := []struct {
		name      string
		failures  int32
		fail      func(http.ResponseWriter)
		wantErr   string
		wantCalls int32
	}{
		{"dropped connection is retried", 2, dropConnection, "", 3},
		{"gateway error is retried", 1, status(http.StatusServiceUnavailable), "", 2},
		{"client error is not retried", 5, status(http.StatusNotFound), "HTTP 404", 1},
		{"retries give up", 5, status(http.StatusBadGateway), "after 3 attempts", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, calls := flaky(t, tt.failures, tt.fail)
			s, err := c.Resolve(context.Background(), "abc")
			if tt.wantErr == "" && (err != nil || s.Source != "ok") {
				t.Fatalf("Resolve = %+v, %v", s, err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
			if got := calls.Load(); got != tt.wantCalls {
				t.Errorf("calls = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}

func TestRetryStopsWhenCanceled(t *testing.T) {
	c, calls := flaky(t, 5, status(http.StatusBadGateway))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Resolve(ctx, "abc"); err == nil {
		t.Fatal("expected an error")
	}
	if calls.Load() > 1 {
		t.Errorf("canceled call retried %d times", calls.Load())
	}
}
