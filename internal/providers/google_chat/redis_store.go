package google_chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisStore shares last successfully delivered group state. Inspection and
// commit are intentionally separate: concurrent replicas may both deliver,
// but a failed delivery can never poison dedup state.
type redisStore struct {
	lo     *slog.Logger
	client redis.UniversalClient
	prefix string
	ttl    time.Duration
}

type redisGroupState struct {
	Hash     string            `json:"hash"`
	LastPost string            `json:"last_post"`
	Statuses map[string]string `json:"statuses"`
}

// commitStateScript compares fixed-width nanosecond strings lexicographically.
// Lua numbers cannot exactly represent Unix nanoseconds, so numeric comparison
// could let a slightly older delivery overwrite a newer one.
var commitStateScript = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if cur then
  local current = cjson.decode(cur)
  if current.last_post > ARGV[1] then
    return 0
  end
end
redis.call('SET', KEYS[1], ARGV[2], 'PX', tonumber(ARGV[3]))
return 1
`)

func newRedisStore(lo *slog.Logger, client redis.UniversalClient, prefix string, ttl time.Duration) *redisStore {
	return &redisStore{lo: lo, client: client, prefix: prefix, ttl: ttl}
}

// key never exposes the Alertmanager GroupKey and is bounded to a SHA-256
// digest. Room is part of the digest so two receivers cannot share state.
func (r *redisStore) key(room, groupKey string) string {
	prefix := r.prefix
	if prefix == "" {
		prefix = "calert"
	}
	return prefix + ":group:" + groupStoreKey(room, groupKey)
}

func (r *redisStore) inspect(ctx context.Context, room, groupKey, hash string, now time.Time, window time.Duration) (groupState, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	encoded, err := r.client.Get(ctx, r.key(room, groupKey)).Result()
	if err == redis.Nil {
		return groupState{}, false, nil
	}
	if err != nil {
		return groupState{}, false, fmt.Errorf("reading group state: %w", err)
	}

	var stored redisGroupState
	if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
		return groupState{}, false, fmt.Errorf("decoding group state: %w", err)
	}
	lastPost, err := strconv.ParseInt(stored.LastPost, 10, 64)
	if err != nil {
		return groupState{}, false, fmt.Errorf("decoding group timestamp: %w", err)
	}
	state := groupState{
		lastHash:     stored.Hash,
		lastPost:     time.Unix(0, lastPost),
		lastStatuses: stored.Statuses,
	}
	return state, state.lastHash == hash && now.Sub(state.lastPost) < window, nil
}

func (r *redisStore) commit(ctx context.Context, room, groupKey string, state groupState) error {
	lastPost := fixedTimestamp(state.lastPost)
	encoded, err := json.Marshal(redisGroupState{
		Hash:     state.lastHash,
		LastPost: lastPost,
		Statuses: state.lastStatuses,
	})
	if err != nil {
		return fmt.Errorf("encoding group state: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ttlMillis := max(1, int(r.ttl.Milliseconds()))
	if _, err := commitStateScript.Run(ctx, r.client, []string{r.key(room, groupKey)}, lastPost, encoded, ttlMillis).Result(); err != nil {
		return fmt.Errorf("saving group state: %w", err)
	}
	return nil
}

func fixedTimestamp(t time.Time) string {
	return fmt.Sprintf("%019d", t.UnixNano())
}
