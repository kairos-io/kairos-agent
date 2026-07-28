package webui

import (
	"bytes"
	"strings"
	"testing"
	"text/template"
)

func TestStripANSI(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"no codes here", "no codes here"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"a\x1b[1mB\x1b[0mc\x1b[32mD\x1b[0m", "aBcD"},
	}
	for _, c := range cases {
		if got := stripANSI(c.in); got != c.want {
			t.Errorf("stripANSI(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestAnsiToHTML_NoCodes(t *testing.T) {
	// Without ANSI codes the function must HTML-escape special chars and
	// return the escaped string.
	got := ansiToHTML("a<b>&c")
	if got != "a&lt;b&gt;&amp;c" {
		t.Fatalf("got %q", got)
	}
}

func TestAnsiToHTML_ColorSpans(t *testing.T) {
	got := ansiToHTML("\x1b[31mRED\x1b[0mnorm\x1b[32mGREEN\x1b[0m")
	if !strings.Contains(got, "color: #ff5555") {
		t.Errorf("missing red color: %q", got)
	}
	if !strings.Contains(got, ">RED<") {
		t.Errorf("RED text not wrapped: %q", got)
	}
	if !strings.Contains(got, "color: #50fa7b") {
		t.Errorf("missing green color: %q", got)
	}
	if !strings.Contains(got, "norm") {
		t.Errorf("norm text missing: %q", got)
	}
}

func TestAnsiToHTML_Bold(t *testing.T) {
	got := ansiToHTML("\x1b[1mBOLD\x1b[0m")
	if !strings.Contains(got, "font-weight: bold") {
		t.Fatalf("missing bold style: %q", got)
	}
}

func TestAnsiToHTML_BrightColor(t *testing.T) {
	got := ansiToHTML("\x1b[91mBRIGHT\x1b[0m")
	if !strings.Contains(got, "color: #ff6e6e") {
		t.Fatalf("missing bright red: %q", got)
	}
}

func TestAnsiToHTML_HTMLEscapesInsideSpan(t *testing.T) {
	got := ansiToHTML("\x1b[31m<x&y>\x1b[0m")
	if !strings.Contains(got, "&lt;x&amp;y&gt;") {
		t.Fatalf("html not escaped inside span: %q", got)
	}
}

func TestAnsiToHTML_TrailingTextAfterCode(t *testing.T) {
	// text before an ANSI code + text after last code (both branches).
	got := ansiToHTML("prefix\x1b[31mmiddle\x1b[0msuffix")
	for _, s := range []string{"prefix", "middle", "suffix"} {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in %q", s, got)
		}
	}
}

func TestAnsiToHTML_EmptyCodeDefaultsToReset(t *testing.T) {
	// `\x1b[m` (empty payload) is treated as `0` (reset) per the code path
	// that substitutes "" → "0".
	got := ansiToHTML("\x1b[m")
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestFormatLogLine(t *testing.T) {
	if got := formatLogLine(""); got != "" {
		t.Errorf("empty line should stay empty, got %q", got)
	}
	if got := formatLogLine("   \t\n"); got != "" {
		t.Errorf("whitespace-only line should be empty, got %q", got)
	}
	got := formatLogLine("  \x1b[32mhello\x1b[0m  ")
	if !strings.Contains(got, "hello") {
		t.Errorf("payload missing from %q", got)
	}
	if !strings.Contains(got, "color: #50fa7b") {
		t.Errorf("color missing from %q", got)
	}
}

func TestTemplateRenderer_Render(t *testing.T) {
	tmpl := template.Must(template.New("hello.html").Parse("Hi {{.Name}}!"))
	r := &TemplateRenderer{templates: tmpl}
	var buf bytes.Buffer
	// echo.Context is only used by Echo internally; Render only touches the
	// writer with the template output, so passing nil is safe here.
	if err := r.Render(nil, &buf, "hello.html", map[string]string{"Name": "Kairos"}); err != nil {
		t.Fatalf("Render err: %v", err)
	}
	if buf.String() != "Hi Kairos!" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestGetFileSystem_And_GetFS(t *testing.T) {
	if getFileSystem() == nil {
		t.Fatal("getFileSystem returned nil")
	}
	if getFS() == nil {
		t.Fatal("getFS returned nil")
	}
}
