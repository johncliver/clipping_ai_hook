package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Colores ANSI
const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Dim    = "\033[2m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
	Gray   = "\033[90m"
	White  = "\033[37m"
)

const Rule = "────────────────────────────────────────────────────────"

var stdin = bufio.NewReader(os.Stdin)

func Col(c, s string) string { return c + s + Reset }
func B(s string) string      { return Bold + s + Reset }
func D(s string) string      { return Dim + s + Reset }

func OKLn(s string)   { fmt.Println(Col(Green, "  ✅ "+s)) }
func ErrLn(s string)  { fmt.Println(Col(Red, "  ❌ "+s)) }
func WarnLn(s string) { fmt.Println(Col(Yellow, "  ⚠️  "+s)) }

func ReadLine() string {
	s, _ := stdin.ReadString('\n')
	return strings.TrimSpace(s)
}

func Ask(q string) string {
	fmt.Println()
	fmt.Print(Col(Bold+Cyan, "▶ ") + Col(White, q) + Col(Cyan, " > "))
	return ReadLine()
}

func AskYes(q string) bool {
	fmt.Println()
	fmt.Print(Col(Bold+Yellow, "? ") + Col(White, q) + Col(Gray, " [s/N] > "))
	a := strings.ToLower(ReadLine())
	return a == "s" || a == "si" || a == "sí" || a == "y" || a == "yes"
}

func Exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func Estado(b bool) string {
	if b {
		return Col(Green+Bold, "ON")
	}
	return Col(Gray, "OFF")
}
