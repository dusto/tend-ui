package markdown

import (
	"strings"
	"testing"
)

func TestRendersCommonMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // substring expected in the output
	}{
		{"heading", "# Title", "<h1"},
		{"bold", "**bold**", "<strong>bold</strong>"},
		{"italic", "*it*", "<em>it</em>"},
		{"unordered list", "- a\n- b", "<ul>"},
		{"ordered list", "1. a\n2. b", "<ol>"},
		{"inline code", "use `x`", "<code>x</code>"},
		{"blockquote", "> quoted", "<blockquote>"},
		{"table", "| a | b |\n|---|---|\n| 1 | 2 |", "<table>"},
		{"strikethrough", "~~gone~~", "<del>gone</del>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ToSafeHTML(tc.in)
			if !strings.Contains(got, tc.want) {
				t.Errorf("ToSafeHTML(%q) = %q, want to contain %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFencedCodeKeepsLanguageClass(t *testing.T) {
	got := ToSafeHTML("```go\nfmt.Println(1)\n```")
	if !strings.Contains(got, `class="language-go"`) {
		t.Errorf("fenced code lost its language class: %q", got)
	}
	if !strings.Contains(got, "<pre>") {
		t.Errorf("fenced code not wrapped in <pre>: %q", got)
	}
}

func TestMermaidKeptAsLabelledCodeBlock(t *testing.T) {
	// Mermaid must NOT be executed here; its source is preserved as a code block
	// (language class kept so the shell can label it) for the sandbox to render.
	got := ToSafeHTML("```mermaid\ngraph TD; A-->B;\n```")
	if !strings.Contains(got, `class="language-mermaid"`) {
		t.Errorf("mermaid language class not preserved: %q", got)
	}
	if strings.Contains(got, "<svg") || strings.Contains(got, "<script") {
		t.Errorf("mermaid must not be rendered/executed inline: %q", got)
	}
	if !strings.Contains(got, "graph TD") {
		t.Errorf("mermaid source not preserved: %q", got)
	}
}

func TestStripsUnsafeContent(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		absent  string // must NOT appear
		present string // optional: must still appear ("" to skip)
	}{
		{"script tag", "hi <script>alert(1)</script>", "<script", "hi"},
		{"img onerror", "<img src=x onerror=alert(1)>", "onerror", ""},
		{"javascript link", "[click](javascript:alert(1))", "javascript:", "click"},
		{"onclick attr", `<a href="https://x.com" onclick="steal()">y</a>`, "onclick", ""},
		{"data uri", "[x](data:text/html,<script>alert(1)</script>)", "data:text/html", ""},
		// Raw HTML blocks are omitted wholesale (goldmark Unsafe off), so the handler
		// never even reaches the sanitizer — the safest outcome.
		{"raw html div", "<div onmouseover=alert(1)>x</div>", "onmouseover", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ToSafeHTML(tc.in)
			if strings.Contains(got, tc.absent) {
				t.Errorf("ToSafeHTML(%q) leaked %q: %q", tc.in, tc.absent, got)
			}
			if tc.present != "" && !strings.Contains(got, tc.present) {
				t.Errorf("ToSafeHTML(%q) dropped %q: %q", tc.in, tc.present, got)
			}
		})
	}
}

func TestSafeLinkGetsNofollowAndTarget(t *testing.T) {
	got := ToSafeHTML("[site](https://example.com)")
	if !strings.Contains(got, `href="https://example.com"`) {
		t.Fatalf("safe link href not kept: %q", got)
	}
	if !strings.Contains(got, `rel=`) || !strings.Contains(got, "nofollow") {
		t.Errorf("link missing rel nofollow: %q", got)
	}
	if !strings.Contains(got, `target="_blank"`) {
		t.Errorf("link missing target _blank (so it can't hijack the shell): %q", got)
	}
}

func TestEmptyInput(t *testing.T) {
	if ToSafeHTML("") != "" || ToSafeHTML("   \n  ") != "" {
		t.Errorf("empty/blank input should render empty")
	}
}
