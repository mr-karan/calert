# Group threading and shared state

Group threading sends each incident of an Alertmanager group to its own Google
Chat thread. Every notification of one incident lands in that thread. When the
same group fires again after all its alerts have resolved, calert starts a new
thread.

## When to use what

| Deployment | `threading_mode` | Redis |
|------------|------------------|-------|
| Single calert instance | `"group"` | not needed |
| Active-active instances | `"group"` | optional shared state |
| Legacy per-alert threads | `"alert"` (default) | n/a |

The thread key is a SHA-256 digest of the receiver room, the `GroupKey` and
the incident anchor. Redis keys are a digest of the room and `GroupKey`, so raw
GroupKeys never appear in Redis and two rooms cannot share state.

## Incidents and threads

Alertmanager keeps an alert's `StartsAt` while the alert stays active, and gives
it a new `StartsAt` when it fires again after resolving. calert uses the pair
of fingerprint and `StartsAt` to tell incidents apart:

- A payload continues the current incident when it carries any alert with the
  same fingerprint and `StartsAt` as an alert that was firing in the last
  delivered notification. An incident therefore lasts until every one of its
  alerts has resolved, even when some alerts resolve and fire again meanwhile.
- A payload with firing alerts and no such match opens a new incident and a new
  thread.
- A payload with only resolved alerts never opens an incident. It goes to the
  thread of the last incident.

Example for one group with alerts A1 and A2:

| Event | Thread |
|-------|--------|
| A1 fires | new thread T1 |
| A2 fires | T1 |
| A1 resolves | T1, because A2 is still firing |
| A1 fires again | T1, because A2 is still firing |
| A1 and A2 resolve | T1 |
| A1 fires again | new thread T2 |

The anchor of a new incident is the firing alert with the earliest `StartsAt`
in the payload, so it does not depend on stored state. Two replicas without
shared state choose the same thread for the same incident.

Limits:

- Events closer together than Alertmanager's `group_interval` arrive in one
  payload. If all alerts resolve and one fires again inside that interval,
  calert never sees the full resolve and keeps the old thread.
- Incident state expires with `thread_ttl`. Set `thread_ttl` longer than
  `repeat_interval`, otherwise a long incident loses its state between
  reminders. Without state, calert recomputes the anchor from the payload. The
  thread stays the same while the alert that opened the incident still fires,
  and changes once that alert has resolved.
- State written before incident threading has no anchor. The first
  notification after the upgrade opens a new thread for each group.

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
thread_ttl = "36h"              # state retention, longer than repeat_interval
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
