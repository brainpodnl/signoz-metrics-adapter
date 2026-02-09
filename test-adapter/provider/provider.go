/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	client   dynamic.Interface
	mapper   apimeta.RESTMapper
	endpoint string
	apiKey   string
	http     http.Client
}

var _ provider.MetricsProvider = &signozProvider{}

func NewSignozProvider(endpoint, apiKey string, client dynamic.Interface, mapper apimeta.RESTMapper) provider.MetricsProvider {
	return &signozProvider{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   client,
		mapper:   mapper,
		http:     http.Client{Timeout: 10 * time.Second},
	}
}

type promResponse struct {
	Status string   `json:"status"`
	Data   promData `json:"data"`
}

type promData struct {
	ResultType string       `json:"resultType"`
	Result     []promResult `json:"result"`
}

type promResult struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value"`
	Values [][]interface{}   `json:"values"`
}

type seriesValue struct {
	Labels map[string]string
	Value  float64
}

func (p *signozProvider) querySignoz() ([]seriesValue, error) {
	now := time.Now()
	u, err := url.Parse(p.endpoint + "/api/v1/query_range")
	if err != nil {
		return nil, fmt.Errorf("parsing endpoint URL: %w", err)
	}
	q := u.Query()
	q.Set("query", metricName)
	// Metrics for DSDEurope are constantly behind about 15m in signoz. idk how to fix this
	q.Set("start", strconv.FormatInt(now.Add(-30*time.Minute).Unix(), 10))
	q.Set("end", strconv.FormatInt(now.Unix(), 10))
	q.Set("step", "60")
	u.RawQuery = q.Encode()

	klog.V(2).Infof("querying signoz: %s", u.String())

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if p.apiKey != "" {
		req.Header.Set("SIGNOZ-API-KEY", p.apiKey)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying signoz: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	klog.V(2).Infof("signoz response (%d): %s", resp.StatusCode, string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signoz returned %d: %s", resp.StatusCode, string(body))
	}

	var promResp promResponse
	if err := json.Unmarshal(body, &promResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if promResp.Status != "success" {
		return nil, fmt.Errorf("query failed with status: %s", promResp.Status)
	}

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

	return results, nil
}

func (p *signozProvider) GetMetricByName(_ context.Context, name types.NamespacedName, info provider.CustomMetricInfo, _ labels.Selector) (*custom_metrics.MetricValue, error) {
	if info.Metric != metricName {
		return nil, provider.NewMetricNotFoundForError(info.GroupResource, info.Metric, name.Name)
	}

	series, err := p.querySignoz()
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

	series, err := p.querySignoz()
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
	if info.Metric != metricName {
		return &external_metrics.ExternalMetricValueList{}, nil
	}

	series, err := p.querySignoz()
	if err != nil {
		return nil, fmt.Errorf("querying signoz: %w", err)
	}

	var total float64
	for _, s := range series {
		total += s.Value
	}

	return &external_metrics.ExternalMetricValueList{
		Items: []external_metrics.ExternalMetricValue{
			{
				MetricName:   metricName,
				MetricLabels: map[string]string{},
				Timestamp:    metav1.Now(),
				Value:        *resource.NewMilliQuantity(int64(total*1000), resource.DecimalSI),
			},
		},
	}, nil
}

func (p *signozProvider) ListAllExternalMetrics() []provider.ExternalMetricInfo {
	return []provider.ExternalMetricInfo{
		{Metric: metricName},
	}
}
