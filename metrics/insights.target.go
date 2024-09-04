package metrics

import (
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/monitor/armmonitor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/webdevops/go-common/azuresdk/armclient"
	stringsCommon "github.com/webdevops/go-common/strings"
	"github.com/webdevops/go-common/utils/to"
)

type (
	AzureInsightMetricsResult struct {
		AzureInsightBaseMetricsResult

		target *MetricProbeTarget
		Result *armmonitor.MetricsClientListResponse
	}
)

func (r *AzureInsightMetricsResult) SendMetricToChannel(channel chan<- PrometheusMetricResult) {
	if r.Result.Value != nil {
		// DEBUGGING
		// data, _ := json.Marshal(r.Result)
		// fmt.Println(string(data))

		for _, metric := range r.Result.Value {
			if metric.Timeseries != nil {
				for _, timeseries := range metric.Timeseries {
					if timeseries.Data != nil {
						// get dimension name (optional)
						dimensions := map[string]string{}
						if timeseries.Metadatavalues != nil {
							for _, dimensionRow := range timeseries.Metadatavalues {
								dimensions[to.String(dimensionRow.Name.Value)] = to.String(dimensionRow.Value)
							}
						}

						resourceId := r.target.ResourceId
						azureResource, _ := armclient.ParseResourceId(resourceId)

						metricUnit := ""
						if metric.Unit != nil {
							metricUnit = string(*metric.Unit)
						}

						subscriptionName := ""
						if subscription, err := r.prober.AzureClient.GetCachedSubscription(r.prober.ctx, azureResource.Subscription); err == nil && subscription != nil {
							subscriptionName = to.String(subscription.DisplayName)
						}

						metricLabels := prometheus.Labels{
							"resourceID":       strings.ToLower(resourceId),
							"subscriptionID":   azureResource.Subscription,
							"subscriptionName": subscriptionName,
							"resourceGroup":    azureResource.ResourceGroup,
							"resourceName":     azureResource.ResourceName,
							"metric":           to.String(metric.Name.Value),
							"unit":             metricUnit,
							"interval":         to.String(r.prober.settings.Interval),
							"timespan":         r.prober.settings.Timespan,
							"aggregation":      "",
						}

						// add resource tags as labels
						metricLabels = r.prober.AzureResourceTagManager.AddResourceTagsToPrometheusLabels(r.prober.ctx, metricLabels, resourceId)

						if len(dimensions) >= 2 || r.prober.settings.AlwaysIncludeDimensionName {
							// we have multiple dimensions or should always use the dimension name
							// add each dimension as dimensionXzy="foobar" label
							for dimensionName, dimensionValue := range dimensions {
								labelName := "dimension" + stringsCommon.UppercaseFirst(dimensionName)
								labelName = metricLabelNotAllowedChars.ReplaceAllString(labelName, "")
								metricLabels[labelName] = dimensionValue
							}
						} else {
							// we have only one dimension
							// add one dimension="foobar" label (backward compatibility)
							for _, dimensionValue := range dimensions {
								metricLabels["dimension"] = dimensionValue
							}
						}

						for _, timeseriesData := range timeseries.Data {
							if timeseriesData.Total != nil {
								metricLabels["aggregation"] = "total"
								channel <- r.buildMetric(
									metricLabels,
									*timeseriesData.Total,
								)
							}

							if timeseriesData.Minimum != nil {
								metricLabels["aggregation"] = "minimum"
								channel <- r.buildMetric(
									metricLabels,
									*timeseriesData.Minimum,
								)
							}

							if timeseriesData.Maximum != nil {
								metricLabels["aggregation"] = "maximum"
								channel <- r.buildMetric(
									metricLabels,
									*timeseriesData.Maximum,
								)
							}

							if timeseriesData.Average != nil {
								metricLabels["aggregation"] = "average"
								channel <- r.buildMetric(
									metricLabels,
									*timeseriesData.Average,
								)
							}

							if timeseriesData.Count != nil {
								metricLabels["aggregation"] = "count"
								channel <- r.buildMetric(
									metricLabels,
									*timeseriesData.Count,
								)
							}
						}
					}
				}
			}
		}
	}
}
