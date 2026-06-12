package wapi

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "wapi",
			Name:      "requests_total",
			Help:      "Total Infoblox WAPI requests made by the exporter.",
		}, []string{"object", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "wapi",
			Name:      "request_duration_seconds",
			Help:      "Infoblox WAPI request duration by object.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"object"}),
	}
}

func (m *Metrics) Collectors() []prometheus.Collector {
	if m == nil {
		return nil
	}
	return []prometheus.Collector{m.requests, m.duration}
}

func (m *Metrics) observe(object string, code string, seconds float64) {
	if m == nil {
		return
	}
	m.requests.WithLabelValues(object, code).Inc()
	m.duration.WithLabelValues(object).Observe(seconds)
}
