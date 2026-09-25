package logging_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rmedranollamas/x-agent/internal/logging"
)

var fixedTime = time.Date(2026, 9, 19, 16, 30, 0, 0, time.UTC)

func fixedTimeFunc() time.Time {
	return fixedTime
}

func TestLogger_FormatRegularMessage(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(
		logging.WithOutput(&buf),
		logging.WithLevel(logging.LevelInfo),
		logging.WithTimeFunc(fixedTimeFunc),
	)

	logger.Info("Starting insights agent...")
	expected := "2026-09-19 16:30:00 - INFO - Starting insights agent...\n"
	if buf.String() != expected {
		t.Fatalf("expected %q, got %q", expected, buf.String())
	}
}

func TestLogger_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(
		logging.WithOutput(&buf),
		logging.WithLevel(logging.LevelInfo),
		logging.WithTimeFunc(fixedTimeFunc),
	)

	logger.Debug("This debug message should be skipped")
	if buf.Len() != 0 {
		t.Fatalf("expected empty buffer for debug message at info level, got %q", buf.String())
	}

	logger.SetLevel(logging.LevelDebug)
	logger.Debug("This debug message should appear")
	expected := "2026-09-19 16:30:00 - DEBUG - This debug message should appear\n"
	if buf.String() != expected {
		t.Fatalf("expected %q, got %q", expected, buf.String())
	}
}

func TestLogger_SingleLine_TTY(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(
		logging.WithOutput(&buf),
		logging.WithTerminal(true),
		logging.WithTimeFunc(fixedTimeFunc),
	)

	logger.SingleLine("Processing user 1001...")
	out := buf.String()
	if !strings.HasPrefix(out, "\r") {
		t.Fatalf("expected \\r prefix on TTY single-line update, got %q", out)
	}
	if strings.HasSuffix(out, "\n") {
		t.Fatalf("expected NO trailing newline on TTY single-line update, got %q", out)
	}
	if !strings.Contains(out, "Processing user 1001...") {
		t.Fatalf("expected message in output, got %q", out)
	}
}

func TestLogger_SingleLine_Overwrites_LongerPrevious(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(
		logging.WithOutput(&buf),
		logging.WithTerminal(true),
		logging.WithTimeFunc(fixedTimeFunc),
	)

	logger.SingleLine("A very long progress message that spans many characters")
	buf.Reset()

	logger.SingleLine("Short message")
	out := buf.String()

	if !strings.Contains(out, "\r") {
		t.Fatalf("expected carriage returns during overwrite, got %q", out)
	}
	if !strings.Contains(out, "Short message") {
		t.Fatalf("expected new message in output, got %q", out)
	}
}

func TestLogger_SingleLine_NonTTY(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(
		logging.WithOutput(&buf),
		logging.WithTerminal(false),
		logging.WithTimeFunc(fixedTimeFunc),
	)

	logger.SingleLine("Processing non-tty message...")
	out := buf.String()

	if strings.Contains(out, "\r") {
		t.Fatalf("expected NO \\r on non-TTY stream, got %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("expected trailing newline on non-TTY stream, got %q", out)
	}
	if !strings.Contains(out, "Processing non-tty message...") {
		t.Fatalf("expected message in output, got %q", out)
	}
}

func TestLogger_Transition_SingleLine_To_Regular_TTY(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(
		logging.WithOutput(&buf),
		logging.WithTerminal(true),
		logging.WithTimeFunc(fixedTimeFunc),
	)

	logger.SingleLine("In progress step 1...")
	logger.Info("All steps completed successfully.")

	out := buf.String()
	if !strings.Contains(out, "All steps completed successfully.\n") {
		t.Fatalf("expected regular message with trailing newline, got %q", out)
	}
}

func TestLogger_ParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  logging.Level
		err   bool
	}{
		{"DEBUG", logging.LevelDebug, false},
		{"debug", logging.LevelDebug, false},
		{"INFO", logging.LevelInfo, false},
		{"WARNING", logging.LevelWarn, false},
		{"warn", logging.LevelWarn, false},
		{"ERROR", logging.LevelError, false},
		{"unknown", logging.LevelInfo, true},
	}

	for _, tt := range tests {
		got, err := logging.ParseLevel(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("ParseLevel(%q) error = %v, wantErr %v", tt.input, err, tt.err)
		}
		if !tt.err && got != tt.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestLogger_Concurrency(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(
		logging.WithOutput(&buf),
		logging.WithTerminal(true),
		logging.WithLevel(logging.LevelDebug),
	)

	var wg sync.WaitGroup
	workers := 25
	iterations := 50

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if j%2 == 0 {
					logger.SingleLinef("worker %d progress %d", workerID, j)
				} else {
					logger.Infof("worker %d completed step %d", workerID, j)
				}
			}
		}(i)
	}

	wg.Wait()
	if buf.Len() == 0 {
		t.Fatalf("expected non-empty output from concurrent loggers")
	}
}
