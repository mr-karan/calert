# Group threading and shared state

Group threading sends each Alertmanager `GroupKey` to one stable Google Chat
thread. A recurring payload for the same group reuses that thread. `thread_ttl`
is only the retention period for dedup state. It does not rotate thread keys.

## When to use what

| Deployment | `threading_mode` | Redis |
|------------|------------------|-------|
| Single calert instance | `"group"` | not needed |
| Active-active instances | `"group"` | optional shared state |
| Legacy per-alert threads | `"alert"` (default) | n/a |

Group mode uses a SHA-256 key derived from the receiver room and `GroupKey`.
Redis keys use the same room-scoped digest, so raw GroupKeys never appear in
Redis and two rooms cannot share state.

## Delivery and deduplication

State is inspected before rendering, but written only after every Google Chat
message is delivered. A render or send failure therefore never suppresses a
retry. Sequential identical delivered payloads within `dedup_window` are
suppressed. Concurrent replicas can both deliver the same payload because
there is intentionally no pre-delivery distributed claim.

If Redis is unavailable, calert posts without dedup state. If Redis fails
while saving state after delivery, the notification remains successful and
`group_dedup_store_errors_total` increments.

## Configuration

Group mode requires `send_resolved: true` on the Alertmanager receiver. Use a
`group_by` that does not include `instance`, so related alerts share a group.
When group mode is selected with the normal default template,
`static/message-group.tmpl` is selected automatically. Keep an explicit custom
template path to preserve a custom template.

```toml
[providers.prod_alerts]
type = "google_chat"
endpoint = "https://chat.googleapis.com/v1/spaces/xxx/messages?key=...&token=..."
threading_mode = "group"
thread_ttl = "12h"              # state retention, not thread rotation
dedup_window = "2m"
max_alerts_per_message = 10

# Optional shared state for active-active deployments.
[providers.prod_alerts.redis]
address = "redis:6379"
password = ""
db = 0
key_prefix = "calert:"
```

`TruncatedAlerts` reported by Alertmanager is surfaced in the group card as an
upstream truncation notice. It is not added to the card's firing or resolved
counters because those counters only describe the payload that calert received.

## Metrics

- `alerts_deduplicated_total` counts sequential duplicate payloads suppressed
  after a successful prior delivery.
- `group_dedup_store_errors_total` counts state-store inspection or save
  failures. Non-zero means duplicate delivery is possible.
