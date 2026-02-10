/*
Copyright 2018 The Kubernetes Authors.

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

package main

import (
	"context"
	"os"
	"strconv"
	"strings"

	"k8s.io/component-base/logs"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"

	signozprov "github.com/brainpodnl/signoz-metrics-adapter/adapter/provider"
	"github.com/brainpodnl/signoz-metrics-adapter/pkg/apiserver/metrics"
	basecmd "github.com/brainpodnl/signoz-metrics-adapter/pkg/cmd"
)

type SignozAdapter struct {
	basecmd.AdapterBase
	SignozEndpoint         string
	SignozAPIKey           string
	SignozTimerangeMinutes int64
	SignozMetrics          string
	SignozLabelFilters     string
}

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()

	cmd := &SignozAdapter{}
	cmd.Name = "signoz-metrics-adapter"

	cmd.Flags().StringVar(&cmd.SignozEndpoint, "signoz-endpoint", "", "SigNoz query endpoint (e.g. https://signoz.example.com)")
	cmd.Flags().StringVar(&cmd.SignozAPIKey, "signoz-api-key", "", "SigNoz API key for authentication")
	cmd.Flags().Int64Var(&cmd.SignozTimerangeMinutes, "signoz-timerange-minutes", 5, "Time range in minutes to use for signoz queries")
	cmd.Flags().StringVar(&cmd.SignozMetrics, "signoz-metrics", "", "Comma-separated list of metric names to expose")
	cmd.Flags().StringVar(&cmd.SignozLabelFilters, "signoz-label-filters", "", "Comma-separated label filters appended to every query (e.g. deployment.environment=prod,service.name=myapp)")

	logs.AddFlags(cmd.Flags())
	if err := cmd.Flags().Parse(os.Args); err != nil {
		klog.Fatalf("unable to parse flags: %v", err)
	}

	if cmd.SignozEndpoint == "" {
		cmd.SignozEndpoint = os.Getenv("SIGNOZ_URL")
		if cmd.SignozEndpoint == "" {
			klog.Fatal("--signoz-endpoint or SIGNOZ_URL is required")
		}
	}

	if cmd.SignozAPIKey == "" {
		cmd.SignozAPIKey = os.Getenv("SIGNOZ_API_KEY")
		if cmd.SignozAPIKey == "" {
			klog.Fatal("--signoz-api-key or SIGNOZ_API_KEY is required")
		}
	}

	if os.Getenv("SIGNOZ_TIMERANGE_MINUTES") != "" {
		val, err := strconv.ParseInt(os.Getenv("SIGNOZ_TIMERANGE_MINUTES"), 10, 64)
		if err != nil {
			klog.Fatal("invalid value for SIGNOZ_TIMERANGE_MINUTES")
		}
		cmd.SignozTimerangeMinutes = val
	}

	if cmd.SignozMetrics == "" {
		cmd.SignozMetrics = os.Getenv("SIGNOZ_METRICS")
		if cmd.SignozMetrics == "" {
			klog.Fatal("--signoz-metrics or SIGNOZ_METRICS is required")
		}
	}

	if cmd.SignozLabelFilters == "" {
		cmd.SignozLabelFilters = os.Getenv("SIGNOZ_LABEL_FILTERS")
	}

	metricsSlice := strings.Split(cmd.SignozMetrics, ",")
	for i := range metricsSlice {
		metricsSlice[i] = strings.TrimSpace(metricsSlice[i])
	}

	labelFilters := map[string]string{}
	if cmd.SignozLabelFilters != "" {
		for _, pair := range strings.Split(cmd.SignozLabelFilters, ",") {
			parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(parts) != 2 {
				klog.Fatalf("invalid label filter %q: expected key=value", pair)
			}
			labelFilters[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	dynClient, err := cmd.DynamicClient()
	if err != nil {
		klog.Fatalf("unable to construct dynamic client: %v", err)
	}
	mapper, err := cmd.RESTMapper()
	if err != nil {
		klog.Fatalf("unable to construct REST mapper: %v", err)
	}

	provider := signozprov.NewSignozProvider(cmd.SignozEndpoint, cmd.SignozAPIKey, cmd.SignozTimerangeMinutes, metricsSlice, labelFilters, dynClient, mapper)
	cmd.WithCustomMetrics(provider)
	cmd.WithExternalMetrics(provider)

	if err := metrics.RegisterMetrics(legacyregistry.Register); err != nil {
		klog.Fatalf("unable to register metrics: %v", err)
	}

	klog.Infof("starting signoz metrics adapter, endpoint=%s, metrics=%v", cmd.SignozEndpoint, metricsSlice)

	if err := cmd.Run(context.Background()); err != nil {
		klog.Fatalf("unable to run custom metrics adapter: %v", err)
	}
}
