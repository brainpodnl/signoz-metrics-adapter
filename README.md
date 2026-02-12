# signoz-metrics-adapter

A Kubernetes custom metrics adapter that queries [SigNoz](https://signoz.io/)
and exposes the results via the Custom Metrics and External Metrics APIs. This
allows the Horizontal Pod Autoscaler (HPA) to scale workloads based on metrics
collected by SigNoz.

## Configuration

The adapter connects to SigNoz using a URL and API key, provided through a
Kubernetes secret. The Helm values configure which metrics to expose and how to
query them.

### Helm Values

```yaml
signoz:
  existingSecret: "signoz-credentials"  # must contain `url` and `token` keys
  secretKeys:
    url: url
    token: token
  timeRangeMinutes: 5                   # lookback window for queries
  metrics: ['phpfpm_active_processes']  # metrics to expose to the HPA
  filterExpression: "deployment.environment = 'prod'"  # optional SigNoz filter
```

The secret must exist before deploying:

```sh
kubectl create secret generic signoz-credentials \
  --namespace signoz-metric-adapter \
  --from-literal=url=https://signoz.example.com \
  --from-literal=token=<your-api-key>
```

### All Values

| Key | Default | Description |
|-----|---------|-------------|
| `replicas` | `1` | Number of adapter replicas |
| `verbosity` | `2` | Log verbosity level |
| `signoz.existingSecret` | (required) | Name of the secret containing SigNoz credentials |
| `signoz.secretKeys.url` | `url` | Key in the secret for the SigNoz URL |
| `signoz.secretKeys.token` | `token` | Key in the secret for the API key |
| `signoz.timeRangeMinutes` | `5` | Lookback window in minutes |
| `signoz.metrics` | (required) | List of SigNoz metric names to expose |
| `signoz.filterExpression` | `""` | SigNoz filter expression |
| `serviceAccount.name` | release fullname | Service account name |
| `resources` | `{}` | Container resource requests/limits |

## Deployment

### Build and push with Steiger

The project uses [Steiger](https://github.com/brainhivenl/steiger) to build a
Nix-based OCI image and deploy it via Helm.

```sh
steiger run --repo ghcr.io/<your-org>
```

This builds the adapter image, pushes it to the registry, and deploys the Helm
chart using the configuration in `steiger.yml`.

## License

Apache License 2.0
