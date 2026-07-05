// Package guide embeds the orchestrator guide so documentation travels with
// the binary — `legwork guide` works on any machine the tool reaches,
// including over ssh (DESIGN.md §12).
package guide

import _ "embed"

//go:embed guide.md
var Text string
