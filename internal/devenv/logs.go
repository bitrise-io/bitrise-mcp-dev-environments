package devenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LogRead is what ReadLogStream collected from a session log stream.
type LogRead struct {
	// Text is the log content, or its last MaxBytes when Truncated.
	Text string
	// Truncated reports that earlier content was dropped to stay within
	// MaxBytes; the end of a log is kept, since that is where a failure shows.
	Truncated bool
	// Incomplete reports that the stream was still sending content when the
	// time budget ran out, so the log may continue past Text.
	Incomplete bool
}

// LogReadParams bounds a ReadLogStream call.
type LogReadParams struct {
	Path   string
	Params map[string]string
	// Idle ends the read once no content has arrived for this long: the
	// backend replays the log and then holds the stream open for new output,
	// never signalling the end, so a quiet gap is the end of what exists.
	Idle time.Duration
	// Budget caps the whole read, so a log that keeps growing still returns
	// within the client's request timeout.
	Budget time.Duration
	// FirstOutputWait is how long to wait for the stream to start. The
	// backend sends nothing, not even response headers, until the stage has
	// output, so a stage with none yet reads as empty once this passes.
	FirstOutputWait time.Duration
	// MaxBytes caps Text; older content is dropped first.
	MaxBytes int
}

// logFrame is one newline-delimited frame of a gRPC-gateway server stream:
// {"result":{...}} for content, {"error":{...}} for a mid-stream failure.
type logFrame struct {
	Result struct {
		LogContent       string `json:"logContent"`
		HeartbeatMessage bool   `json:"heartbeatMessage"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ReadLogStream reads a session log stream (GET .../sessions/{id}/logs/{stage})
// until it goes quiet for p.Idle, the stream ends, or p.Budget runs out.
func ReadLogStream(ctx context.Context, p LogReadParams) (LogRead, error) {
	authHeader, authValue, err := AuthFromCtx(ctx)
	if err != nil {
		return LogRead{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.Budget)
	defer cancel()

	fullURL := BaseURL + p.Path
	if q := encodeParams(p.Params, nil); q != "" {
		fullURL += "?" + q
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return LogRead{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set(authHeader, authValue)
	req.Header.Set("User-Agent", "bitrise-mcp-dev-environments/1.0")
	req.Header.Set("X-Request-Source", "mcp")

	// No client timeout: the stream never ends on its own; ctx bounds it.
	type response struct {
		resp *http.Response
		err  error
	}
	started := make(chan response, 1)
	go func() {
		resp, err := (&http.Client{}).Do(req)
		started <- response{resp, err}
	}()
	var resp *http.Response
	select {
	case r := <-started:
		if r.err != nil {
			if ctx.Err() != nil {
				return LogRead{}, nil
			}
			return LogRead{}, fmt.Errorf("execute request: %w", r.err)
		}
		resp = r.resp
	case <-time.After(p.FirstOutputWait):
		cancel()
		if r := <-started; r.resp != nil {
			r.resp.Body.Close()
		}
		return LogRead{}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return LogRead{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	type frame struct {
		content string
		err     error
		done    bool
	}
	frames := make(chan frame)
	go func() {
		send := func(f frame) bool {
			select {
			case frames <- f:
				return true
			case <-ctx.Done():
				return false
			}
		}
		// A json.Decoder rather than a line scanner: a frame has no size
		// cap, and the replay of a finished stage can arrive as one frame
		// far larger than any line limit. MaxBytes trims it afterwards.
		dec := json.NewDecoder(resp.Body)
		for {
			var f logFrame
			if err := dec.Decode(&f); err != nil {
				if errors.Is(err, io.EOF) {
					send(frame{done: true})
					return
				}
				var syntaxErr *json.SyntaxError
				var typeErr *json.UnmarshalTypeError
				if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
					err = fmt.Errorf("decode log frame: %w", err)
				}
				send(frame{err: err})
				return
			}
			if f.Error != nil {
				send(frame{err: errors.New(f.Error.Message)})
				return
			}
			if f.Result.HeartbeatMessage || f.Result.LogContent == "" {
				continue
			}
			if !send(frame{content: f.Result.LogContent}) {
				return
			}
		}
	}()

	var out LogRead
	var buf strings.Builder
	keep := func(s string) {
		buf.WriteString(s)
		if p.MaxBytes > 0 && buf.Len() > p.MaxBytes {
			// The cut can land inside a multi-byte character; drop its rest.
			tail := strings.ToValidUTF8(buf.String()[buf.Len()-p.MaxBytes:], "")
			buf.Reset()
			buf.WriteString(tail)
			out.Truncated = true
		}
	}
	idle := time.NewTimer(p.Idle)
	defer idle.Stop()
	for {
		select {
		case <-idle.C:
			out.Text = buf.String()
			return out, nil
		case <-ctx.Done():
			out.Text = buf.String()
			out.Incomplete = true
			return out, nil
		case f := <-frames:
			switch {
			case f.err != nil:
				if ctx.Err() != nil {
					out.Text = buf.String()
					out.Incomplete = true
					return out, nil
				}
				return LogRead{}, f.err
			case f.done:
				out.Text = buf.String()
				return out, nil
			default:
				keep(f.content)
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(p.Idle)
			}
		}
	}
}
