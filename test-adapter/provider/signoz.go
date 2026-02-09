package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"k8s.io/klog/v2"
)

type signozClient struct {
	http     http.Client
	endpoint string
	apiKey   string
}

type signozQueryOptions struct {
	Start, End time.Time
	Step       int64
	Query      string
}

func (client *signozClient) query(opts signozQueryOptions) ([]seriesValue, error) {
	u, err := url.Parse(client.endpoint + "/api/v1/query_range")
	if err != nil {
		return nil, fmt.Errorf("parsing endpoint URL: %w", err)
	}
	q := u.Query()
	q.Set("query", opts.Query)
	q.Set("start", strconv.FormatInt(opts.Start.Unix(), 10))
	q.Set("end", strconv.FormatInt(opts.End.Unix(), 10))
	q.Set("step", strconv.FormatInt(opts.Step, 10))

	u.RawQuery = q.Encode()

	klog.V(2).Infof("querying signoz: %s", u.String())
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("SIGNOZ-API-KEY", client.apiKey)

	resp, err := client.http.Do(req)
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

	return promResp.Series(), nil
}
