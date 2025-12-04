# calert

![Version: 2.0.3](https://img.shields.io/badge/Version-2.0.3-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 2.0.2](https://img.shields.io/badge/AppVersion-2.0.2-informational?style=flat-square)

A Helm chart for the calert which uses Alertmanager webhook receiver to receive alerts payload, and pushes this data to Google Chat webhook endpoint.

## Source Code

* <https://github.com/mr-karan/calert>

## Installation

### Add Helm repository

```bash
helm repo add calert https://mr-karan.github.io/calert/charts
helm repo update
```

### Install the chart

```bash
helm install calert calert/calert
```

### Install with custom values

```bash
helm install calert calert/calert -f values.yaml
```

## Alertmanager Integration

The Alertmanager helm chart can be found [here](https://github.com/prometheus-community/helm-charts/tree/main/charts/alertmanager). You can refer to the following config block to route webhook alerts to `calert`:

```yaml
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

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity rules for pod assignment |
| app | object | See below | Application configuration block |
| app.address | string | `"0.0.0.0:6000"` | Address of the HTTP Server |
| app.enable_request_logs | bool | `true` | Whether to log incoming HTTP requests or not |
| app.log | string | `"info"` | Log level. Use `debug` to enable verbose logging |
| app.server_timeout | string | `"60s"` | Server timeout for HTTP requests |
| args | list | `["--config=/app/config.toml"]` | Arguments to pass to the calert binary |
| fullnameOverride | string | `""` | Override the full name of the chart |
| image | object | `{"pullPolicy":"Always","repository":"ghcr.io/mr-karan/calert","tag":"{{ .Chart.AppVersion }}"}` | Image configuration |
| image.pullPolicy | string | `"Always"` | Image pull policy |
| image.repository | string | `"ghcr.io/mr-karan/calert"` | Image repository |
| image.tag | string | `"{{ .Chart.AppVersion }}"` | Image tag (defaults to Chart appVersion) |
| ingress | object | `{"annotations":{},"enabled":false,"hosts":[{"host":"calert.example.com","paths":[{"path":"/","pathType":"Prefix"}]}],"ingressClassName":"","tls":[]}` | Ingress configuration |
| ingress.annotations | object | `{}` | Annotations for the Ingress resource |
| ingress.enabled | bool | `false` | Enable ingress |
| ingress.hosts | list | `[{"host":"calert.example.com","paths":[{"path":"/","pathType":"Prefix"}]}]` | Hosts configuration |
| ingress.ingressClassName | string | `""` | Ingress class name (e.g., nginx, traefik, alb) |
| ingress.tls | list | `[]` | TLS configuration |
| livenessProbe | object | `{"periodSeconds":60,"timeoutSeconds":3}` | Liveness probe configuration |
| livenessProbe.periodSeconds | int | `60` | How often to perform the probe (seconds) |
| livenessProbe.timeoutSeconds | int | `3` | Probe timeout (seconds) |
| nameOverride | string | `""` | Override the name of the chart |
| nodeSelector | object | `{}` | Node selector for pod assignment |
| podAnnotations | object | `{}` | Annotations to add to the pod |
| podSecurityContext | object | See below | Pod security context configuration |
| podSecurityContext.enabled | bool | `false` | Enable pod security context |
| podSecurityContext.runAsGroup | int | `1001` | Group ID to run as |
| podSecurityContext.runAsNonRoot | bool | `true` | Run as non-root user |
| podSecurityContext.runAsUser | int | `1001` | User ID to run as |
| podSecurityContext.seccompProfile | object | `{"type":"RuntimeDefault"}` | Seccomp profile configuration |
| podSecurityContext.windowsOptions | object | `{"hostProcess":false}` | Windows-specific options |
| priorityClassName | string | `""` | Priority class name for pod scheduling |
| providers | object | `{}` (no providers configured) | Provider configuration for routing alerts to different Google Chat rooms. See [config.sample.toml](https://github.com/mr-karan/calert/blob/main/config.sample.toml) for more details |
| readinessProbe | object | `{"periodSeconds":60,"timeoutSeconds":3}` | Readiness probe configuration |
| readinessProbe.periodSeconds | int | `60` | How often to perform the probe (seconds) |
| readinessProbe.timeoutSeconds | int | `3` | Probe timeout (seconds) |
| replicaCount | int | `1` | Number of pod replicas |
| resources | object | `{"limits":{"cpu":"20m","memory":"48Mi"},"requests":{"cpu":"5m","memory":"24Mi"}}` | Resource limits and requests |
| resources.limits | object | `{"cpu":"20m","memory":"48Mi"}` | Resource limits |
| resources.requests | object | `{"cpu":"5m","memory":"24Mi"}` | Resource requests |
| securityContext | object | See below | Container security context configuration |
| securityContext.allowPrivilegeEscalation | bool | `false` | Allow privilege escalation |
| securityContext.capabilities | object | `{"add":["NET_BIND_SERVICE"],"drop":["ALL"]}` | Linux capabilities |
| securityContext.enabled | bool | `false` | Enable container security context |
| securityContext.privileged | bool | `false` | Run container in privileged mode |
| securityContext.readOnlyRootFilesystem | bool | `true` | Mount root filesystem as read-only |
| securityContext.runAsGroup | int | `1001` | Group ID to run as |
| securityContext.runAsNonRoot | bool | `true` | Run as non-root user |
| securityContext.runAsUser | int | `1001` | User ID to run as |
| securityContext.seccompProfile | object | `{"type":"RuntimeDefault"}` | Seccomp profile configuration |
| securityContext.windowsOptions | object | `{"hostProcess":false}` | Windows-specific options |
| service | object | `{"port":6000,"type":"ClusterIP"}` | Service configuration |
| service.port | int | `6000` | Service port |
| service.type | string | `"ClusterIP"` | Service type |
| startupProbe | object | `{"periodSeconds":10,"timeoutSeconds":3}` | Startup probe configuration |
| startupProbe.periodSeconds | int | `10` | How often to perform the probe (seconds) |
| startupProbe.timeoutSeconds | int | `3` | Probe timeout (seconds) |
| templates | object | `{}` (no custom templates) | Custom message templates for alert formatting. Templates use Go templating syntax |
| tolerations | list | `[]` | Tolerations for pod assignment |
| topologySpreadConstraints | list | `[]` | Topology spread constraints for pod assignment |

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Karan Sharma | <hello@mrkaran.dev> | <https://github.com/mr-karan> |
