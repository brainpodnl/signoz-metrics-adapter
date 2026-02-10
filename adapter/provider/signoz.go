package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type SignozClient struct {
	Http     http.Client
	Endpoint string
	ApiKey   string
}

// not suitable when querying logs/traces
type SignozMetricAggregation struct {
	MetricName       string `json:"metricName"`
	Temporality      string `json:"temporality,omitempty"` // Cumulative, Delta, Unspecified
	TimeAggregation  string `json:"timeAggregation"`       // rate, latest, sum, avg, min, max, count, count_distinct, increase
	SpaceAggregation string `json:"spaceAggregation"`      // sum, avg, min, max, count, p50, p75, p90, p95, p99
	ReduceTo         string `json:"reduceTo,omitempty"`    // last, sum, avg, min, max, count, median
}

type SignozQueryGroupBy struct {
	Name          string `json:"name"`
	FieldDataType string `json:"fieldDataType"` // string, int64, float64, bool, array(string), array(int64), array(float64), array(bool)
	FieldContext  string `json:"fieldContext"`  // resource, attribute, scope, span, log
}

type SignozQueryFilter struct {
	Expression string `json:"expression,omitempty"`
}

type SignozQuerySpec struct {
	Name         string                    `json:"name"`
	Signal       string                    `json:"signal"`
	StepInterval int64                    `json:"stepInterval"`
	Disabled     *bool                     `json:"disabled,omitempty"`
	Aggregations []SignozMetricAggregation `json:"aggregations"`
	GroupBy      []SignozQueryGroupBy      `json:"groupBy,omitempty"`
	Filter       *SignozQueryFilter        `json:"filter,omitempty"`
	Having       *SignozQueryFilter        `json:"having,omitempty"`
	Limit        int                       `json:"limit,omitempty"`
	Offset       int                       `json:"offset,omitempty"`
}

type SignozQuery struct {
	Type string          `json:"type"` // builder_query, builder_formula, builder_trace_operator, clickhouse_sql, promql
	Spec SignozQuerySpec `json:"spec"`
}

type SignozCompositeQuery struct {
	Queries []SignozQuery `json:"queries"`
}

type SignozQueryRangeOptions struct {
	Start          int64                `json:"start"`
	End            int64                `json:"end"`
	RequestType    string               `json:"requestType"` // time_series, scalar, raw, trace
	CompositeQuery SignozCompositeQuery `json:"compositeQuery"`
}

type SignozQueryRangeResponse struct {
	Status string                          `json:"status"`
	Data   SignozQueryRangeResponseWrapper `json:"data"`
}

type SignozQueryRangeResponseWrapper struct {
	Type string                       `json:"type"` // time_series, scalar, raw, trace, distribution
	Meta SignozResponseMeta           `json:"meta"`
	Data SignozQueryRangeResponseData `json:"data"`
}

type SignozQueryRangeResponseData struct {
	Results  []SignozQueryResult     `json:"results"`
	Warning  *SignozResponseWarning  `json:"warning,omitempty"`
	Warnings []SignozResponseWarning `json:"warnings,omitempty"`
}

type SignozQueryResult struct {
	QueryName        string                    `json:"queryName"`
	Aggregations     []SignozResultAggregation `json:"aggregations"`
	Meta             map[string]any            `json:"meta,omitempty"`
	PredictedSeries  []SignozResultSeries      `json:"predictedSeries,omitempty"`
	UpperBoundSeries []SignozResultSeries      `json:"upperBoundSeries,omitempty"`
	LowerBoundSeries []SignozResultSeries      `json:"lowerBoundSeries,omitempty"`
	AnomalyScores    []SignozResultSeries      `json:"anomalyScores,omitempty"`
}

type SignozResultAggregation struct {
	Index  int                  `json:"index"`
	Alias  string               `json:"alias"`
	Meta   map[string]any       `json:"meta,omitempty"`
	Series []SignozResultSeries `json:"series"`
}

type SignozLabelKey struct {
	Name string `json:"name"`
}

type SignozLabel struct {
	Key   SignozLabelKey `json:"key"`
	Value any            `json:"value"`
}

type SignozResultSeries struct {
	Labels []SignozLabel      `json:"labels,omitempty"`
	Values []SignozSeriesValue `json:"values"`
}

func (s *SignozResultSeries) LabelMap() map[string]string {
	m := make(map[string]string, len(s.Labels))
	for _, l := range s.Labels {
		m[l.Key.Name] = fmt.Sprintf("%v", l.Value)
	}
	return m
}

type SignozSeriesValue struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

type SignozResponseMeta struct {
	RowsScanned  int64 `json:"rowsScanned"`
	BytesScanned int64 `json:"bytesScanned"`
	DurationMs   int64 `json:"durationMs"`
}

type SignozResponseWarning struct {
	Message string `json:"message"`
	URL     string `json:"url,omitempty"`
}

func (client *SignozClient) Query(query SignozQueryRangeOptions) (*SignozQueryRangeResponse, error) {
	body, err := json.Marshal(&query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	endpointUrl := client.Endpoint + "/api/v5/query_range"
	request, err := http.NewRequest("POST", endpointUrl, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	request.Header.Set("Signoz-Api-Key", client.ApiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("failed post signoz query: %w", err)
	}
	defer response.Body.Close()

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if response.StatusCode != 200 {
		return nil, fmt.Errorf("signoz returned non-OK status code: %d, body: %s", response.StatusCode, string(bodyBytes))
	}

	var responseData SignozQueryRangeResponse
	if err := json.Unmarshal(bodyBytes, &responseData); err != nil {
		return nil, fmt.Errorf("failed to decode response body: %w", err)
	}

	return &responseData, nil
}
