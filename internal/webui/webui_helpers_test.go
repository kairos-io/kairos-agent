package webui

import (
	"bytes"
	"text/template"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WebUI helpers", func() {
	Describe("stripANSI", func() {
		It("strips ANSI escape codes", func() {
			cases := []struct {
				in, want string
			}{
				{"", ""},
				{"no codes here", "no codes here"},
				{"\x1b[31mred\x1b[0m", "red"},
				{"a\x1b[1mB\x1b[0mc\x1b[32mD\x1b[0m", "aBcD"},
			}
			for _, c := range cases {
				Expect(stripANSI(c.in)).To(Equal(c.want), "stripANSI(%q)", c.in)
			}
		})
	})

	Describe("ansiToHTML", func() {
		It("HTML-escapes special chars when there are no codes", func() {
			// Without ANSI codes the function must HTML-escape special chars and
			// return the escaped string.
			Expect(ansiToHTML("a<b>&c")).To(Equal("a&lt;b&gt;&amp;c"))
		})

		It("wraps colored text in spans", func() {
			got := ansiToHTML("\x1b[31mRED\x1b[0mnorm\x1b[32mGREEN\x1b[0m")
			Expect(got).To(ContainSubstring("color: #ff5555"), "missing red color: %q", got)
			Expect(got).To(ContainSubstring(">RED<"), "RED text not wrapped: %q", got)
			Expect(got).To(ContainSubstring("color: #50fa7b"), "missing green color: %q", got)
			Expect(got).To(ContainSubstring("norm"), "norm text missing: %q", got)
		})

		It("renders bold text with a bold style", func() {
			got := ansiToHTML("\x1b[1mBOLD\x1b[0m")
			Expect(got).To(ContainSubstring("font-weight: bold"), "missing bold style: %q", got)
		})

		It("maps bright colors", func() {
			got := ansiToHTML("\x1b[91mBRIGHT\x1b[0m")
			Expect(got).To(ContainSubstring("color: #ff6e6e"), "missing bright red: %q", got)
		})

		It("HTML-escapes text inside spans", func() {
			got := ansiToHTML("\x1b[31m<x&y>\x1b[0m")
			Expect(got).To(ContainSubstring("&lt;x&amp;y&gt;"), "html not escaped inside span: %q", got)
		})

		It("keeps text before an ANSI code and after the last code", func() {
			// text before an ANSI code + text after last code (both branches).
			got := ansiToHTML("prefix\x1b[31mmiddle\x1b[0msuffix")
			for _, s := range []string{"prefix", "middle", "suffix"} {
				Expect(got).To(ContainSubstring(s), "missing %q in %q", s, got)
			}
		})

		It("treats an empty code as reset", func() {
			// `\x1b[m` (empty payload) is treated as `0` (reset) per the code path
			// that substitutes "" → "0".
			Expect(ansiToHTML("\x1b[m")).To(BeEmpty())
		})
	})

	Describe("formatLogLine", func() {
		It("trims whitespace and colorizes the payload", func() {
			Expect(formatLogLine("")).To(BeEmpty(), "empty line should stay empty")
			Expect(formatLogLine("   \t\n")).To(BeEmpty(), "whitespace-only line should be empty")
			got := formatLogLine("  \x1b[32mhello\x1b[0m  ")
			Expect(got).To(ContainSubstring("hello"), "payload missing from %q", got)
			Expect(got).To(ContainSubstring("color: #50fa7b"), "color missing from %q", got)
		})
	})

	Describe("TemplateRenderer", func() {
		It("renders a template by name", func() {
			tmpl := template.Must(template.New("hello.html").Parse("Hi {{.Name}}!"))
			r := &TemplateRenderer{templates: tmpl}
			var buf bytes.Buffer
			// echo.Context is only used by Echo internally; Render only touches the
			// writer with the template output, so passing nil is safe here.
			Expect(r.Render(nil, &buf, "hello.html", map[string]string{"Name": "Kairos"})).To(Succeed())
			Expect(buf.String()).To(Equal("Hi Kairos!"))
		})
	})

	Describe("embedded filesystem accessors", func() {
		It("returns non-nil filesystems from getFileSystem and getFS", func() {
			Expect(getFileSystem()).ToNot(BeNil())
			Expect(getFS()).ToNot(BeNil())
		})
	})
})
