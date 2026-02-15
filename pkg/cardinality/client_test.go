package cardinality

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const validTSDBResponse = `{
	"status": "success",
	"data": {
		"headStats": {"numSeries": 54321},
		"seriesCountByMetricName": [
			{"name": "http_requests_total", "value": 5000},
			{"name": "go_goroutines", "value": 12},
			{"name": "node_cpu_seconds_total", "value": 800}
		],
		"labelValueCountByLabelName": [
			{"name": "instance", "value": 200},
			{"name": "job", "value": 15},
			{"name": "pod", "value": 3000}
		],
		"seriesCountByLabelValuePair": [
			{"name": "job=api-server", "value": 300},
			{"name": "job=prometheus", "value": 150}
		]
	}
}`

func TestFetch_ValidResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status/tsdb" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(validTSDBResponse))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second)
	data, err := client.Fetch()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if data.HeadSeriesCount != 54321 {
		t.Errorf("HeadSeriesCount = %d, want 54321", data.HeadSeriesCount)
	}
	if got := data.SeriesByMetric["http_requests_total"]; got != 5000 {
		t.Errorf("SeriesByMetric[http_requests_total] = %d, want 5000", got)
	}
	if got := data.SeriesByMetric["go_goroutines"]; got != 12 {
		t.Errorf("SeriesByMetric[go_goroutines] = %d, want 12", got)
	}
	if got := data.ValuesByLabel["pod"]; got != 3000 {
		t.Errorf("ValuesByLabel[pod] = %d, want 3000", got)
	}
	if got := data.SeriesByLabelPair["job=api-server"]; got != 300 {
		t.Errorf("SeriesByLabelPair[job=api-server] = %d, want 300", got)
	}
}

func TestFetch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second)
	data, err := client.Fetch()
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if data != nil {
		t.Error("expected nil data for error response")
	}
}

func TestFetch_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second)
	data, err := client.Fetch()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if data != nil {
		t.Error("expected nil data for invalid JSON")
	}
}

func TestFetch_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status": "error", "error": "something broke"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second)
	data, err := client.Fetch()
	if err == nil {
		t.Fatal("expected error for non-success status")
	}
	if data != nil {
		t.Error("expected nil data for error status")
	}
}

func TestFetch_Caching(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Write([]byte(validTSDBResponse))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second)

	// First call hits the server
	data1, err := client.Fetch()
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}

	// Second call should use cache
	data2, err := client.Fetch()
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call (cached), got %d", callCount)
	}

	// Both should return same data
	if data1.HeadSeriesCount != data2.HeadSeriesCount {
		t.Error("cached data should match original")
	}
}

func TestFetch_Unreachable(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", 1*time.Second)
	data, err := client.Fetch()
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if data != nil {
		t.Error("expected nil data for unreachable server")
	}
}

func TestEstimatedSeries(t *testing.T) {
	data := &CardinalityData{
		SeriesByMetric: map[string]int{
			"http_requests_total": 5000,
		},
	}

	// Known metric
	if got := data.EstimatedSeries("http_requests_total", DefaultHeuristicSeries); got != 5000 {
		t.Errorf("known metric: got %d, want 5000", got)
	}

	// Unknown metric falls back to default
	if got := data.EstimatedSeries("unknown_metric", DefaultHeuristicSeries); got != DefaultHeuristicSeries {
		t.Errorf("unknown metric: got %d, want %d", got, DefaultHeuristicSeries)
	}

	// Nil receiver
	var nilData *CardinalityData
	if got := nilData.EstimatedSeries("any", DefaultHeuristicSeries); got != DefaultHeuristicSeries {
		t.Errorf("nil receiver: got %d, want %d", got, DefaultHeuristicSeries)
	}
}

func TestLabelCardinality(t *testing.T) {
	data := &CardinalityData{
		ValuesByLabel: map[string]int{
			"instance": 200,
		},
	}

	if got := data.LabelCardinality("instance", 100); got != 200 {
		t.Errorf("known label: got %d, want 200", got)
	}
	if got := data.LabelCardinality("unknown", 100); got != 100 {
		t.Errorf("unknown label: got %d, want 100", got)
	}

	var nilData *CardinalityData
	if got := nilData.LabelCardinality("any", 100); got != 100 {
		t.Errorf("nil receiver: got %d, want 100", got)
	}
}
