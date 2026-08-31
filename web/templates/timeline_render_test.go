package templates

import (
	"context"
	"strings"
	"testing"
)

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
