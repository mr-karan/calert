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
in the payload, together with the number of whole `thread_ttl` periods since
that `StartsAt`. It depends only on the payload and the clock, so two replicas
without shared state choose the same thread for a new incident.

### Quiet threads and `thread_ttl`

`thread_ttl` is the longest time a thread may stay quiet. calert keeps the
state of a group for `thread_ttl` after its last delivered notification, and it
never posts into a thread it has no state for unless the incident started
less than `thread_ttl` ago.

- A long incident keeps its thread as long as Alertmanager re-sends it within
  `thread_ttl`. Set `thread_ttl` longer than `repeat_interval`.
- When a group was quiet for longer than `thread_ttl`, for example during a
  long silence, the next notification starts a new thread, even when the alert
  that opened the old thread is still firing.

### Silences

Alertmanager removes silenced alerts from the payload, including their resolve,
so calert does not see them while the silence lasts.

| Event | Thread |
|-------|--------|
| A1 fires | new thread T1 |
| A1 is silenced | no message |
| A2 fires | new thread T2, because A1 is not in the payload |
| A2 resolves | T2 |
| silence on A1 ends within `thread_ttl`, A1 still firing | T1, if A1 started less than `thread_ttl` ago |
| silence on A1 ends after `thread_ttl`, A1 still firing | new thread T3 |

When A1 resolves while it is silenced, T1 never gets a resolved message, and
the next firing of A1 starts a new thread.

### Limits

- Events closer together than Alertmanager's `group_interval` arrive in one
  payload. If all alerts resolve and one fires again inside that interval,
  calert never sees the full resolve and keeps the old thread.
- Without state, calert recomputes the anchor from the payload. The thread then
  stays the same only while the alert that opened the incident still fires and
  started less than `thread_ttl` ago. A restart without Redis during an older
  incident therefore starts a new thread.
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
thread_ttl = "36h"              # longest quiet time of a thread, longer than repeat_interval
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
