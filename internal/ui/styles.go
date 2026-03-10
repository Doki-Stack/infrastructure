package ui

import "github.com/charmbracelet/lipgloss"

var (
	Purple = lipgloss.Color("#7C3AED")
	Green  = lipgloss.Color("#10B981")
	Red    = lipgloss.Color("#EF4444")
	Yellow = lipgloss.Color("#F59E0B")
	Cyan   = lipgloss.Color("#06B6D4")
	Gray   = lipgloss.Color("#6B7280")
	White  = lipgloss.Color("#F9FAFB")

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Purple).
			MarginBottom(1)

	SubtitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Cyan)

	PassStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Green)

	FailStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Red)

	WarnStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Yellow)

	SkipStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Gray)

	DimStyle = lipgloss.NewStyle().
			Foreground(Gray)

	BannerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Purple).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Purple).
			Padding(0, 2)
)

const Banner = `     _       _    _      _   _ 
  __| | ___ | | _(_) ___| |_| |
 / _` + "`" + ` |/ _ \| |/ / |/ __| __| |
| (_| | (_) |   <| | (__| |_| |
 \__,_|\___/|_|\_\_|\___|\__|_|`

func PrintBanner() string {
	return BannerStyle.Render(Banner)
}

func Pass(msg string) string {
	return PassStyle.Render("✓ PASS") + "  " + msg
}

func Fail(msg string) string {
	return FailStyle.Render("✗ FAIL") + "  " + msg
}

func Warn(msg string) string {
	return WarnStyle.Render("! WARN") + "  " + msg
}

func Skip(msg string) string {
	return SkipStyle.Render("- SKIP") + "  " + msg
}

func StepHeader(name string) string {
	return SubtitleStyle.Render("▸ " + name)
}

func SectionHeader(name string) string {
	return "\n" + TitleStyle.Render("═══ "+name+" ═══")
}
