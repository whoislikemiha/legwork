// Package transcript replays a job's retained transcript through its
// adapter's parser, recovering per-turn results after the fact for
// `result --turn`.
package transcript

import (
	"bufio"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"

	"github.com/whoislikemiha/legwork/internal/adapter"
	"github.com/whoislikemiha/legwork/internal/job"
)

// Results parses every retained turn result out of the job's transcript.
// A retired (gc'd) or never-written transcript yields nil, nil.
func Results(s *job.Store, m *job.Meta) ([]*adapter.TurnResult, error) {
	r, err := openRetained(s, m)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	defer r.Close()
	ad, err := adapter.New(m.Agent)
	if err != nil {
		return nil, err
	}
	parser := ad.Parser()
	var results []*adapter.TurnResult
	// transcript.jsonl also captures agent stderr. Non-JSON noise is ignored
	// by the adapters, so replaying it preserves the same parser behavior as
	// the live runner without treating the transcript as a clean stdout stream.
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)
	for sc.Scan() {
		_, res, perr := parser.Line(sc.Bytes())
		if perr != nil {
			continue
		}
		if res != nil {
			results = append(results, res)
			parser = ad.Parser()
		}
	}
	return results, sc.Err()
}

func openRetained(s *job.Store, m *job.Meta) (io.ReadCloser, error) {
	plain := filepath.Join(s.JobDir(m.ID), "transcript.jsonl")
	f, err := os.Open(plain)
	if err == nil {
		return f, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	gzf, err := os.Open(plain + ".gz")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	zr, err := gzip.NewReader(gzf)
	if err != nil {
		gzf.Close()
		return nil, err
	}
	return multiReadCloser{Reader: zr, closers: []io.Closer{zr, gzf}}, nil
}

type multiReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (m multiReadCloser) Close() error {
	var first error
	for _, c := range m.closers {
		if err := c.Close(); first == nil && err != nil {
			first = err
		}
	}
	return first
}
