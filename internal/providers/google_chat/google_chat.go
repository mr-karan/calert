package google_chat

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/mr-karan/calert/internal/metrics"
	"github.com/mr-karan/calert/internal/providers"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/redis/go-redis/v9"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type GoogleChatManager struct {
	lo              *slog.Logger
	metrics         *metrics.Manager
	activeAlerts    *ActiveAlerts
	endpoint        string
	room            string
	client          *retryablehttp.Client
	msgTmpl         *template.Template
	dryRun          bool
	threadedReplies bool
	threadingMode   string
	threadTTL       time.Duration
	dedupWindow     time.Duration
	maxAlertsPerMsg int
	groupStore      groupStateStore
}

const (
	// ThreadingModeAlert is the legacy behaviour: one thread per alert
	// fingerprint, keyed by a random UUID held in memory.
	ThreadingModeAlert = "alert"
	// ThreadingModeGroup threads by Alertmanager group: one aggregated
	// message per webhook payload, posted into a deterministic thread.
	ThreadingModeGroup = "group"
)

type GoogleChatOpts struct {
	Log             *slog.Logger
	Metrics         *metrics.Manager
	DryRun          bool
	MaxIdleConn     int
	Timeout         time.Duration
	ProxyURL        string
	Endpoint        string
	Room            string
	Template        string
	ThreadTTL       time.Duration
	ThreadedReplies bool
	ThreadingMode   string
	DedupWindow     time.Duration
	MaxAlertsPerMsg int
	RetryMax        int
	RetryWaitMin    time.Duration
	RetryWaitMax    time.Duration

	// Optional shared dedup state for active-active deployments. When
	// RedisAddress is empty, group state stays in memory (default).
	RedisAddress   string
	RedisPassword  string
	RedisDB        int
	RedisKeyPrefix string
}

// NewGoogleChat initializes a Google Chat provider object.
func NewGoogleChat(opts GoogleChatOpts) (*GoogleChatManager, error) {
	transport := &http.Transport{
		MaxIdleConnsPerHost: opts.MaxIdleConn,
	}

	// Add a proxy to make upstream requests if specified in config.
	if opts.ProxyURL != "" {
		u, err := url.Parse(opts.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("error parsing proxy URL: %s", err)
		}
		transport.Proxy = http.ProxyURL(u)
	}

	// Initialise a retryable HTTP Client for communicating with the G-Chat APIs.
	client := retryablehttp.NewClient()
	client.RetryMax = opts.RetryMax
	client.RetryWaitMin = opts.RetryWaitMin
	client.RetryWaitMax = opts.RetryWaitMax
	client.HTTPClient.Timeout = opts.Timeout
	client.HTTPClient.Transport = transport
	client.Logger = &slogAdapter{logger: opts.Log}

	// Custom CheckRetry policy that also retries on 429 (Too Many Requests).
	// This is important for Google Chat API which rate limits requests.
	client.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		// First, check the default retry policy.
		shouldRetry, checkErr := retryablehttp.DefaultRetryPolicy(ctx, resp, err)
		if shouldRetry {
			return true, checkErr
		}

		// Additionally retry on 429 Too Many Requests.
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			return true, nil
		}

		return false, nil
	}

	// Initialise the map of active alerts.
	alerts := make(map[string]AlertDetails, 0)

	// Initialise message template functions.
	templateFuncMap := template.FuncMap{
		"Title": func(v any) string {
			s := fmt.Sprintf("%v", v)
			titleCaser := cases.Title(language.English)
			return titleCaser.String(s)
		},
		"toUpper": func(v any) string {
			return strings.ToUpper(fmt.Sprintf("%v", v))
		},
		"toLower": func(v any) string {
			return strings.ToLower(fmt.Sprintf("%v", v))
		},
		"Contains": func(s, substr any) bool {
			return strings.Contains(fmt.Sprintf("%v", s), fmt.Sprintf("%v", substr))
		},
		"HasPrefix": func(s, prefix any) bool {
			return strings.HasPrefix(fmt.Sprintf("%v", s), fmt.Sprintf("%v", prefix))
		},
		"HasSuffix": func(s, suffix any) bool {
			return strings.HasSuffix(fmt.Sprintf("%v", s), fmt.Sprintf("%v", suffix))
		},
		"Replace": func(s, old, new any) string {
			return strings.ReplaceAll(fmt.Sprintf("%v", s), fmt.Sprintf("%v", old), fmt.Sprintf("%v", new))
		},
		"TrimSpace": func(v any) string {
			return strings.TrimSpace(fmt.Sprintf("%v", v))
		},
		"Default": func(defaultVal, val any) any {
			if val == nil || fmt.Sprintf("%v", val) == "" {
				return defaultVal
			}
			return val
		},
		"reReplaceAll": func(pattern, repl, text string) string {
			re := regexp.MustCompile(pattern)
			return re.ReplaceAllString(text, repl)
		},
		"CurrentTime": func(location ...string) string {
			if len(location) == 0 || location[0] == "" {
				return time.Now().Format("2006-01-02 15:04:05 MST")
			}
			loc, err := time.LoadLocation(location[0])
			if err != nil {
				return fmt.Sprintf("Error loading timezone: %v", err)
			}
			return time.Now().In(loc).Format("2006-01-02 15:04:05 MST")
		},
		"ConvertTZ": func(t time.Time, location string) string {
			loc, err := time.LoadLocation(location)
			if err != nil {
				return fmt.Sprintf("Error loading timezone: %v", err)
			}
			return t.In(loc).Format("2006-01-02 15:04:05 MST")
		},
		"DurationSince": func(t time.Time) string {
			d := time.Since(t)
			h := int(d.Hours())
			m := int(d.Minutes()) % 60
			s := int(d.Seconds()) % 60
			return fmt.Sprintf("%dh %dm %ds", h, m, s)
		},
	}

	// Load the template.
	tmpl, err := template.New(filepath.Base(opts.Template)).Funcs(templateFuncMap).ParseFiles(opts.Template)
	if err != nil {
		return nil, err
	}

	mgr := &GoogleChatManager{
		lo:       opts.Log,
		metrics:  opts.Metrics,
		client:   client,
		endpoint: opts.Endpoint,
		room:     opts.Room,
		activeAlerts: &ActiveAlerts{
			alerts:  alerts,
			lo:      opts.Log,
			metrics: opts.Metrics,
		},
		msgTmpl:         tmpl,
		dryRun:          opts.DryRun,
		threadedReplies: opts.ThreadedReplies,
		threadingMode:   opts.ThreadingMode,
		threadTTL:       opts.ThreadTTL,
		dedupWindow:     opts.DedupWindow,
		maxAlertsPerMsg: opts.MaxAlertsPerMsg,
	}
	if mgr.threadingMode == "" {
		mgr.threadingMode = ThreadingModeAlert
	}
	if mgr.threadTTL <= 0 {
		mgr.threadTTL = 12 * time.Hour
	}
	if mgr.threadingMode != ThreadingModeAlert && mgr.threadingMode != ThreadingModeGroup {
		return nil, fmt.Errorf("invalid threading_mode %q: must be %q or %q", mgr.threadingMode, ThreadingModeAlert, ThreadingModeGroup)
	}
	// Group mode is threaded by definition (one thread per alert group), so it
	// ignores threaded_replies. Force it on and tell the operator, rather than
	// silently honouring a flag that can't apply.
	if mgr.threadingMode == ThreadingModeGroup && !mgr.threadedReplies {
		opts.Log.Warn("threaded_replies is ignored in group threading mode; threading is always on", "room", mgr.room)
		mgr.threadedReplies = true
	}
	// Start a background worker to cleanup alerts based on TTL mechanism.
	go mgr.activeAlerts.startPruneWorker(1*time.Hour, opts.ThreadTTL)

	// Select the group dedup store. With Redis configured, both calert
	// instances share state; otherwise state is per-process in memory.
	if opts.RedisAddress != "" {
		client := redis.NewClient(&redis.Options{
			Addr:     opts.RedisAddress,
			Password: opts.RedisPassword,
			DB:       opts.RedisDB,
		})
		mgr.groupStore = newRedisStore(opts.Log, client, opts.RedisKeyPrefix, mgr.threadTTL)
		opts.Log.Info("group dedup state shared via redis", "address", opts.RedisAddress, "key_prefix", opts.RedisKeyPrefix)
	} else {
		mem := newMemoryStore(opts.Log)
		// Backstop pruner for groups that never report a fully-resolved
		// payload. Redis uses key TTLs instead, so this is memory-only.
		go mem.startPruneWorker(1*time.Hour, opts.ThreadTTL)
		mgr.groupStore = mem
	}

	return mgr, nil
}

// Push accepts an Alertmanager webhook payload and dispatches notifications
// to the Webhook API endpoint, threading either per alert (legacy) or per
// group depending on the configured threading mode.
func (m *GoogleChatManager) Push(payload providers.WebhookPayload) error {
	if m.threadingMode == ThreadingModeGroup {
		return m.pushGroup(payload)
	}
	return m.pushAlerts(payload.Alerts)
}

// pushGroup posts one aggregated message per webhook payload into the
// group's deterministic thread.
func (m *GoogleChatManager) pushGroup(payload providers.WebhookPayload) error {
	var (
		now       = time.Now()
		threadKey = threadKeyForGroup(payload.GroupKey, now, m.threadTTL)
		hash      = stateHash(payload.Alerts)
	)

	m.lo.Info("dispatching group notification to google chat", "group_key", payload.GroupKey, "count", len(payload.Alerts))

	// Drop cluster-race duplicates: same alert states, posted moments ago.
	prevStatuses, post, err := m.groupStore.shouldPost(payload.GroupKey, hash, statusesOf(payload.Alerts), now, m.dedupWindow)
	if err != nil {
		// Fail open: a dedup-store outage must never drop an alert. Worst
		// case is the duplicate we already tolerate today.
		m.lo.Warn("group dedup store unavailable, posting anyway", "error", err, "group_key", payload.GroupKey)
		m.metrics.Increment(fmt.Sprintf(`group_dedup_store_errors_total{provider="%s", room="%s"}`, m.ID(), m.Room()))
		prevStatuses, post = nil, true
	} else if !post {
		m.lo.Debug("suppressing duplicate group notification", "group_key", payload.GroupKey, "state_hash", hash)
		m.metrics.Increment(fmt.Sprintf(`alerts_deduplicated_total{provider="%s", room="%s"}`, m.ID(), m.Room()))
		return nil
	}

	tmplCtx := buildGroupContext(payload, threadKey, m.maxAlertsPerMsg, prevStatuses)

	// Nothing new to render: every instance in this payload was already shown
	// (e.g. an all-resolved re-send after the dedup window, or a payload of
	// only already-resolved instances re-sent when a new member fires). Skip
	// so we don't post an empty card. The first firing→resolved transition
	// still renders, since that instance isn't yet recorded as resolved.
	if len(tmplCtx.Alerts) == 0 {
		m.lo.Debug("no new alerts to render for group, skipping", "group_key", payload.GroupKey)
		return nil
	}

	msgs, err := m.prepareMessage(tmplCtx)
	if err != nil {
		m.lo.Error("error preparing group message", "error", err)
		m.metrics.Increment(fmt.Sprintf(`alerts_dispatched_errors_total{provider="%s", room="%s", reason="preparing"}`, m.ID(), m.Room()))
		return err
	}

	for _, msg := range msgs {
		if m.dryRun {
			m.lo.Info("dry_run is enabled for this room. skipping pushing notification", "room", m.Room())
			continue
		}
		// threadedReplies is forced true for group mode in the constructor.
		if err := m.sendMessage(msg, threadKey, m.threadedReplies); err != nil {
			m.metrics.Increment(fmt.Sprintf(`alerts_dispatched_errors_total{provider="%s", room="%s", reason="sending"}`, m.ID(), m.Room()))
			m.lo.Error("error sending group message", "error", err)
			return err
		}
	}
	// Count in alert-units (not payloads) so alerts_dispatched_total keeps the
	// same meaning as alert mode, and only after a real send so it never
	// overlaps alerts_deduplicated_total.
	m.metrics.IncrementBy(fmt.Sprintf(`alerts_dispatched_total{provider="%s", room="%s"}`, m.ID(), m.Room()), len(payload.Alerts))
	m.metrics.Duration(fmt.Sprintf(`alerts_dispatched_duration_seconds{provider="%s", room="%s"}`, m.ID(), m.Room()), now)

	// Group state is intentionally NOT deleted on full resolve: Alertmanager
	// keeps recently-resolved members in the group and re-sends them when a new
	// member fires, so we must remember they were already shown to suppress
	// them. The store's TTL (thread_ttl) is the backstop that forgets the group
	// once the incident is genuinely over.
	return nil
}

// pushAlerts is the legacy per-alert dispatch path.
func (m *GoogleChatManager) pushAlerts(alerts []alertmgrtmpl.Alert) error {
	m.lo.Info("dispatching alerts to google chat", "count", len(alerts))

	// For each alert, lookup the UUID and send the alert.
	for _, a := range alerts {
		now := time.Now()

		m.metrics.Increment(fmt.Sprintf(`alerts_dispatched_total{provider="%s", room="%s"}`, m.ID(), m.Room()))

		// If it's a new alert whose fingerprint isn't in the active alerts map, add it first.
		if m.activeAlerts.loookup(a.Fingerprint) == "" {
			m.activeAlerts.add(a)
		}

		// Prepare a list of messages to send.
		msgs, err := m.prepareMessage(a)
		if err != nil {
			m.lo.Error("error preparing message", "error", err)
			m.metrics.Increment(fmt.Sprintf(`alerts_dispatched_errors_total{provider="%s", room="%s", reason="preparing"}`, m.ID(), m.Room()))
			continue
		}

		// Dispatch an HTTP request for each message.
		for _, msg := range msgs {
			var threadKey = m.activeAlerts.alerts[a.Fingerprint].UUID.String()

			// Send message to API.
			if m.dryRun {
				m.lo.Info("dry_run is enabled for this room. skipping pushing notification", "room", m.Room())
			} else {
				if err := m.sendMessage(msg, threadKey, m.threadedReplies); err != nil {
					m.metrics.Increment(fmt.Sprintf(`alerts_dispatched_errors_total{provider="%s", room="%s", reason="sending"}`, m.ID(), m.Room()))
					m.lo.Error("error sending message", "error", err)
					continue
				}
			}
		}
		m.metrics.Duration(fmt.Sprintf(`alerts_dispatched_duration_seconds{provider="%s", room="%s"}`, m.ID(), m.Room()), now)
	}

	return nil
}

// Room returns the name of room for which this provider is configured.
func (m *GoogleChatManager) Room() string {
	return m.room
}

// ID returns the provider name.
func (m *GoogleChatManager) ID() string {
	return "google_chat"
}
