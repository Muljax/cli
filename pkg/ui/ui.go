package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

var (
	NoColor = false
)

func init() {
	if _, exists := os.LookupEnv("NO_COLOR"); exists {
		NoColor = true
	}
	if !IsTerminal(os.Stdout) {
		NoColor = true
	}
	enableVirtualTerminalProcessing()
}

func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

const (
	ansiReset      = "\033[0m"
	ansiBold       = "\033[1m"
	ansiDim        = "\033[2m"
	ansiUnderline  = "\033[4m"
	ansiRed        = "\033[31m"
	ansiGreen      = "\033[32m"
	ansiYellow     = "\033[33m"
	ansiBlue       = "\033[34m"
	ansiMagenta    = "\033[35m"
	ansiCyan       = "\033[36m"
	ansiWhite      = "\033[37m"
	ansiBgRed      = "\033[41m"
	ansiBgGreen    = "\033[42m"
	ansiBgYellow   = "\033[43m"
	ansiBrightCyan = "\033[96m"
)

func colorize(code, text string) string {
	if NoColor || text == "" {
		return text
	}
	return code + text + ansiReset
}

func Bold(text string) string       { return colorize(ansiBold, text) }
func Dim(text string) string        { return colorize(ansiDim, text) }
func Red(text string) string        { return colorize(ansiRed, text) }
func Green(text string) string      { return colorize(ansiGreen, text) }
func Yellow(text string) string     { return colorize(ansiYellow, text) }
func Blue(text string) string       { return colorize(ansiBlue, text) }
func Cyan(text string) string       { return colorize(ansiCyan, text) }
func BrightCyan(text string) string { return colorize(ansiBrightCyan, text) }

// Badges
func BadgeSuccess(text string) string {
	if NoColor {
		return "[" + text + "]"
	}
	return ansiBold + ansiGreen + "[" + text + "]" + ansiReset
}

func BadgeDanger(text string) string {
	if NoColor {
		return "[" + text + "]"
	}
	return ansiBold + ansiRed + "[" + text + "]" + ansiReset
}

func BadgeWarning(text string) string {
	if NoColor {
		return "[" + text + "]"
	}
	return ansiBold + ansiYellow + "[" + text + "]" + ansiReset
}

func BadgeInfo(text string) string {
	if NoColor {
		return "[" + text + "]"
	}
	return ansiBold + ansiCyan + "[" + text + "]" + ansiReset
}

// Icons
func SuccessIcon() string { return Green("✓") }
func ErrorIcon() string   { return Red("✗") }
func WarningIcon() string { return Yellow("⚠") }
func InfoIcon() string    { return Cyan("ℹ") }
func ArrowIcon() string   { return Dim("→") }

// Structured formatting
func Header(title string) {
	fmt.Printf("\n%s\n%s\n", Bold(title), Dim(strings.Repeat("─", 54)))
}

func Subheader(title string) {
	fmt.Printf("\n%s\n", Dim("── "+title+" "+strings.Repeat("─", 48-len(title))))
}

func KeyValue(key string, val string) {
	padded := fmt.Sprintf("%-16s", key)
	fmt.Printf("  %s %s\n", Dim(padded), val)
}

func KeyValueColored(key string, val string, colorFunc func(string) string) {
	padded := fmt.Sprintf("%-16s", key)
	fmt.Printf("  %s %s\n", Dim(padded), colorFunc(val))
}

func Step(text string) {
	fmt.Printf("%s %s\n", SuccessIcon(), text)
}

func StepWarn(text string) {
	fmt.Printf("%s %s\n", WarningIcon(), text)
}

func StepError(text string) {
	fmt.Printf("%s %s\n", ErrorIcon(), Red(text))
}

func StepInfo(text string) {
	fmt.Printf("%s %s\n", InfoIcon(), text)
}
