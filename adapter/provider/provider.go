package provider

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
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

type signozProvider struct {
	defaults.DefaultExternalMetricsProvider
	client           dynamic.Interface
	mapper           apimeta.RESTMapper
	timeRangeMinutes int64
	signoz           signozClient
	metrics          []string
	labelFilters     map[string]string
}

var _ provider.MetricsProvider = &signozProvider{}

func NewSignozProvider(endpoint, apiKey string, timeRangeMinutes int64, metrics []string, labelFilters map[string]string, client dynamic.Interface, mapper apimeta.RESTMapper) provider.MetricsProvider {
	return &signozProvider{
		client:           client,
		mapper:           mapper,
		timeRangeMinutes: timeRangeMinutes,
		metrics:          metrics,
		labelFilters:     labelFilters,
		signoz: signozClient{
			http:     http.Client{Timeout: 10 * time.Second},
			endpoint: endpoint,
			apiKey:   apiKey,
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

func (p *signozProvider) buildQuery(metric string) string {
	if len(p.labelFilters) == 0 {
		return metric
	}
	keys := make([]string, 0, len(p.labelFilters))
	for k := range p.labelFilters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var selectors []string
	for _, k := range keys {
		selectors = append(selectors, k+`="`+p.labelFilters[k]+`"`)
	}
	return metric + "{" + strings.Join(selectors, ",") + "}"
}

type promResult struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value"`
	Values [][]interface{}   `json:"values"`
}

type promData struct {
	ResultType string       `json:"resultType"`
	Result     []promResult `json:"result"`
}

type promResponse struct {
	Status string   `json:"status"`
	Data   promData `json:"data"`
}

type seriesValue struct {
	Labels map[string]string
	Value  float64
}

func (promResp *promResponse) Series() []seriesValue {
	var results []seriesValue
	for _, r := range promResp.Data.Result {
		var raw string
		if len(r.Values) > 0 {
			last := r.Values[len(r.Values)-1]
			if len(last) >= 2 {
				raw, _ = last[1].(string)
			}
		} else if len(r.Value) >= 2 {
			raw, _ = r.Value[1].(string)
		}
		if raw == "" {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			klog.Warningf("skipping non-numeric value %q: %v", raw, err)
			continue
		}
		results = append(results, seriesValue{Labels: r.Metric, Value: v})
	}
	return results
}

func (p *signozProvider) GetMetricByName(_ context.Context, name types.NamespacedName, info provider.CustomMetricInfo, _ labels.Selector) (*custom_metrics.MetricValue, error) {
	if !p.isAllowedMetric(info.Metric) {
		return nil, provider.NewMetricNotFoundForError(info.GroupResource, info.Metric, name.Name)
	}

	series, err := p.signoz.query(signozQueryOptions{
		Query: p.buildQuery(info.Metric),
		End:   time.Now(),
		Start: time.Now().Add(-time.Duration(p.timeRangeMinutes) * time.Minute),
		Step:  60,
	})
	if err != nil {
		return nil, err
	}

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

	series, err := p.signoz.query(signozQueryOptions{
		Query: p.buildQuery(info.Metric),
		End:   time.Now(),
		Start: time.Now().Add(-time.Duration(p.timeRangeMinutes) * time.Minute),
		Step:  60,
	})
	if err != nil {
		return nil, err
	}

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
