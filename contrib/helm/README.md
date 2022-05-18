# calert

![Version: 2.0.2](https://img.shields.io/badge/Version-2.0.2-informational?style=flat-square) ![AppVersion: 2.0.2](https://img.shields.io/badge/AppVersion-2.0.2-informational?style=flat-square)

A Helm chart for the calert which uses Alertmanager webhook receiver to receive alerts payload, and pushes this data to Google Chat webhook endpoint.

**Homepage:** <https://github.com/mr-karan/calert>

## Alertmanager Integration

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

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Karan Sharma | <karansharma1295@gmail.com> |  |

## Source Code

* <https://github.com/mr-karan/calert>

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` |  |
| args[0] | string | `"--config=/app/static/config.toml"` |  |
| configmap.app.address | string | `"0.0.0.0:6000"` |  |
| configmap.app.enable_request_logs | bool | `true` |  |
| configmap.app.log | string | `"info"` |  |
| configmap.app.server_timeout | string | `"5s"` |  |
| configmap.providers.dev_alerts.dry_run | bool | `false` |  |
| configmap.providers.dev_alerts.endpoint | string | `"https://chat.googleapis.com/v1/spaces/xxx/messages?key=key&token=token%3D"` |  |
| configmap.providers.dev_alerts.max_idle_conns | int | `50` |  |
| configmap.providers.dev_alerts.proxy_url | string | `"http://internal-squid-proxy.com:3128"` |  |
| configmap.providers.dev_alerts.template | string | `"static/message.tmpl"` |  |
| configmap.providers.dev_alerts.thread_ttl | string | `"12h"` |  |
| configmap.providers.dev_alerts.timeout | string | `"7s"` |  |
| configmap.providers.dev_alerts.type | string | `"google_chat"` |  |
| configmap.providers.prod_alerts.dry_run | bool | `false` |  |
| configmap.providers.prod_alerts.endpoint | string | `"https://chat.googleapis.com/v1/spaces/xxx/messages?key=key&token=token%3D"` |  |
| configmap.providers.prod_alerts.max_idle_conns | int | `50` |  |
| configmap.providers.prod_alerts.proxy_url | string | `"http://internal-squid-proxy.com:3128"` |  |
| configmap.providers.prod_alerts.template | string | `"static/message.tmpl"` |  |
| configmap.providers.prod_alerts.thread_ttl | string | `"12h"` |  |
| configmap.providers.prod_alerts.timeout | string | `"7s"` |  |
| configmap.providers.prod_alerts.type | string | `"google_chat"` |  |
| fullnameOverride | string | `""` |  |
| image.pullPolicy | string | `"Always"` |  |
| image.repository | string | `"ghcr.io/mr-karan/calert"` |  |
| image.tag | string | `"latest"` |  |
| ingress.enabled | bool | `false` |  |
| nameOverride | string | `""` |  |
| nodeSelector | object | `{}` |  |
| replicaCount | int | `1` |  |
| resources.limits.cpu | string | `"20m"` |  |
| resources.limits.memory | string | `"48Mi"` |  |
| resources.requests.cpu | string | `"5m"` |  |
| resources.requests.memory | string | `"24Mi"` |  |
| service.port | int | `6000` |  |
| service.type | string | `"ClusterIP"` |  |
| tolerations | list | `[]` |  |

----------------------------------------------
