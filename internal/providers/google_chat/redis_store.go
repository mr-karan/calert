package google_chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisStore is the shared-state implementation of groupStateStore. Two
// active-active calert instances pointed at the same Redis coordinate their
// dedup decision: the whole read-modify-write runs as one atomic Lua script,
// so two instances racing on the same group key cannot both decide to post.
//
// Every error path resolves to a returned error so the dispatcher can fail
// open and post anyway — a Redis outage degrades dedup to "possible duplicate",
// never to a missed alert.
type redisStore struct {
	lo     *slog.Logger
	client redis.UniversalClient
	prefix string
	ttl    time.Duration
}

// redisGroupState is the JSON document stored per group key. It mirrors the
// memory store's groupState. Times are unix-nano so the Lua window comparison
// is plain integer arithmetic.
type redisGroupState struct {
	Hash     string            `json:"hash"`
	LastPost int64             `json:"last_post"`
	Statuses map[string]string `json:"statuses"`
}

// shouldPostScript is the cross-instance mutex. KEYS[1] is the group key.
// ARGV: incoming hash, now (unix-nano), window (nanos), ttl (seconds), and the
// new state JSON to store. It returns {postFlag, prevStateJSON}: postFlag 0 is
// the dedup skip; 1 means the new state was written and prevStateJSON (possibly
// empty) holds whatever was there before.
var shouldPostScript = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if cur then
  local st = cjson.decode(cur)
  if st.hash == ARGV[1] and (tonumber(ARGV[2]) - st.last_post) < tonumber(ARGV[3]) then
    return {0, ''}
  end
end
redis.call('SET', KEYS[1], ARGV[5], 'EX', tonumber(ARGV[4]))
return {1, cur or ''}
`)

func newRedisStore(lo *slog.Logger, client redis.UniversalClient, prefix string, ttl time.Duration) *redisStore {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &redisStore{lo: lo, client: client, prefix: prefix, ttl: ttl}
}

func (r *redisStore) key(groupKey string) string {
	if r.prefix == "" {
		return "calert:group:" + groupKey
	}
	return r.prefix + ":group:" + groupKey
}

func (r *redisStore) shouldPost(groupKey, hash string, statuses map[string]string, now time.Time, window time.Duration) (map[string]string, bool, error) {
	newState, err := json.Marshal(redisGroupState{Hash: hash, LastPost: now.UnixNano(), Statuses: statuses})
	if err != nil {
		return nil, false, fmt.Errorf("marshalling group state: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := shouldPostScript.Run(ctx, r.client,
		[]string{r.key(groupKey)},
		hash, now.UnixNano(), window.Nanoseconds(), int(r.ttl.Seconds()), newState,
	).Slice()
	if err != nil {
		return nil, false, fmt.Errorf("running dedup script: %w", err)
	}
	if len(res) != 2 {
		return nil, false, fmt.Errorf("unexpected dedup script reply: %v", res)
	}

	post, _ := res[0].(int64)
	if post == 0 {
		return nil, false, nil
	}

	prevJSON, _ := res[1].(string)
	if prevJSON == "" {
		return nil, true, nil
	}
	var prev redisGroupState
	if err := json.Unmarshal([]byte(prevJSON), &prev); err != nil {
		// Posting with no previous statuses is safe: at worst a resolved
		// instance is rendered one extra time. Don't fail the post over it.
		r.lo.Warn("unmarshalling previous group state, rendering all", "error", err, "group_key", groupKey)
		return nil, true, nil
	}
	return prev.Statuses, true, nil
}

// delete removes a group's key. Best-effort: a failed delete just leaves the
// TTL backstop to expire the key, so it never blocks alert delivery.
func (r *redisStore) delete(groupKey string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.client.Del(ctx, r.key(groupKey)).Err(); err != nil {
		r.lo.Warn("deleting group state from redis", "error", err, "group_key", groupKey)
	}
}
