package tui

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/validation"
)

func TestWrapProseKeepsWordsAndExplicitNewlines(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{"viewer settings", "Open the viewer settings to continue.", 18, []string{"Open the viewer", "settings to", "continue."}},
		{"indented reason", "  Open the viewer settings to continue.", 18, []string{"  Open the viewer", "settings to", "continue."}},
		{"intentional paragraphs", "First line\n\nSecond line\n", 80, []string{"First line", "", "Second line", ""}},
		{"empty", "", 80, []string{""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wrap(tc.text, tc.width); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("wrap = %#v; want %#v", got, tc.want)
			}
		})
	}
}

func TestWrapPreservesStructuredSpacing(t *testing.T) {
	for _, text := range []string{
		"Name              State          CPU",
		"  fixture         shut off       2",
		"\tname\tstate\tcpu",
		"  \"message\": \"keep the exact JSON spacing\",",
		"<disk type=\"file\" device=\"disk\">",
		"  <source file=\"/images/a path with spaces.qcow2\"/>",
		"    [  Advanced options  ]",
		"trailing spaces   ",
		"          /long-first-token-with-indentation",
		"                    ",
	} {
		for _, width := range []int{2, 16, 80} {
			got := strings.Join(wrap(text, width), "\n")
			want := ansi.Hardwrap(text, width, true)
			if got != want {
				t.Fatalf("structured spacing changed at width %d: got %q; want %q", width, got, want)
			}
		}
	}
}

func TestWrapLongTokensAndGraphemesRemainComplete(t *testing.T) {
	for _, text := range []string{
		"/var/lib/virmill/images/abcdefghijklmnopqrstuvwxyz0123456789.qcow2",
		strings.Repeat("abcdef0123456789", 8),
		"界面設定界面設定界面設定",
		strings.Repeat("e\u0301", 12),
		strings.Repeat("👩‍💻", 6),
	} {
		for _, width := range []int{2, 3, 7, 16, 80} {
			lines := wrap(text, width)
			if strings.Join(lines, "") != text {
				t.Fatalf("content lost at width %d: %q => %#v", width, text, lines)
			}
			for _, line := range lines {
				if !utf8.ValidString(line) || ansi.StringWidth(line) > width || strings.HasPrefix(line, "\u0301") || strings.HasPrefix(line, "\u200d") {
					t.Fatalf("invalid grapheme or width %d exceeded: %q", width, line)
				}
			}
		}
	}
	// A two-cell grapheme cannot fit one cell. Keep it, rather than silently
	// erasing it; normal Workspace content is only rendered at width >= 60.
	for _, width := range []int{0, 1} {
		if got := strings.Join(wrap("界", width), ""); got != "界" {
			t.Fatalf("narrow viewport discarded grapheme: %q", got)
		}
	}
}

func TestWrapTrustedANSIAndCallerSanitization(t *testing.T) {
	styled := "Open the \x1b[1mviewer settings\x1b[0m to continue."
	got := strings.Join(wrap(styled, 18), "\n")
	if ansi.Strip(got) != "Open the viewer\nsettings to\ncontinue." || !strings.Contains(got, "\x1b[1m") || !strings.Contains(got, "\x1b[0m") {
		t.Fatalf("trusted styles or words changed: %q", got)
	}
	untrusted := "Open\x1b[2J the viewer\x1b]52;c;c2VjcmV0\x07 settings\x00 to continue."
	safe := validation.SafeText(untrusted)
	got = strings.Join(pageLines([]string{untrusted}, 18, 24, 0), "\n")
	if strings.ContainsAny(got, "\x1b\x00\x07") || !reflect.DeepEqual(strings.Fields(got), strings.Fields(safe)) {
		t.Fatalf("page wrapping bypassed sanitization or changed text: %q", got)
	}
}

func TestWrapPagedReviewRemainsReachableAt80x24(t *testing.T) {
	text := strings.Repeat("Review viewer settings before opening the guest display. ", 70) + "FINAL_RESOURCE"
	all := wrap(text, 80)
	var seen []string
	for offset := 0; offset < len(all); offset += 24 {
		page := pageLines([]string{text}, 80, 24, offset)
		if len(page) > 24 {
			t.Fatal("page exceeded 24 rows")
		}
		for _, line := range page {
			if ansi.StringWidth(line) > 80 {
				t.Fatalf("page exceeded 80 columns: %q", line)
			}
		}
		// pageLines clamps the final page back to a complete viewport. Its
		// overlap is intentional; compare each displayed row to its source.
		start := min(offset, max(0, len(all)-24))
		if !reflect.DeepEqual(page, all[start:min(len(all), start+24)]) {
			t.Fatal("paging changed wrapped content")
		}
		seen = append(seen, page...)
	}
	if !strings.Contains(strings.Join(seen, "\n"), "FINAL_RESOURCE") {
		t.Fatal("last review content became unreachable")
	}
	m := NewWorkspace(nil, "qemu:///system")
	m.NoColor = true
	m.Width, m.Height = 80, 24
	view := m.View()
	if len(strings.Split(strings.TrimSuffix(view, "\n"), "\n")) > 24 {
		t.Fatal("workspace exceeded 24 rows")
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > 80 {
			t.Fatalf("workspace exceeded 80 columns: %q", line)
		}
	}
}
