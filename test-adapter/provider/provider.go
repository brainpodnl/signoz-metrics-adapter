package provider

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
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

	"sigs.k8s.io/custom-metrics-apiserver/pkg/provider"
	"sigs.k8s.io/custom-metrics-apiserver/pkg/provider/defaults"
	"sigs.k8s.io/custom-metrics-apiserver/pkg/provider/helpers"
)

const (
	metricName  = "phpfpm_active_processes"
	podLabelKey = "k8s.pod.name"
)

type signozProvider struct {
	defaults.DefaultExternalMetricsProvider
	client dynamic.Interface
	mapper apimeta.RESTMapper
	signoz signozClient
}

var _ provider.MetricsProvider = &signozProvider{}

func NewSignozProvider(endpoint, apiKey string, client dynamic.Interface, mapper apimeta.RESTMapper) provider.MetricsProvider {
	return &signozProvider{
		client: client,
		mapper: mapper,
		signoz: signozClient{
			http:     http.Client{Timeout: 10 * time.Second},
			endpoint: endpoint,
			apiKey:   apiKey,
		},
	}
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
	if info.Metric != metricName {
		return nil, provider.NewMetricNotFoundForError(info.GroupResource, info.Metric, name.Name)
	}

	series, err := p.signoz.query(signozQueryOptions{
		Query: metricName,
		End:   time.Now(),
		Start: time.Now().Add(-30 * time.Minute),
		Step:  60,
	})
	if err != nil {
		return nil, err
	}

	var total float64
	var found bool
	for _, s := range series {

		for _, label := range s.Labels {
			fmt.Printf("label: %s\n", label)
		}

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
		Metric:          custom_metrics.MetricIdentifier{Name: metricName},
		Timestamp:       metav1.Now(),
		Value:           *resource.NewMilliQuantity(int64(total*1000), resource.DecimalSI),
	}, nil
}

func (p *signozProvider) GetMetricBySelector(_ context.Context, namespace string, selector labels.Selector, info provider.CustomMetricInfo, _ labels.Selector) (*custom_metrics.MetricValueList, error) {
	if info.Metric != metricName {
		return &custom_metrics.MetricValueList{}, nil
	}

	series, err := p.signoz.query(signozQueryOptions{
		Query: metricName,
		End:   time.Now(),
		Start: time.Now().Add(-30 * time.Minute),
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
			Metric:          custom_metrics.MetricIdentifier{Name: metricName},
			Timestamp:       metav1.Now(),
			Value:           *resource.NewMilliQuantity(int64(value*1000), resource.DecimalSI),
		})
	}

	return &custom_metrics.MetricValueList{Items: items}, nil
}

func (p *signozProvider) ListAllMetrics() []provider.CustomMetricInfo {
	return []provider.CustomMetricInfo{
		{
			GroupResource: schema.GroupResource{Group: "", Resource: "pods"},
			Metric:        metricName,
			Namespaced:    true,
		},
	}
}

func (p *signozProvider) GetExternalMetric(_ context.Context, _ string, _ labels.Selector, info provider.ExternalMetricInfo) (*external_metrics.ExternalMetricValueList, error) {
	return &external_metrics.ExternalMetricValueList{
		Items: []external_metrics.ExternalMetricValue{},
	}, nil
}

func (p *signozProvider) ListAllExternalMetrics() []provider.ExternalMetricInfo {
	return []provider.ExternalMetricInfo{
		{Metric: metricName},
	}
}
