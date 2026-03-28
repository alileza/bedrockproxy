package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bedrockproxy_requests_total",
		Help: "Total number of proxied Bedrock requests.",
	}, []string{"model", "operation", "status"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "bedrockproxy_request_duration_seconds",
		Help:    "Bedrock request latency in seconds.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"model", "operation"})

	InputTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bedrockproxy_input_tokens_total",
		Help: "Total input tokens consumed.",
	}, []string{"model", "caller"})

	OutputTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bedrockproxy_output_tokens_total",
		Help: "Total output tokens consumed.",
	}, []string{"model", "caller"})

	CostTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bedrockproxy_cost_usd_total",
		Help: "Total estimated cost in USD.",
	}, []string{"model", "caller"})

	ActiveRequests = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bedrockproxy_active_requests",
		Help: "Number of in-flight Bedrock requests.",
	})

	WebSocketClients = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bedrockproxy_websocket_clients",
		Help: "Number of connected WebSocket clients.",
	})
)
