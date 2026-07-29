# Group threading & Redis shared state

How to run calert so an incident's whole lifecycle (firing → partially
resolved → fully resolved) lands in **one** Google Chat thread instead of one
thread per alert.

## When to use what

| Deployment | `threading_mode` | Redis |
|------------|------------------|-------|
| Single calert instance | `"group"` | not needed (in-memory dedup) |
| Active-active (two+ instances) | `"group"` | **required** to avoid duplicate cards |
| Legacy / per-alert threads | `"alert"` (default) | n/a |

In `group` mode each calert keeps a small "what did I last post for this
group" state to suppress duplicates. With multiple instances that state must
be shared, otherwise both post the same card — that's what Redis is for.

## How it works (active-active)

```
                  ┌─────────────────┐
   Alertmanager A │   webhook fan    │ Alertmanager B
        └─────────┤   (clustered)    ├─────────┘
                  └────────┬─────────┘
            ┌──────────────┴──────────────┐
            ▼                              ▼
      ┌──────────┐                  ┌──────────┐
      │ calert 1 │                  │ calert 2 │
      └────┬─────┘                  └─────┬────┘
           │   atomic CAS (Lua)           │
           └──────────►┌──────────┐◄──────┘
                       │  Redis   │  shared dedup state
                       └──────────┘  key = hash(GroupKey + 12h bucket)
                            │
                            ▼ first writer wins
                     ┌─────────────┐
                     │ Google Chat │  one card per group thread
                     └─────────────┘
```

- **Thread key** is `hash(GroupKey + wall-clock bucket)`. Both instances
  compute the same key with no coordination, so their messages land in the
  same thread. Still-firing groups roll into a fresh thread at each bucket
  boundary (bucket size = `thread_ttl`).
- **Dedup** is a compare-and-set: an instance only posts if the group's
  alert-state hash changed since the last post within `dedup_window`. The
  read-modify-write runs as a single Lua script, so racing instances cannot
  both win.
- **Fail-open**: if Redis is unreachable the alert is posted anyway and
  `group_dedup_store_errors_total` increments. A Redis blip degrades to a
  possible duplicate, never a dropped alert.

## Configuration

Group mode requires `send_resolved: true` on the Alertmanager receiver (calert
needs the resolved events to update the thread). Use a `group_by` that does
**not** include `instance`, so related alerts share a group.

```toml
[providers.prod_alerts]
type = "google_chat"
endpoint = "https://chat.googleapis.com/v1/spaces/xxx/messages?key=...&token=..."
template = "static/message-group.tmpl"

threading_mode = "group"          # one thread per Alertmanager group
thread_ttl = "12h"                # bucket size for thread rollover
dedup_window = "2m"               # suppress identical re-posts within this window
max_alerts_per_message = 10       # per-instance sections per card; rest summarised as "and X more"

# Redis — only needed for active-active. Omit the whole block for single-instance
# (state stays in memory).
[providers.prod_alerts.redis]
address = "redis:6379"
password = ""                     # optional
db = 0
key_prefix = "calert:"            # namespace if the Redis is shared with other apps
```

Point every active-active instance at the **same** Redis and use the same
`key_prefix`.

## Metrics

- `alerts_deduplicated_total` — payloads suppressed as duplicates.
- `group_dedup_store_errors_total` — dedup-store (Redis) errors; non-zero
  means dedup is degraded and duplicates are possible.
