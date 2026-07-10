package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/whoislikemiha/legwork/internal/job"
)

type reviewVerdict struct {
	Verdict  string          `json:"verdict"`
	Findings []reviewFinding `json:"findings"`
}

type reviewFinding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

// ParseReviewReceipt accepts either the exact JSON report requested by
// workspaceReviewPrompt or one such report in a fenced json block. Prose,
// missing fields, unknown fields, and ambiguous candidates remain explicit
// unparsed receipts rather than accidental SHIPs.
func ParseReviewReceipt(jm *job.Meta) *ReviewReceipt {
	r := &ReviewReceipt{
		Job: jm.ID, Agent: jm.Agent, Model: jm.Model, State: jm.State,
		CompletedAt: receiptTime(jm.Updated),
	}
	if jm.Review == nil {
		r.Parsed = false
		r.ParseError = "review dispatch receipt missing"
		return r
	}
	r.CheckpointRef = jm.Review.CheckpointRef
	r.CheckpointOID = jm.Review.CheckpointOID
	r.DiffSHA256 = jm.Review.DiffSHA256
	if jm.State != job.StateDone {
		r.ParseError = "review turn ended " + string(jm.State) + "; verdict is fail-closed"
		return r
	}

	report, err := parseReviewVerdict(jm.Result)
	if err != nil {
		r.ParseError = "invalid verdict JSON: " + err.Error()
		return r
	}
	if report.Findings == nil {
		r.ParseError = "invalid verdict JSON: findings is required"
		return r
	}
	report.Verdict = strings.ToUpper(strings.TrimSpace(report.Verdict))
	if report.Verdict != "SHIP" && report.Verdict != "FIX" {
		r.ParseError = "invalid verdict JSON: verdict must be SHIP or FIX"
		return r
	}
	for i := range report.Findings {
		finding := &report.Findings[i]
		finding.Severity = strings.ToLower(strings.TrimSpace(finding.Severity))
		if err := validateReviewFinding(*finding); err != nil {
			r.ParseError = "invalid verdict JSON: " + err.Error()
			return r
		}
		r.Findings.Total++
		switch finding.Severity {
		case "critical":
			r.Findings.Critical++
		case "high":
			r.Findings.High++
		case "medium":
			r.Findings.Medium++
		case "low":
			r.Findings.Low++
		}
	}
	r.Parsed = true
	r.Verdict = report.Verdict
	return r
}

func receiptTime(updated time.Time) time.Time {
	if !updated.IsZero() {
		return updated
	}
	return time.Now().UTC()
}

func parseReviewVerdict(result string) (reviewVerdict, error) {
	trimmed := strings.TrimSpace(result)
	if strings.HasPrefix(trimmed, "{") {
		return decodeReviewVerdict(trimmed)
	}

	var candidates []string
	for rest := result; ; {
		start := strings.Index(rest, "```")
		if start < 0 {
			break
		}
		rest = rest[start+3:]
		lineEnd := strings.IndexByte(rest, '\n')
		if lineEnd < 0 {
			return reviewVerdict{}, fmt.Errorf("unterminated fenced block")
		}
		language := strings.TrimSpace(rest[:lineEnd])
		rest = rest[lineEnd+1:]
		end := strings.Index(rest, "```")
		if end < 0 {
			return reviewVerdict{}, fmt.Errorf("unterminated fenced block")
		}
		if strings.EqualFold(language, "json") {
			candidates = append(candidates, rest[:end])
		}
		rest = rest[end+3:]
	}
	if len(candidates) == 0 {
		return reviewVerdict{}, fmt.Errorf("expected an exact verdict object or one fenced json verdict")
	}
	if len(candidates) != 1 {
		return reviewVerdict{}, fmt.Errorf("ambiguous fenced json verdicts")
	}
	return decodeReviewVerdict(candidates[0])
}

func decodeReviewVerdict(candidate string) (reviewVerdict, error) {
	dec := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(candidate)))
	dec.DisallowUnknownFields()
	var report reviewVerdict
	if err := dec.Decode(&report); err != nil {
		return reviewVerdict{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return reviewVerdict{}, fmt.Errorf("trailing value")
	}
	return report, nil
}

func validateReviewFinding(f reviewFinding) error {
	if strings.TrimSpace(f.File) == "" {
		return fmt.Errorf("finding file is required")
	}
	if f.Line < 0 {
		return fmt.Errorf("finding line must be non-negative")
	}
	switch f.Severity {
	case "critical", "high", "medium", "low":
	default:
		return fmt.Errorf("finding severity is invalid")
	}
	if strings.TrimSpace(f.Detail) == "" {
		return fmt.Errorf("finding detail is required")
	}
	return nil
}

// RecordReview refreshes the workspace metadata before updating its current
// receipt, so review completion never overwrites a newer checkpoint/close
// update with the runner's stale workspace pointer.
func (s *Store) RecordReview(wsID string, receipt *ReviewReceipt) error {
	m, err := s.Load(wsID)
	if err != nil {
		return err
	}
	m.LatestReview = receipt
	return s.save(m)
}

// RecordVerification advances the workspace mirror then appends its history
// event. A history append failure is recorded on the already-durable receipt;
// callers must not replay a completed host command.
func (s *Store) RecordVerification(wsID string, receipt *job.VerificationReceipt) error {
	m, err := s.Load(wsID)
	if err != nil {
		return err
	}
	m.LatestVerification = receipt
	if err := s.save(m); err != nil {
		return err
	}
	if err := s.appendEvent(wsID, job.VerificationEvent(receipt)); err != nil {
		receipt.HistoryError = appendHistoryError(receipt.HistoryError,
			fmt.Sprintf("append workspace event %s: %v", s.eventPath(wsID), err))
		_ = s.save(m)
	}
	return nil
}

// ClearVerification removes only the current rollup belonging to a resumed
// job. Append-only receipt history intentionally remains intact.
func (s *Store) ClearVerification(wsID, jobID string) error {
	m, err := s.Load(wsID)
	if err != nil {
		return err
	}
	if m.LatestVerification == nil || m.LatestVerification.Job != jobID {
		return nil
	}
	m.LatestVerification = nil
	return s.save(m)
}
