package audiomuse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	return New(srv.URL+"/", "secret", logger), srv
}

func respondWith(ids ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		results := make([]map[string]any, len(ids))
		for i, id := range ids {
			results[i] = map[string]any{"item_id": id, "fade_in_seconds": nil, "fade_out_seconds": nil}
		}
		json.NewEncoder(w).Encode(map[string]any{"count": len(ids), "results": results})
	}
}

func TestOrderSongsSendsRequestAndUsesOrder(t *testing.T) {
	var gotAuth, gotPath, gotType string
	var gotBody smartFadeRequest
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotType = r.Header.Get("Authorization"), r.URL.Path, r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&gotBody)
		respondWith("a", "c", "d", "b")(w, r)
	})

	got, err := c.OrderSongs(context.Background(), []string{"a", "b", "c", "d"})
	if err != nil {
		t.Fatalf("OrderSongs() error = %v", err)
	}
	if want := []string{"a", "c", "d", "b"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	if gotPath != "/api/recording_search/smart_fade" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotType != "application/json" {
		t.Errorf("Content-Type = %q", gotType)
	}
	if want := []string{"a", "b", "c", "d"}; !slices.Equal(gotBody.ItemIDs, want) {
		t.Errorf("sent ids = %v, want %v", gotBody.ItemIDs, want)
	}
}

func TestOrderSongsWithoutToken(t *testing.T) {
	var gotAuth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Values("Authorization")
		respondWith("a", "b", "c")(w, r)
	}))
	defer srv.Close()

	c := New(srv.URL, "", logrus.New())
	if _, err := c.OrderSongs(context.Background(), []string{"a", "b", "c"}); err != nil {
		t.Fatal(err)
	}
	if len(gotAuth) != 0 {
		t.Errorf("Authorization sent without a token: %v", gotAuth)
	}
}

func TestOrderSongsLeavesShortAndLongListsAlone(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		respondWith()(w, r)
	})

	long := make([]string, MaxSongs+1)
	for i := range long {
		long[i] = fmt.Sprint(i)
	}
	for _, ids := range [][]string{nil, {"a"}, {"a", "b"}, long} {
		got, err := c.OrderSongs(context.Background(), ids)
		if err != nil || !slices.Equal(got, ids) {
			t.Errorf("OrderSongs(%d ids) = %v, %v; want the input unchanged", len(ids), got, err)
		}
	}
	if calls.Load() != 0 {
		t.Errorf("AudioMuse was called %d times for lists it can't order", calls.Load())
	}
}

func TestOrderSongsKeepsEverySong(t *testing.T) {
	tests := []struct {
		name   string
		answer []string
		want   []string
	}{
		{"missing songs go last", []string{"a", "c"}, []string{"a", "c", "b", "d"}},
		{"unknown ids are dropped", []string{"a", "zzz", "d", "c", "b"}, []string{"a", "d", "c", "b"}},
		{"repeated ids count once", []string{"a", "c", "c", "b", "d"}, []string{"a", "c", "b", "d"}},
		{"empty answer keeps the order", nil, []string{"a", "b", "c", "d"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := testClient(t, respondWith(tt.answer...))
			got, err := c.OrderSongs(context.Background(), []string{"a", "b", "c", "d"})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("order = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOrderSongsFailures(t *testing.T) {
	tests := map[string]http.HandlerFunc{
		"HTTP error": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusUnauthorized) },
		"not found":  http.NotFound,
		"not JSON":   func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) },
		"JSON error body": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"x"}`))
		},
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			c, _ := testClient(t, handler)
			if got, err := c.OrderSongs(context.Background(), []string{"a", "b", "c"}); err == nil {
				t.Errorf("OrderSongs() = %v, want an error", got)
			}
		})
	}
}

func TestOrderSongsBacksOffAfterFailure(t *testing.T) {
	var calls atomic.Int32
	fail := atomic.Bool{}
	fail.Store(true)
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		respondWith("a", "c", "b")(w, r)
	})
	now := time.Now()
	c.now = func() time.Time { return now }
	ids := []string{"a", "b", "c"}

	if _, err := c.OrderSongs(context.Background(), ids); err == nil {
		t.Fatal("first call should fail")
	}
	fail.Store(false)
	if _, err := c.OrderSongs(context.Background(), ids); err == nil {
		t.Fatal("call during the back-off should fail")
	}
	if calls.Load() != 1 {
		t.Errorf("AudioMuse was called %d times, want 1 during the back-off", calls.Load())
	}

	now = now.Add(FailureBackoff + time.Second)
	got, err := c.OrderSongs(context.Background(), ids)
	if err != nil || !slices.Equal(got, []string{"a", "c", "b"}) {
		t.Errorf("after the back-off OrderSongs() = %v, %v", got, err)
	}
}

func TestOrderSongsClientGivingUpDoesNotBackOff(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.OrderSongs(ctx, []string{"a", "b", "c"}); err == nil {
		t.Fatal("expected an error for a cancelled context")
	}
	if !c.retryAfter.IsZero() {
		t.Error("a cancelled request must not start a back-off")
	}
}

func TestOrderSongsTimesOut(t *testing.T) {
	release := make(chan struct{})
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	c.http.Timeout = 50 * time.Millisecond

	start := time.Now()
	if _, err := c.OrderSongs(context.Background(), []string{"a", "b", "c"}); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 2*time.Second {
		t.Error("the request wasn't abandoned at the timeout")
	}
}

func TestNewUsesRequestTimeout(t *testing.T) {
	if c := New("http://audiomuse", "", logrus.New()); c.http.Timeout != RequestTimeout {
		t.Errorf("HTTP timeout = %v, want %v", c.http.Timeout, RequestTimeout)
	}
}
