package cardinality

// DefaultHeuristicSeries is the assumed series count for an unknown metric
// when TSDB status data is not available.
const DefaultHeuristicSeries = 1000

// CardinalityData holds cardinality information fetched from the Prometheus
// TSDB status API (/api/v1/status/tsdb). This is added to AnalysisContext
// and is nil when no Prometheus URL is configured.
type CardinalityData struct {
	// SeriesByMetric maps metric name to its active series count.
	SeriesByMetric map[string]int

	// ValuesByLabel maps label name to its distinct value count.
	ValuesByLabel map[string]int

	// SeriesByLabelPair maps "label=value" to its series count.
	SeriesByLabelPair map[string]int

	// HeadSeriesCount is the total number of active head series.
	HeadSeriesCount int
}

// EstimatedSeries returns the series count for a metric from TSDB data,
// or defaultCount if the metric is not found or the receiver is nil.
func (c *CardinalityData) EstimatedSeries(metricName string, defaultCount int) int {
	if c == nil {
		return defaultCount
	}
	if count, ok := c.SeriesByMetric[metricName]; ok {
		return count
	}
	return defaultCount
}

// LabelCardinality returns the distinct value count for a label,
// or defaultCount if not found or the receiver is nil.
func (c *CardinalityData) LabelCardinality(labelName string, defaultCount int) int {
	if c == nil {
		return defaultCount
	}
	if count, ok := c.ValuesByLabel[labelName]; ok {
		return count
	}
	return defaultCount
}
