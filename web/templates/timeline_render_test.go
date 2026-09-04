package templates

import (
	"context"
	"strings"
	"testing"

	"github.com/dusto/tend/api"
)

func TestTLArtifactRendersMarkdownFile(t *testing.T) {
	var b strings.Builder
	art := api.ArtifactWritten{URI: "file:///repo/notes.md", Content: "# Title\n\n- a", Diff: "+# Title"}
	if err := TLArtifact(art, "").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "notes.md") {
		t.Errorf("artifact missing filename: %q", out)
	}
	// A markdown file renders as markdown, not as a raw diff.
	if !strings.Contains(out, "<h1") || !strings.Contains(out, "<li>a") {
		t.Errorf("markdown artifact not rendered: %q", out)
	}
	if strings.Contains(out, "art-diff") {
		t.Errorf("markdown file should render markdown, not the diff: %q", out)
	}
}

func TestTLArtifactRendersCodeDiff(t *testing.T) {
	var b strings.Builder
	art := api.ArtifactWritten{URI: "file:///repo/main.go", Diff: "@@ -1 +1 @@\n-old\n+new"}
	if err := TLArtifact(art, "").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "art-diff") {
		t.Errorf("code artifact should show a diff: %q", out)
	}
	if !strings.Contains(out, "d-add") || !strings.Contains(out, "d-del") {
		t.Errorf("diff lines not colored: %q", out)
	}
}

func TestTLArtifactRichShowsSandboxNote(t *testing.T) {
	var b strings.Builder
	art := api.ArtifactWritten{URI: "file:///repo/diagram.svg", Diff: "+<svg></svg>"}
	if err := TLArtifact(art, "").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()
	// SVG source shows as a diff, plus a note that the render lands in the sandbox.
	if !strings.Contains(out, "art-diff") || !strings.Contains(out, "sandbox") {
		t.Errorf("rich artifact should show source diff + sandbox note: %q", out)
	}
	// It must NOT inline the SVG (that would execute in the shell).
	if strings.Contains(out, "<svg>") {
		t.Errorf("SVG must not be inlined in the shell: %q", out)
	}
}

func TestTLArtifactTruncatedNote(t *testing.T) {
	var b strings.Builder
	art := api.ArtifactWritten{URI: "file:///repo/big.txt", Diff: "@@ big @@", Truncated: true}
	if err := TLArtifact(art, "").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(b.String(), "content omitted") {
		t.Errorf("truncated artifact should note omitted content: %q", b.String())
	}
}

func TestTLMessageRendersMarkdown(t *testing.T) {
	var b strings.Builder
	if err := TLMessage("**bold** and `code`\n\n- item").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "<strong>bold</strong>") {
		t.Errorf("agent markdown not rendered (bold): %q", out)
	}
	if !strings.Contains(out, "<code>code</code>") || !strings.Contains(out, "<li>item") {
		t.Errorf("agent markdown not rendered (code/list): %q", out)
	}
	// The HTML must be injected raw, not escaped into visible text.
	if strings.Contains(out, "&lt;strong&gt;") {
		t.Errorf("markdown HTML was escaped instead of injected: %q", out)
	}
}

func TestTLMessageSanitizesScript(t *testing.T) {
	var b strings.Builder
	if err := TLMessage("hi <script>alert(1)</script>").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(b.String(), "<script") {
		t.Errorf("script leaked into rendered agent message: %q", b.String())
	}
}

func TestTLArtifactRendersSandboxIframe(t *testing.T) {
	var b strings.Builder
	art := api.ArtifactWritten{URI: "file:///repo/page.html", Content: "<h1>x</h1>", Diff: "+<h1>x</h1>"}
	if err := TLArtifact(art, "http://127.0.0.1:9/tok/a/abc").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, `<iframe`) || !strings.Contains(out, `src="http://127.0.0.1:9/tok/a/abc"`) {
		t.Errorf("expected a sandboxed iframe pointing at the preview url: %q", out)
	}
	if !strings.Contains(out, `sandbox="allow-scripts"`) {
		t.Errorf("iframe must be sandboxed (allow-scripts, no allow-same-origin): %q", out)
	}
	// The raw HTML must not be inlined in the shell — only framed.
	if strings.Contains(out, "<h1>x</h1>") {
		t.Errorf("agent HTML must not be inlined in the shell: %q", out)
	}
	if !strings.Contains(out, "open in browser") {
		t.Errorf("expected the open-in-browser escape hatch: %q", out)
	}
}
