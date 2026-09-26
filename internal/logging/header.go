package logging

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// ANSI color escape sequences matching Python Typer style mappings.
const (
	ColorReset      = "\033[0m"
	ColorBoldGreen  = "\033[1;32m"
	ColorBoldYellow = "\033[1;33m"
	ColorCyan       = "\033[36m"
)

// FormatStartupHeader constructs the styled startup visibility header string.
// - Development: Bold Green (\033[1;32mDEVELOPMENT\033[0m)
// - Production / Other: Bold Yellow (\033[1;33mPRODUCTION\033[0m)
// - Database: Cyan (\033[36m<db_name>\033[0m)
func FormatStartupHeader(env string, isDev bool, dbName string) string {
	envUpper := strings.ToUpper(strings.TrimSpace(env))
	var envColor string
	if isDev {
		envColor = ColorBoldGreen
	} else {
		envColor = ColorBoldYellow
	}
	envStylized := fmt.Sprintf("%s%s%s", envColor, envUpper, ColorReset)
	dbStylized := fmt.Sprintf("%s%s%s", ColorCyan, strings.TrimSpace(dbName), ColorReset)
	return fmt.Sprintf("Environment: %s | Database: %s", envStylized, dbStylized)
}

// FormatStartupHeaderPlain constructs the unstyled startup header string.
func FormatStartupHeaderPlain(env string, dbName string) string {
	envUpper := strings.ToUpper(strings.TrimSpace(env))
	return fmt.Sprintf("Environment: %s | Database: %s", envUpper, strings.TrimSpace(dbName))
}

type terminalChecker interface {
	IsTerminal() bool
}

// IsTerminalWriter reports whether w is an interactive terminal character device.
func IsTerminalWriter(w io.Writer) bool {
	if tc, ok := w.(terminalChecker); ok {
		return tc.IsTerminal()
	}
	return isTerminalWriter(w)
}

// ShouldUseColor determines whether ANSI color sequences should be written to w.
// Color is disabled if:
//   - NO_COLOR environment variable is set and non-empty (https://no-color.org)
//   - TERM environment variable is "dumb"
//   - w is not an interactive terminal character device (TTY)
func ShouldUseColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if strings.ToLower(os.Getenv("TERM")) == "dumb" {
		return false
	}
	return IsTerminalWriter(w)
}

// PrintStartupHeader writes the formatted startup visibility header to w followed by a newline.
// If w is not a TTY, or if NO_COLOR is set or TERM=dumb, ANSI escape sequences are stripped via StripANSI.
func PrintStartupHeader(w io.Writer, env string, isDev bool, dbName string) error {
	header := FormatStartupHeader(env, isDev, dbName)
	if !ShouldUseColor(w) {
		header = StripANSI(header)
	}
	_, err := fmt.Fprintln(w, header)
	return err
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\033\[[0-9;]*[a-zA-Z]`)

// StripANSI strips all ANSI escape codes from s.
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}
