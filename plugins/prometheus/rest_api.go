package prometheus

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/iotaledger/hive.go/events"
)

var (
	restapiHTTPErrorCount prometheus.Gauge

	restapiPoWCompletedCount prometheus.Gauge
	restapiPoWMessageSizes   prometheus.Histogram
	restapiPoWDurations      prometheus.Histogram
)

func configureRestAPI() {
	restapiHTTPErrorCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "iota",
			Subsystem: "restapi",
			Name:      "http_request_error_count",
			Help:      "The amount of encountered HTTP request errors.",
		},
	)

	restapiPoWCompletedCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "iota",
			Subsystem: "restapi",
			Name:      "pow_count",
			Help:      "The amount of completed REST API PoW requests.",
		},
	)

	restapiPoWMessageSizes = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "iota",
			Subsystem: "restapi",
			Name:      "pow_message_sizes",
			Help:      "The message size of REST API PoW requests.",
			Buckets:   powMessageSizeBuckets,
		})

	restapiPoWDurations = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "iota",
			Subsystem: "restapi",
			Name:      "pow_durations",
			Help:      "The duration of REST API PoW requests [s].",
			Buckets:   powDurationBuckets,
		})

	registry.MustRegister(restapiHTTPErrorCount)

	registry.MustRegister(restapiPoWCompletedCount)
	registry.MustRegister(restapiPoWMessageSizes)
	registry.MustRegister(restapiPoWDurations)

	deps.RestAPIMetrics.Events.PoWCompleted.Attach(events.NewClosure(func(messageSize int, duration time.Duration) {
		restapiPoWMessageSizes.Observe(float64(messageSize))
		restapiPoWDurations.Observe(duration.Seconds())
	}))

	addCollect(collectRestAPI)
}

func collectRestAPI() {
	restapiHTTPErrorCount.Set(float64(deps.RestAPIMetrics.HTTPRequestErrorCounter.Load()))
	restapiPoWCompletedCount.Set(float64(deps.RestAPIMetrics.PoWCompletedCounter.Load()))
}
