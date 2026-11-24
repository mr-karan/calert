# Group threading and shared state

Group threading sends the notifications of an Alertmanager group to a Google
Chat thread as one aggregated message per payload. By default a group keeps one
thread forever. `thread_rotation` can start a new thread after a quiet period
or for each new incident of the group.

## When to use what

| Deployment | `threading_mode` | Redis |
|------------|------------------|-------|
| Single calert instance | `"group"` | not needed |
| Active-active instances | `"group"` | optional shared state |
| Legacy per-alert threads | `"alert"` (default) | n/a |

The thread key is a SHA-256 digest of the receiver room and the `GroupKey`,
plus the thread anchor when `thread_rotation` is not `"none"`. Redis keys are a
digest of the room and `GroupKey`, so raw GroupKeys never appear in Redis and
two rooms cannot share state.

## Thread rotation

| `thread_rotation` | A group starts a new thread |
|-------------------|-----------------------------|
| `"none"` (default) | never, one thread per group |
| `"ttl"` | after `thread_ttl` without a post |
| `"incident"` | after `thread_ttl` without a post, and when the group fires again after all its alerts resolved |

With `"none"`, `thread_ttl` is only the retention period of the group state,
and thread keys are the same as before thread rotation existed.

### Quiet threads and `thread_ttl`

With `"ttl"` and `"incident"`, `thread_ttl` is the longest time a thread
may stay quiet. calert keeps the state of a group for `thread_ttl` after its
last delivered notification, and it never posts into a thread it has no state
for unless the alert that opened the thread started less than `thread_ttl` ago.

- A long incident keeps its thread as long as Alertmanager re-sends it within
  `thread_ttl`. Set `thread_ttl` longer than `repeat_interval`.
- When a group was quiet for longer than `thread_ttl`, for example during a
  long silence, the next notification starts a new thread, even when the alert
  that opened the old thread is still firing.

The anchor of a new thread is the firing alert with the earliest `StartsAt` in
the payload, together with the number of whole `thread_ttl` periods since that
`StartsAt`. It depends only on the payload and the clock, so two replicas
without shared state choose the same new thread.

### Incidents

With `"incident"`, Alertmanager's alert lifecycle tells incidents apart. An
alert keeps its `StartsAt` while it stays active, and gets a new `StartsAt`
when it fires again after resolving. calert uses the pair of fingerprint and
`StartsAt`:

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
| A1 fires again | new thread T2 (`"ttl"` keeps T1 if it was quiet for less than `thread_ttl`) |

### Silences

Alertmanager removes silenced alerts from the payload, including their resolve,
so calert does not see them while the silence lasts. With `"incident"`:

| Event | Thread |
|-------|--------|
| A1 fires | new thread T1 |
| A1 is silenced | no message |
| A2 fires | new thread T2, because A1 is not in the payload |
| A2 resolves | T2 |
| silence on A1 ends within `thread_ttl`, A1 still firing | T1, if A1 started less than `thread_ttl` ago |
| silence on A1 ends after `thread_ttl`, A1 still firing | new thread T3 |

When A1 resolves while it is silenced, T1 never gets a resolved message, and
the next firing of A1 starts a new thread. `incident_grace` keeps A2 in T1 when
it fires within the grace.

### Incident grace

During an upgrade alerts often fire and resolve over and over, and
`"incident"` would start a thread for each of them. `incident_grace` keeps a
new incident in the previous thread when the group posted less than
`incident_grace` ago. Every post extends the grace, so the whole burst shares
one thread until the group stays quiet for `incident_grace`.

With `incident_grace = "15m"`:

| Time | Event | Without grace | With grace |
|------|-------|---------------|------------|
| 0:00 | A1 fires | new thread T1 | new thread T1 |
| 0:10 | A1 resolves | T1 | T1 |
| 0:15 | A2 fires | new thread T2 | T1, 5 minutes after the last post |
| 0:25 | A2 resolves | T2 | T1 |
| 1:00 | A3 fires | new thread T3 | new thread T2, 35 minutes after the last post |

`incident_grace` works only with `thread_rotation = "incident"` and must be
shorter than `thread_ttl`. It needs the group state, so a replica without
shared state that did not deliver the previous post starts a new thread. Use
Redis with more than one replica.

### Limits

- Events closer together than Alertmanager's `group_interval` arrive in one
  payload. If all alerts resolve and one fires again inside that interval,
  `"incident"` never sees the full resolve and keeps the old thread.
- Without state, calert recomputes the anchor from the payload. The thread then
  stays the same only while the alert that opened it still fires and started
  less than `thread_ttl` ago. A restart without Redis during an older incident
  therefore starts a new thread.
- Replicas without shared state each see only part of the notifications, so
  with `"ttl"` or `"incident"` they can disagree on the thread, and a
  resolved message can land in a different thread than its firing one. Use
  Redis with more than one replica.
- Switching `thread_rotation` from `"none"`, and state written before thread
  rotation existed, start a new thread once for each group.

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
thread_rotation = "incident"    # "none" (default), "ttl" or "incident"
incident_grace = "15m"          # keep a new incident in the thread for 15m after the last post
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
