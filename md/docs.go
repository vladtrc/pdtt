package md

import _ "embed"

// Full is the complete PDTT reference for LLM scene generation.
//
//go:embed full.md
var Full string

// WebHarness describes the web playground execution environment and style rules
// appended to LLM scene-generation prompts.
//
//go:embed web_harness.md
var WebHarness string
