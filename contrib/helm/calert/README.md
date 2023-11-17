# calert

![Version: 2.0.3](https://img.shields.io/badge/Version-2.0.3-informational?style=flat-square) ![AppVersion: 2.0.2](https://img.shields.io/badge/AppVersion-2.0.2-informational?style=flat-square)

A Helm chart for the calert which uses Alertmanager webhook receiver to receive alerts payload, and pushes this data to Google Chat webhook endpoint.

**Source Code:** <https://github.com/mr-karan/calert>

# Alertmanager Integration

The Alertmanager helm chart can be found [here](https://github.com/prometheus-community/helm-charts/tree/main/charts/alertmanager). You can refer to the following config block to route webhook alerts to `calert`:

```yml
config:
  receivers:
    - name: 'calert'
      webhook_configs:
      - url: 'http://calert:6000/dispatch'

  route:
    receiver: 'calert'
    group_wait: 30s
    group_interval: 60s
    repeat_interval: 3h
    group_by: ['alertname']
```

# Parameters

The following tables lists the configurable parameters of the chart and their default values.

Change the values according to the need of the environment in ``contrib/helm/calert/values.yaml`` file.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| app.address | string | `"0.0.0.0:6000"` |  |
| app.server_timeout | string | `"60s"` |  |
| app.enable_request_logs | bool | `true` |  |
| app.log | string | `"info"` |  |
| providers | object | `{}` |  |
| templates | object | `{}` |  |
| replicaCount | int | `1` |  |
| image.repository | string | `"ghcr.io/mr-karan/calert"` |  |
| image.tag | string | `"v2.0.2"` |  |
| image.pullPolicy | string | `"Always"` |  |
| args[0] | string | `"--config=/app/static/config.toml"` |  |
| nameOverride | string | `""` |  |
| fullnameOverride | string | `""` |  |
| service.type | string | `"ClusterIP"` |  |
| service.port | int | `6000` |  |
| ingress.enabled | bool | `false` |  |
| resources.limits.cpu | string | `"20m"` |  |
| resources.limits.memory | string | `"48Mi"` |  |
| resources.requests.cpu | string | `"5m"` |  |
| resources.requests.memory | string | `"24Mi"` |  |
| priorityClassName | string | `""` |  |
| nodeSelector | object | `{}` |  |
| tolerations | list | `[]` |  |
| affinity | object | `{}` |  |
| topologySpreadConstraints | list | `[]` |  |
| podAnnotations | object | `{}` |  |
