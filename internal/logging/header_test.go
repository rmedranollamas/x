package logging_test

import (
	"bytes"
	"testing"

	"github.com/rmedranollamas/x-agent/internal/logging"
)

func TestFormatStartupHeader_Development(t *testing.T) {
	header := logging.FormatStartupHeader("development", true, "insights_dev.db")
	expected := "Environment: \033[1;32mDEVELOPMENT\033[0m | Database: \033[36minsights_dev.db\033[0m"
	if header != expected {
		t.Fatalf("expected %q, got %q", expected, header)
	}
}

func TestFormatStartupHeader_Production(t *testing.T) {
	header := logging.FormatStartupHeader("production", false, "insights.db")
	expected := "Environment: \033[1;33mPRODUCTION\033[0m | Database: \033[36minsights.db\033[0m"
	if header != expected {
		t.Fatalf("expected %q, got %q", expected, header)
	}
}

func TestFormatStartupHeader_WhitespaceAndUpper(t *testing.T) {
	header := logging.FormatStartupHeader("  development  ", true, "  insights_dev.db  ")
	expected := "Environment: \033[1;32mDEVELOPMENT\033[0m | Database: \033[36minsights_dev.db\033[0m"
	if header != expected {
		t.Fatalf("expected %q, got %q", expected, header)
	}
}

func TestFormatStartupHeaderPlain(t *testing.T) {
	plain := logging.FormatStartupHeaderPlain("development", "insights_dev.db")
	expected := "Environment: DEVELOPMENT | Database: insights_dev.db"
	if plain != expected {
		t.Fatalf("expected %q, got %q", expected, plain)
	}
}

func TestStripANSI(t *testing.T) {
	styled := logging.FormatStartupHeader("development", true, "insights_dev.db")
	stripped := logging.StripANSI(styled)
	expected := "Environment: DEVELOPMENT | Database: insights_dev.db"
	if stripped != expected {
		t.Fatalf("expected %q, got %q", expected, stripped)
	}
}

type mockTerminalWriter struct {
	bytes.Buffer
	isTerm bool
}

func (m *mockTerminalWriter) IsTerminal() bool {
	return m.isTerm
}

func TestPrintStartupHeader(t *testing.T) {
	// bytes.Buffer is a non-terminal writer, so ANSI codes must be stripped.
	var buf bytes.Buffer
	err := logging.PrintStartupHeader(&buf, "development", true, "insights_dev.db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Environment: DEVELOPMENT | Database: insights_dev.db\n"
	if buf.String() != expected {
		t.Fatalf("expected plain header %q, got %q", expected, buf.String())
	}
}

func TestPrintStartupHeader_Terminal(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	w := &mockTerminalWriter{isTerm: true}
	err := logging.PrintStartupHeader(w, "development", true, "insights_dev.db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Environment: \033[1;32mDEVELOPMENT\033[0m | Database: \033[36minsights_dev.db\033[0m\n"
	if w.String() != expected {
		t.Fatalf("expected styled header %q, got %q", expected, w.String())
	}
}

func TestPrintStartupHeader_NO_COLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	w := &mockTerminalWriter{isTerm: true}
	err := logging.PrintStartupHeader(w, "development", true, "insights_dev.db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Environment: DEVELOPMENT | Database: insights_dev.db\n"
	if w.String() != expected {
		t.Fatalf("expected plain header with NO_COLOR %q, got %q", expected, w.String())
	}
}

func TestPrintStartupHeader_TermDumb(t *testing.T) {
	t.Setenv("TERM", "dumb")
	w := &mockTerminalWriter{isTerm: true}
	err := logging.PrintStartupHeader(w, "development", true, "insights_dev.db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Environment: DEVELOPMENT | Database: insights_dev.db\n"
	if w.String() != expected {
		t.Fatalf("expected plain header with TERM=dumb %q, got %q", expected, w.String())
	}
}

func TestShouldUseColor(t *testing.T) {
	var buf bytes.Buffer
	if logging.ShouldUseColor(&buf) {
		t.Errorf("expected ShouldUseColor to return false for non-terminal bytes.Buffer")
	}

	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	wTerm := &mockTerminalWriter{isTerm: true}
	if !logging.ShouldUseColor(wTerm) {
		t.Errorf("expected ShouldUseColor to return true for terminal writer")
	}

	t.Setenv("NO_COLOR", "1")
	if logging.ShouldUseColor(wTerm) {
		t.Errorf("expected ShouldUseColor to return false when NO_COLOR is set")
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if logging.ShouldUseColor(wTerm) {
		t.Errorf("expected ShouldUseColor to return false when TERM=dumb")
	}
}

func TestIsTerminalWriter(t *testing.T) {
	var buf bytes.Buffer
	if logging.IsTerminalWriter(&buf) {
		t.Errorf("expected IsTerminalWriter to return false for bytes.Buffer")
	}

	wTerm := &mockTerminalWriter{isTerm: true}
	if !logging.IsTerminalWriter(wTerm) {
		t.Errorf("expected IsTerminalWriter to return true for mockTerminalWriter{isTerm: true}")
	}
}
