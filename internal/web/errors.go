package web

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/vladtrc/pdtt/internal/jobs"
)

const visibleCopySourceClass = "max-h-64 overflow-auto rounded-box bg-base-300 p-3 font-mono text-sm leading-relaxed text-base-content whitespace-pre-wrap break-words"

type copyCard struct {
	alertClass string
	title      string
	detail     string
	sources    []copySource
	actions    []copyAction
}

type copySource struct {
	name    string
	text    string
	visible bool
}

type copyAction struct {
	label  string
	source string
}

func writeGenerateError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<div class="alert alert-error"><pre class="text-sm whitespace-pre-wrap">%s</pre></div>`,
		html.EscapeString(msg))
}

func (s *Server) writeRenderError(w http.ResponseWriter, status int, msg string) {
	writeCopyCard(w, status, copyCard{
		alertClass: "alert-error",
		title:      "Compilation failed",
		detail:     "Copy the compiler error or the PDTT LLM docs and paste them into Claude.",
		sources: []copySource{
			{name: "error", text: msg, visible: true},
			{name: "docs", text: s.pdttLLMDocs()},
		},
		actions: []copyAction{
			{label: "copy error", source: "error"},
			{label: "copy LLM docs", source: "docs"},
		},
	})
}

func (s *Server) writeLLMFailure(w http.ResponseWriter, ctx context.Context, status int, prompt, genID, msg string) {
	docs := s.pdttLLMDocs()
	payload := llmFailureCopyText(prompt, msg, docs, s.lastGenerationAttempt(ctx, genID))
	writeCopyCard(w, status, copyCard{
		alertClass: "alert-warning",
		title:      "LLM could not produce valid PDTT",
		detail:     "You can copy the last attempt plus the error and ask Claude to fix it.",
		sources: []copySource{
			{name: "attempt", text: payload},
			{name: "docs", text: docs},
		},
		actions: []copyAction{
			{label: "copy last attempt + error", source: "attempt"},
			{label: "copy LLM docs", source: "docs"},
		},
	})
}

func writeCopyCard(w http.ResponseWriter, status int, card copyCard) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	var b strings.Builder
	b.WriteString(copyScript())
	_, _ = fmt.Fprintf(&b, `<div class="alert %s shadow-lg items-start" data-copy-card>
<div class="flex w-full flex-col gap-3">
  <div>
    <div class="font-semibold">%s</div>
    <div class="text-sm opacity-80">%s</div>
  </div>`,
		html.EscapeString(card.alertClass),
		html.EscapeString(card.title),
		html.EscapeString(card.detail),
	)
	for _, source := range card.sources {
		writeCopySource(&b, source)
	}
	if len(card.actions) > 0 {
		b.WriteString(`<div class="flex flex-wrap gap-2">`)
		for _, action := range card.actions {
			writeCopyButton(&b, action)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div></div>`)
	_, _ = w.Write([]byte(b.String()))
}

func writeCopySource(b *strings.Builder, source copySource) {
	class := "hidden"
	if source.visible {
		class = visibleCopySourceClass
	}
	_, _ = fmt.Fprintf(b, `<pre class="%s" data-copy-source="%s">%s</pre>`,
		class,
		html.EscapeString(source.name),
		html.EscapeString(source.text),
	)
}

func writeCopyButton(b *strings.Builder, action copyAction) {
	_, _ = fmt.Fprintf(
		b,
		`<button type="button" class="btn btn-xs btn-outline" onclick="pdttCopyFrom(this, '%s')">%s</button>`,
		html.EscapeString(action.source),
		html.EscapeString(action.label),
	)
}

func (s *Server) lastGenerationAttempt(ctx context.Context, genID string) *jobs.GenerationAttempt {
	if s.store == nil || genID == "" {
		return nil
	}
	attempt, err := s.store.LastGenerationAttempt(ctx, genID)
	if err != nil {
		return nil
	}
	return attempt
}

func (s *Server) pdttLLMDocs() string {
	return loadPDTTLLMDocs(s.cfg.OpenRouter)
}

func llmFailureCopyText(prompt, msg, docs string, attempt *jobs.GenerationAttempt) string {
	var b strings.Builder
	b.WriteString("The LLM could not produce valid PDTT. Please fix the last attempt and return only a complete PDTT scene.\n\nUser prompt:\n")
	b.WriteString(prompt)
	if attempt != nil {
		if strings.TrimSpace(attempt.ExtractedScene) != "" {
			b.WriteString("\n\nLast extracted PDTT attempt:\n")
			b.WriteString(attempt.ExtractedScene)
		} else if strings.TrimSpace(attempt.ResponseContent) != "" {
			b.WriteString("\n\nLast raw LLM response:\n")
			b.WriteString(attempt.ResponseContent)
		}
		if strings.TrimSpace(attempt.ValidationError) != "" {
			b.WriteString("\n\nValidation error:\n")
			b.WriteString(attempt.ValidationError)
		}
		if strings.TrimSpace(attempt.OpenRouterError) != "" {
			b.WriteString("\n\nOpenRouter error:\n")
			b.WriteString(attempt.OpenRouterError)
		}
	}
	b.WriteString("\n\nFinal error:\n")
	b.WriteString(msg)
	b.WriteString("\n\nPDTT LLM docs:\n")
	b.WriteString(docs)
	return b.String()
}

func copyScript() string {
	return `<script>
window.pdttCopyFrom = window.pdttCopyFrom || function(button, name) {
  const source = button.closest("[data-copy-card]")?.querySelector('[data-copy-source="' + name + '"]');
  const text = source?.textContent || "";
  const done = () => {
    const previous = button.textContent;
    button.textContent = "copied";
    setTimeout(() => { button.textContent = previous; }, 1200);
  };
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(text).then(done);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  document.execCommand("copy");
  textarea.remove();
  done();
};
</script>`
}
