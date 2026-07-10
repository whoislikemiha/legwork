package job

import (
	"errors"
	"fmt"
	"os"
)

// SelectorKind identifies the namespace a read-side selector resolved in.
// Job IDs take precedence for automatic resolution; --job and --run opt into
// either namespace when a label deliberately collides with a job ID.
type SelectorKind string

const (
	SelectorJob SelectorKind = "job"
	SelectorRun SelectorKind = "run"
)

// Selection is the common result of resolving a job ID or run label. Jobs is
// one element for a job selection and every job in a run selection; a run with
// only notes or artifacts legitimately has no jobs.
type Selection struct {
	Selector string
	Kind     SelectorKind
	Jobs     []*Meta
}

// Newest returns the most recently created job in the selection. Ties are
// broken by updated time, matching the historic result <run> behavior.
func (s Selection) Newest() *Meta {
	if len(s.Jobs) == 0 {
		return nil
	}
	newest := s.Jobs[0]
	for _, m := range s.Jobs[1:] {
		if m.Created.After(newest.Created) ||
			(m.Created.Equal(newest.Created) && m.Updated.After(newest.Updated)) {
			newest = m
		}
	}
	return newest
}

// Resolve selects a job or an existing run scope. A run exists when it has at
// least one job or an event log (notes and artifacts create the latter). When
// forced is empty, an exact job ID wins over a same-named run; callers use
// SelectorJob or SelectorRun for the explicit --job/--run namespaces.
func Resolve(s *Store, selector string, forced SelectorKind) (Selection, error) {
	if selector == "" {
		return Selection{}, fmt.Errorf("selector is required")
	}
	if forced != "" && forced != SelectorJob && forced != SelectorRun {
		return Selection{}, fmt.Errorf("unknown selector kind %q", forced)
	}

	if forced != SelectorRun {
		m, err := s.LoadMeta(selector)
		if err == nil {
			return Selection{Selector: selector, Kind: SelectorJob, Jobs: []*Meta{m}}, nil
		}
		if forced == SelectorJob {
			return Selection{}, err
		}
		// Automatic resolution falls through only when there is no job at all.
		// A corrupt or unreadable meta file is a real job lookup failure, not a
		// reason to silently reinterpret its ID as a run label.
		if !errors.Is(err, os.ErrNotExist) {
			return Selection{}, err
		}
	}

	if err := ValidateRunLabel(selector); err != nil {
		return Selection{}, err
	}
	metas, err := s.List()
	if err != nil {
		return Selection{}, err
	}
	jobs := make([]*Meta, 0)
	for _, m := range metas {
		if m.Run == selector {
			jobs = append(jobs, m)
		}
	}
	hasEvents, err := s.HasRunEvents(selector)
	if err != nil {
		return Selection{}, err
	}
	if len(jobs) == 0 && !hasEvents {
		return Selection{}, fmt.Errorf("no job or run event log %q; use --job <id> for an exact job", selector)
	}
	return Selection{Selector: selector, Kind: SelectorRun, Jobs: jobs}, nil
}
