package timeline

import "github.com/whoislikemiha/legwork/internal/events"

// curated is the significance set: the events a human watching a pipeline wants
// by default — lifecycle, narration, and the coarse worker signals (text,
// checkpoint). The complement (tool-call, progress, usage) is the firehose,
// surfaced only by `tail --full` and the dashboard's detail drill-in. One
// definition so every surface agrees on what "significant" means.
var curated = map[string]bool{
	events.TypeQueued:      true,
	events.TypeStarted:     true,
	events.TypeFinished:    true,
	events.TypeInterrupted: true,
	events.TypeNeedsInput:  true,
	events.TypeAnswer:      true,
	events.TypeResume:      true,
	events.TypeCancel:      true,
	events.TypeClosed:      true,
	events.TypeNote:        true,
	events.TypeText:        true,
	events.TypeCheckpoint:  true,
}

// IsCurated reports whether an event type is in the curated (significant) set.
func IsCurated(eventType string) bool { return curated[eventType] }

// Curated filters items to the significant set, preserving order.
func Curated(items []Item) []Item {
	out := items[:0:0]
	for _, it := range items {
		if IsCurated(it.Event.Type) {
			out = append(out, it)
		}
	}
	return out
}
