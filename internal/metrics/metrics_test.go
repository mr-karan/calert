package metrics

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	m := New("test_namespace")

	assert.NotNil(t, m)
	assert.Equal(t, "test_namespace", m.namespace)
	assert.NotNil(t, m.metrics)
	assert.False(t, m.startTime.IsZero())
}

func TestIncrement(t *testing.T) {
	m := New("test")
	m.Increment("counter_total")

	var buf bytes.Buffer
	m.FlushMetrics(&buf)

	assert.Contains(t, buf.String(), "test_counter_total")
}

func TestDecrement(t *testing.T) {
	m := New("test")
	m.Increment("counter_total")
	m.Increment("counter_total")
	m.Decrement("counter_total")

	var buf bytes.Buffer
	m.FlushMetrics(&buf)

	output := buf.String()
	assert.Contains(t, output, "test_counter_total")
}

func TestDuration(t *testing.T) {
	m := New("test")
	startTime := time.Now().Add(-100 * time.Millisecond)
	m.Duration("request_duration_seconds", startTime)

	var buf bytes.Buffer
	m.FlushMetrics(&buf)

	output := buf.String()
	assert.Contains(t, output, "test_request_duration_seconds")
}

func TestSet(t *testing.T) {
	m := New("test")
	m.Set("gauge_value", 42.5)

	var buf bytes.Buffer
	m.FlushMetrics(&buf)

	assert.Contains(t, buf.String(), "test_gauge_value")
}

func TestFlushMetrics(t *testing.T) {
	m := New("calert")

	var buf bytes.Buffer
	m.FlushMetrics(&buf)

	output := buf.String()

	assert.Contains(t, output, "calert_start_timestamp")
	assert.Contains(t, output, "calert_uptime_seconds")
	assert.Contains(t, output, "go_memstats")
}

func TestGetFormattedLabel(t *testing.T) {
	t.Run("with namespace", func(t *testing.T) {
		m := New("myapp")
		label := m.getFormattedLabel("request_count")
		assert.Equal(t, "myapp_request_count", label)
	})

	t.Run("without namespace", func(t *testing.T) {
		m := New("")
		label := m.getFormattedLabel("request_count")
		assert.Equal(t, "request_count", label)
	})
}

func TestMetricsIsolation(t *testing.T) {
	m1 := New("app1")
	m2 := New("app2")

	m1.Increment("counter")
	m2.Increment("counter")
	m2.Increment("counter")

	var buf1, buf2 bytes.Buffer
	m1.FlushMetrics(&buf1)
	m2.FlushMetrics(&buf2)

	lines1 := strings.Split(buf1.String(), "\n")
	lines2 := strings.Split(buf2.String(), "\n")

	var app1Counter, app2Counter string
	for _, line := range lines1 {
		if strings.HasPrefix(line, "app1_counter") {
			app1Counter = line
			break
		}
	}
	for _, line := range lines2 {
		if strings.HasPrefix(line, "app2_counter") {
			app2Counter = line
			break
		}
	}

	require.NotEmpty(t, app1Counter)
	require.NotEmpty(t, app2Counter)
	assert.Contains(t, app1Counter, "1")
	assert.Contains(t, app2Counter, "2")
}
