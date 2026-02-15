package cardinality

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const cacheTTL = 5 * time.Minute

// Client fetches cardinality data from the Prometheus TSDB status API.
type Client struct {
	baseURL    string
	httpClient *http.Client

	mu       sync.Mutex
	cached   *CardinalityData
	cachedAt time.Time
}

// NewClient creates a cardinality client for the given Prometheus base URL.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Fetch retrieves cardinality data, using cache if fresh.
// Returns (nil, error) if the API is unreachable — caller should log and continue.
func (c *Client) Fetch() (*CardinalityData, error) {
	c.mu.Lock()
	if c.cached != nil && time.Since(c.cachedAt) < cacheTTL {
		data := c.cached
		c.mu.Unlock()
		return data, nil
	}
	c.mu.Unlock()

	data, err := c.fetchFromAPI()
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cached = data
	c.cachedAt = time.Now()
	c.mu.Unlock()

	return data, nil
}

// tsdbStatusResponse matches the Prometheus /api/v1/status/tsdb JSON structure.
type tsdbStatusResponse struct {
	Status string         `json:"status"`
	Data   tsdbStatusData `json:"data"`
}

type tsdbStatusData struct {
	HeadStats                  headStats       `json:"headStats"`
	SeriesCountByMetricName    []nameValuePair `json:"seriesCountByMetricName"`
	LabelValueCountByLabelName []nameValuePair `json:"labelValueCountByLabelName"`
	SeriesCountByLabelPair     []nameValuePair `json:"seriesCountByLabelValuePair"`
}

type headStats struct {
	NumSeries int `json:"numSeries"`
}

type nameValuePair struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func (c *Client) fetchFromAPI() (*CardinalityData, error) {
	url := c.baseURL + "/api/v1/status/tsdb"
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching TSDB status from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TSDB status API returned %d from %s", resp.StatusCode, url)
	}

	var tsdb tsdbStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&tsdb); err != nil {
		return nil, fmt.Errorf("decoding TSDB status response: %w", err)
	}

	if tsdb.Status != "success" {
		return nil, fmt.Errorf("TSDB status API returned status %q", tsdb.Status)
	}

	data := &CardinalityData{
		SeriesByMetric:    make(map[string]int, len(tsdb.Data.SeriesCountByMetricName)),
		ValuesByLabel:     make(map[string]int, len(tsdb.Data.LabelValueCountByLabelName)),
		SeriesByLabelPair: make(map[string]int, len(tsdb.Data.SeriesCountByLabelPair)),
		HeadSeriesCount:   tsdb.Data.HeadStats.NumSeries,
	}

	for _, pair := range tsdb.Data.SeriesCountByMetricName {
		data.SeriesByMetric[pair.Name] = pair.Value
	}
	for _, pair := range tsdb.Data.LabelValueCountByLabelName {
		data.ValuesByLabel[pair.Name] = pair.Value
	}
	for _, pair := range tsdb.Data.SeriesCountByLabelPair {
		data.SeriesByLabelPair[pair.Name] = pair.Value
	}

	return data, nil
}
