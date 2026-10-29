package devenv

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logServer streams the given frames, one per line, then keeps the
// connection open like the backend does, until the client goes away.
func logServer(t *testing.T, frames []string, every time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, f := range frames {
			_, _ = fmt.Fprintln(w, f)
			w.(http.Flusher).Flush()
			if every > 0 {
				time.Sleep(every)
			}
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	old := BaseURL
	BaseURL = srv.URL
	t.Cleanup(func() { BaseURL = old })
	return srv
}

func content(s string) string {
	return fmt.Sprintf(`{"result":{"logContent":%q,"heartbeatMessage":false}}`, s)
}

const heartbeat = `{"result":{"logContent":"","heartbeatMessage":true}}`

func readParams() LogReadParams {
	return LogReadParams{Path: "/v1/logs", Idle: 200 * time.Millisecond, FirstOutputWait: time.Second, Budget: 5 * time.Second, MaxBytes: 1024}
}

func patCtx() context.Context { return ContextWithPAT(context.Background(), "pat") }

// The backend never ends the stream: the read returns once it goes quiet,
// with every content frame in order and the heartbeats left out.
func TestReadLogStream_EndsOnQuietGap(t *testing.T) {
	logServer(t, []string{content("line 1\n"), heartbeat, content("line 2\n")}, 0)
	start := time.Now()
	read, err := ReadLogStream(patCtx(), readParams())
	require.NoError(t, err)
	assert.Equal(t, "line 1\nline 2\n", read.Text)
	assert.False(t, read.Truncated)
	assert.False(t, read.Incomplete)
	assert.Less(t, time.Since(start), 2*time.Second)
}

// A log that keeps printing returns at the budget, marked incomplete.
func TestReadLogStream_BudgetMarksIncomplete(t *testing.T) {
	var frames []string
	for i := 0; i < 100; i++ {
		frames = append(frames, content(fmt.Sprintf("tick %d\n", i)))
	}
	logServer(t, frames, 50*time.Millisecond)
	p := readParams()
	p.Budget = 500 * time.Millisecond
	read, err := ReadLogStream(patCtx(), p)
	require.NoError(t, err)
	assert.True(t, read.Incomplete)
	assert.True(t, strings.HasPrefix(read.Text, "tick 0\n"))
}

// Only the end of a long log is kept: that is where a failure shows.
func TestReadLogStream_KeepsTheEnd(t *testing.T) {
	logServer(t, []string{content(strings.Repeat("a", 1500)), content("the error\n")}, 0)
	read, err := ReadLogStream(patCtx(), readParams())
	require.NoError(t, err)
	assert.True(t, read.Truncated)
	assert.Len(t, read.Text, 1024)
	assert.True(t, strings.HasSuffix(read.Text, "the error\n"))
}

// A frame over 1 MiB (a finished stage replayed in one piece) is read whole
// and trimmed to its end, not refused with "token too long".
func TestReadLogStream_FrameLargerThanOneMiB(t *testing.T) {
	logServer(t, []string{content(strings.Repeat("a", 3<<20) + "the error\n")}, 0)
	read, err := ReadLogStream(patCtx(), readParams())
	require.NoError(t, err)
	assert.True(t, read.Truncated)
	assert.Len(t, read.Text, 1024)
	assert.True(t, strings.HasSuffix(read.Text, "the error\n"))
}

// A cut inside a multi-byte character never leaves invalid UTF-8 behind.
func TestReadLogStream_TruncatesOnCharacterBoundary(t *testing.T) {
	logServer(t, []string{content(strings.Repeat("é", 600))}, 0)
	read, err := ReadLogStream(patCtx(), readParams())
	require.NoError(t, err)
	assert.True(t, read.Truncated)
	assert.True(t, strings.Count(read.Text, "é")*2 == len(read.Text), "only whole characters are kept")
}

func TestReadLogStream_ErrorFrame(t *testing.T) {
	logServer(t, []string{content("partial\n"), `{"error":{"code":14,"message":"log stream interrupted, please reconnect"}}`}, 0)
	_, err := ReadLogStream(patCtx(), readParams())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "log stream interrupted")
}

func TestReadLogStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer pat", r.Header.Get("Authorization"))
		assert.Equal(t, "attempt=2", r.URL.RawQuery)
		http.Error(w, `{"code":5,"message":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()
	old := BaseURL
	BaseURL = srv.URL
	defer func() { BaseURL = old }()
	p := readParams()
	p.Params = map[string]string{"attempt": "2"}
	_, err := ReadLogStream(patCtx(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 404")
}

// A stage with no output yet gets no response at all, not even headers:
// the read gives up after FirstOutputWait and reports an empty log.
func TestReadLogStream_NoResponseIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	old := BaseURL
	BaseURL = srv.URL
	defer func() { BaseURL = old }()
	start := time.Now()
	read, err := ReadLogStream(patCtx(), readParams())
	require.NoError(t, err)
	assert.Equal(t, LogRead{}, read)
	assert.Less(t, time.Since(start), 3*time.Second)
}
