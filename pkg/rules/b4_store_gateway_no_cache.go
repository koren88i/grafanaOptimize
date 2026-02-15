package rules

// StoreGatewayNoCache detects Thanos store gateways operating without an
// external cache (e.g., memcached). Without caching, every query that touches
// historical data reads blocks from object storage, dramatically increasing
// query latency.
type StoreGatewayNoCache struct{}

func (r *StoreGatewayNoCache) ID() string            { return "B4" }
func (r *StoreGatewayNoCache) RuleSeverity() Severity { return High }

func (r *StoreGatewayNoCache) Check(ctx *AnalysisContext) []Finding {
	// This rule requires live Prometheus metrics to check for cache operations.
	if ctx.PrometheusURL == "" {
		return nil
	}

	// TODO: Query thanos_store_bucket_cache_operation_hits_total.
	// If absent, the store gateway has no cache configured.

	return nil
}
