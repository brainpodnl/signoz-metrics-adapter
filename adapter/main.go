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

	"k8s.io/component-base/logs"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"

	"github.com/brainpodnl/signoz-metrics-adapter/pkg/apiserver/metrics"
	basecmd "github.com/brainpodnl/signoz-metrics-adapter/pkg/cmd"
	signozprov "github.com/brainpodnl/signoz-metrics-adapter/adapter/provider"
)

type SignozAdapter struct {
	basecmd.AdapterBase
	SignozEndpoint         string
	SignozAPIKey           string
	SignozTimerangeMinutes int64
}

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()

	cmd := &SignozAdapter{}
	cmd.Name = "signoz-metrics-adapter"

	cmd.Flags().StringVar(&cmd.SignozEndpoint, "signoz-endpoint", "", "SigNoz query endpoint (e.g. https://signoz.example.com)")
	cmd.Flags().StringVar(&cmd.SignozAPIKey, "signoz-api-key", "", "SigNoz API key for authentication")
	cmd.Flags().Int64Var(&cmd.SignozTimerangeMinutes, "signoz-timerange-minutes", 5, "Time range in minutes to use for signoz queries")

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
		cmd.SignozAPIKey = os.Getenv("SIGNOZ_TOKEN")
		if cmd.SignozAPIKey == "" {
			klog.Fatal("--signoz-api-key or SIGNOZ_TOKEN is required")
		}
	}

	if os.Getenv("SIGNOZ_TIMERANGE_MINUTES") != "" {
		val, err := strconv.ParseInt(os.Getenv("SIGNOZ_TIMERANGE_MINUTES"), 10, 64)
		if err != nil {
			klog.Fatal("invalid value for SIGNOZ_TIMERANGE_MINUTES")
		}
		cmd.SignozTimerangeMinutes = val
	}

	dynClient, err := cmd.DynamicClient()
	if err != nil {
		klog.Fatalf("unable to construct dynamic client: %v", err)
	}
	mapper, err := cmd.RESTMapper()
	if err != nil {
		klog.Fatalf("unable to construct REST mapper: %v", err)
	}

	provider := signozprov.NewSignozProvider(cmd.SignozEndpoint, cmd.SignozAPIKey, cmd.SignozTimerangeMinutes, dynClient, mapper)
	cmd.WithCustomMetrics(provider)
	cmd.WithExternalMetrics(provider)

	if err := metrics.RegisterMetrics(legacyregistry.Register); err != nil {
		klog.Fatalf("unable to register metrics: %v", err)
	}

	klog.Infof("starting signoz metrics adapter, endpoint=%s", cmd.SignozEndpoint)

	if err := cmd.Run(context.Background()); err != nil {
		klog.Fatalf("unable to run custom metrics adapter: %v", err)
	}
}
