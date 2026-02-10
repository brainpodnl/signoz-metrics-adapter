package provider

import (
	"context"
	"net/http"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"k8s.io/metrics/pkg/apis/custom_metrics"
	"k8s.io/metrics/pkg/apis/external_metrics"

	"github.com/brainpodnl/signoz-metrics-adapter/pkg/provider"
	"github.com/brainpodnl/signoz-metrics-adapter/pkg/provider/defaults"
	"github.com/brainpodnl/signoz-metrics-adapter/pkg/provider/helpers"
)

const podLabelKey = "k8s.pod.name"

type seriesValue struct {
	Labels map[string]string
	Value  float64
}

func (resp *SignozQueryRangeResponse) Series() []seriesValue {
	var results []seriesValue
	for _, qr := range resp.Data.Data.Results {
		for _, agg := range qr.Aggregations {
			for _, s := range agg.Series {
				if len(s.Values) == 0 {
					continue
				}
				last := s.Values[len(s.Values)-1]
				results = append(results, seriesValue{
					Labels: s.LabelMap(),
					Value:  last.Value,
				})
			}
		}
	}
	return results
}

type signozProvider struct {
	defaults.DefaultExternalMetricsProvider
	client           dynamic.Interface
	mapper           apimeta.RESTMapper
	timeRangeMinutes int64
	signoz           SignozClient
	metrics          []string
	filterExpression string
}

var _ provider.MetricsProvider = &signozProvider{}

func NewSignozProvider(endpoint, apiKey string, timeRangeMinutes int64, metrics []string, filterExpression string, client dynamic.Interface, mapper apimeta.RESTMapper) provider.MetricsProvider {
	return &signozProvider{
		client:           client,
		mapper:           mapper,
		timeRangeMinutes: timeRangeMinutes,
		metrics:          metrics,
		filterExpression: filterExpression,
		signoz: SignozClient{
			Http:     http.Client{Timeout: 10 * time.Second},
			Endpoint: endpoint,
			ApiKey:   apiKey,
		},
	}
}

func (p *signozProvider) isAllowedMetric(name string) bool {
	for _, m := range p.metrics {
		if m == name {
			return true
		}
	}
	return false
}

func (p *signozProvider) buildQuery(metricName string) SignozQueryRangeOptions {
	query := SignozQuery{
		Type: "builder_query",
		Spec: SignozQuerySpec{
			Name:         "A",
			Signal:       "metrics",
			StepInterval: 60,
			Aggregations: []SignozMetricAggregation{
				{
					MetricName:       metricName,
					TimeAggregation:  "avg",
					SpaceAggregation: "avg",
				},
			},
			GroupBy: []SignozQueryGroupBy{
				{
					Name:          podLabelKey,
					FieldDataType: "string",
					FieldContext:  "resource",
				},
			},
		},
	}

	if p.filterExpression != "" {
		query.Spec.Filter = &SignozQueryFilter{Expression: p.filterExpression}
	}

	return SignozQueryRangeOptions{
		RequestType: "time_series",
		Start:       time.Now().Add(-time.Duration(p.timeRangeMinutes) * time.Minute).UnixMilli(),
		End:         time.Now().UnixMilli(),
		CompositeQuery: SignozCompositeQuery{
			Queries: []SignozQuery{query},
		},
	}
}

func (p *signozProvider) GetMetricByName(_ context.Context, name types.NamespacedName, info provider.CustomMetricInfo, _ labels.Selector) (*custom_metrics.MetricValue, error) {
	if !p.isAllowedMetric(info.Metric) {
		return nil, provider.NewMetricNotFoundForError(info.GroupResource, info.Metric, name.Name)
	}

	queryResponse, err := p.signoz.Query(p.buildQuery(info.Metric))
	if err != nil {
		return nil, err
	}

	series := queryResponse.Series()
	var total float64
	var found bool

	for _, s := range series {
		if s.Labels[podLabelKey] == name.Name {
			total += s.Value
			found = true
		}
	}
	if !found {
		for _, s := range series {
			total += s.Value
		}
	}

	objRef, err := helpers.ReferenceFor(p.mapper, name, info)
	if err != nil {
		return nil, err
	}

	return &custom_metrics.MetricValue{
		DescribedObject: objRef,
		Metric:          custom_metrics.MetricIdentifier{Name: info.Metric},
		Timestamp:       metav1.Now(),
		Value:           *resource.NewMilliQuantity(int64(total*1000), resource.DecimalSI),
	}, nil
}

func (p *signozProvider) GetMetricBySelector(_ context.Context, namespace string, selector labels.Selector, info provider.CustomMetricInfo, _ labels.Selector) (*custom_metrics.MetricValueList, error) {
	if !p.isAllowedMetric(info.Metric) {
		return &custom_metrics.MetricValueList{}, nil
	}

	queryResponse, err := p.signoz.Query(p.buildQuery(info.Metric))
	if err != nil {
		return nil, err
	}

	series := queryResponse.Series()

	podNames, err := helpers.ListObjectNames(p.mapper, p.client, namespace, selector, info)
	if err != nil {
		return nil, err
	}

	klog.V(2).Infof("matched %d pods, got %d series from signoz", len(podNames), len(series))

	byPod := map[string]float64{}
	for _, s := range series {
		if pod, ok := s.Labels[podLabelKey]; ok {
			byPod[pod] += s.Value
		}
	}

	var items []custom_metrics.MetricValue
	for _, podName := range podNames {
		value, ok := byPod[podName]
		if !ok {
			klog.V(2).Infof("no signoz series for pod %s, skipping", podName)
			continue
		}

		name := types.NamespacedName{Name: podName, Namespace: namespace}
		objRef, err := helpers.ReferenceFor(p.mapper, name, info)
		if err != nil {
			return nil, err
		}

		items = append(items, custom_metrics.MetricValue{
			DescribedObject: objRef,
			Metric:          custom_metrics.MetricIdentifier{Name: info.Metric},
			Timestamp:       metav1.Now(),
			Value:           *resource.NewMilliQuantity(int64(value*1000), resource.DecimalSI),
		})
	}

	return &custom_metrics.MetricValueList{Items: items}, nil
}

func (p *signozProvider) ListAllMetrics() []provider.CustomMetricInfo {
	var infos []provider.CustomMetricInfo
	for _, m := range p.metrics {
		infos = append(infos, provider.CustomMetricInfo{
			GroupResource: schema.GroupResource{Group: "", Resource: "pods"},
			Metric:        m,
			Namespaced:    true,
		})
	}
	return infos
}

func (p *signozProvider) GetExternalMetric(_ context.Context, _ string, _ labels.Selector, info provider.ExternalMetricInfo) (*external_metrics.ExternalMetricValueList, error) {
	return &external_metrics.ExternalMetricValueList{
		Items: []external_metrics.ExternalMetricValue{},
	}, nil
}

func (p *signozProvider) ListAllExternalMetrics() []provider.ExternalMetricInfo {
	var infos []provider.ExternalMetricInfo
	for _, m := range p.metrics {
		infos = append(infos, provider.ExternalMetricInfo{Metric: m})
	}
	return infos
}
