package firms

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAreaRetriesOnTransactionLimit(t *testing.T) {
	RateLimitWait = 10 * time.Millisecond
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("Exceeding allowed transaction limit."))
			return
		}
		_, _ = w.Write([]byte(viirsCSV))
	}))
	defer srv.Close()

	c := NewClient("k")
	c.BaseURL = srv.URL
	dets, err := c.Area(context.Background(), "VIIRS_NOAA20_NRT", BBox{70, 25, 80, 33}, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(dets) != 2 || calls.Load() != 2 {
		t.Fatalf("dets=%d calls=%d", len(dets), calls.Load())
	}
}
