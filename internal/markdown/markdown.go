// Package markdown renders agent and user message text as SAFE inline HTML for
// the session timeline. Agent output is untrusted, so this is a two-step
// pipeline: goldmark turns markdown into HTML, then bluemonday sanitizes it to an
// inert allowlist — headings, emphasis, lists, links, tables, and inline/fenced
// code — with no scripts, event handlers, or dangerous URL schemes. This is the
// SAFE tier (tend-du1.15): it stays in the privileged shell precisely because it
// is reduced to inert HTML.
//
// RICH or executable content (mermaid diagrams, raw HTML, images, webviews) is
// deliberately NOT rendered here — it belongs in the sandboxed preview surface on
// a separate origin (tend-du1.9, ADR 0005). A fenced ```mermaid block is left as
// a labelled code block (its language class preserved) so its source is readable;
// actually drawing it is the sandbox's job.
package markdown

import (
	"regexp"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// md is the shared goldmark renderer. GFM adds tables, strikethrough, task lists,
// and autolinks; hard-wraps keep single newlines as <br> so agent prose that
// relies on line breaks reads as written. It emits no raw HTML (Unsafe stays
// off), so any HTML the agent typed is escaped rather than passed through.
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithHardWraps()),
)

// classAllow permits only language-* code classes (goldmark's fenced-block
// annotation, including language-mermaid) to survive sanitization — enough to
// label a code block, nothing that can style-inject.
var classAllow = regexp.MustCompile(`^language-[\w-]+$`)

var (
	policyOnce sync.Once
	policy     *bluemonday.Policy
)

// sanitizer builds (once) the allowlist policy for rendered markdown: the inline
// and block elements goldmark produces, safe links only, and language classes on
// code. Images and raw HTML are intentionally excluded (rich content is the
// sandbox's job).
func sanitizer() *bluemonday.Policy {
	policyOnce.Do(func() {
		p := bluemonday.NewPolicy()
		p.AllowElements(
			"p", "br", "hr", "blockquote",
			"h1", "h2", "h3", "h4", "h5", "h6",
			"ul", "ol", "li",
			"pre", "code", "em", "strong", "del", "span",
			"table", "thead", "tbody", "tr", "th", "td",
		)
		// Safe links only: http/https/mailto, no-follow, opened out of the shell.
		p.AllowAttrs("href").OnElements("a")
		p.AllowURLSchemes("http", "https", "mailto")
		p.RequireNoFollowOnLinks(true)
		p.AddTargetBlankToFullyQualifiedLinks(true)
		// The fenced-block language annotation, for styling/labels (incl. mermaid).
		p.AllowAttrs("class").Matching(classAllow).OnElements("code")
		policy = p
	})
	return policy
}

// ToSafeHTML renders untrusted markdown to sanitized, inert HTML. It returns ""
// for empty input; on a render error it falls back to the escaped raw text so no
// content is lost and nothing unsafe leaks.
func ToSafeHTML(source string) string {
	if strings.TrimSpace(source) == "" {
		return ""
	}
	var buf strings.Builder
	if err := md.Convert([]byte(source), &buf); err != nil {
		// Escape-and-return so the text still shows, safely.
		return sanitizer().Sanitize(source)
	}
	return sanitizer().Sanitize(buf.String())
}
