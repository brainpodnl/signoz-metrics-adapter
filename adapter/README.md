# SigNoz Metrics Adapter

Kubernetes custom metrics API server that exposes SigNoz metrics for use
with Horizontal Pod Autoscalers (HPA).

The main entrypoint lives in [main.go](main.go), while the provider
implementations live in [provider/provider.go](provider/provider.go).
