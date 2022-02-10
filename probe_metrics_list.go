package main

import (
	"context"
	"crypto/sha1" // #nosec G505
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/webdevops/azure-metrics-exporter/metrics"
	"net/http"
	"time"
)

func probeMetricsListHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	var timeoutSeconds float64

	startTime := time.Now()
	contextLogger := buildContextLoggerFromRequest(r)
	registry := prometheus.NewRegistry()

	// If a timeout is configured via the Prometheus header, add it to the request.
	timeoutSeconds, err = getPrometheusTimeout(r, ProbeMetricsListTimeoutDefault)
	if err != nil {
		contextLogger.Error(err)
		http.Error(w, fmt.Sprintf("failed to parse timeout from Prometheus header: %s", err), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds*float64(time.Second)))
	defer cancel()
	r = r.WithContext(ctx)

	var settings metrics.RequestMetricSettings
	if settings, err = metrics.NewRequestMetricSettingsForAzureResourceApi(r, opts); err != nil {
		contextLogger.Errorln(err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	prober := metrics.NewMetricProber(ctx, contextLogger, w, r, &settings, opts)
	prober.SetUserAgent(UserAgent + gitTag)
	prober.SetAzure(AzureEnvironment, AzureAuthorizer)
	prober.SetPrometheusRegistry(registry)
	if settings.Cache != nil {
		cacheKey := fmt.Sprintf("list:%x", sha1.Sum([]byte(r.URL.String()))) // #nosec G401
		prober.EnableMetricsCache(metricsCache, cacheKey, settings.CacheDuration(startTime))
	}

	if opts.Azure.ServiceDiscovery.CacheDuration.Seconds() > 0 {
		prober.EnableServiceDiscoveryCache(azureCache, opts.Azure.ServiceDiscovery.CacheDuration)
	}

	if !prober.FetchFromCache() {
		for _, subscription := range settings.Subscriptions {
			prober.ServiceDiscovery.FindSubscriptionResources(subscription, settings.Filter)
		}

		prober.RegisterSubscriptionCollectFinishCallback(func(subscriptionId string) {
			// global stats counter
			prometheusCollectTime.With(prometheus.Labels{
				"subscriptionID": subscriptionId,
				"handler":        ProbeMetricsListUrl,
				"filter":         settings.Filter,
			}).Observe(time.Since(startTime).Seconds())
		})

		prober.Run()
	} else {
		w.Header().Add("X-metrics-cached", "true")
		for _, subscriptionId := range settings.Subscriptions {
			prometheusMetricRequests.With(prometheus.Labels{
				"subscriptionID": subscriptionId,
				"handler":        ProbeMetricsListUrl,
				"filter":         settings.Filter,
				"result":         "cached",
			}).Inc()
		}
	}

	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}
