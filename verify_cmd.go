package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/verify"
)

type verifyOutput struct {
	OK      bool                     `json:"ok"`
	State   string                   `json:"state,omitempty"`
	Receipt *job.VerificationReceipt `json:"receipt,omitempty"`
	Retry   []string                 `json:"retry,omitempty"`
	Blocked *verifyBlocked           `json:"blocked,omitempty"`
}

type verifyBlocked struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

func verifyCmd() *cobra.Command {
	var timeout string
	var asJSON bool
	c := &cobra.Command{
		Use:   "verify <job> -- <argv...>",
		Short: "Run an explicit host-side verification for a blocked workspace job",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.ArgsLenAtDash() != 1 {
				return fmt.Errorf("verification command must follow -- as argv")
			}
			d, err := time.ParseDuration(timeout)
			if err != nil || d <= 0 {
				return fmt.Errorf("--timeout must be a positive duration: %q", timeout)
			}
			s, wss, err := openWorkspaces()
			if err != nil {
				return err
			}
			m, wm, err := verify.Preconditions(s, wss, args[0])
			if err != nil {
				return verifyError(asJSON, args[0], err)
			}
			lease, err := verify.AcquireLease(s, m.ID, d)
			if err != nil {
				return verifyError(asJSON, m.ID, err)
			}

			r, startErr := verify.Run(m.ID, wm.ID, wm.Tree, m.Turns, args[1:], d)
			if startErr != nil {
				verify.ClearLease(s, m.ID, lease.ID)
				verify.AppendStartRefusal(s, m.ID, wm.ID, m.Turns, args[1:], startErr)
				return verifyRefusal(asJSON, m.ID, verify.KindCommandStart, fmt.Sprintf("verification command could not start: %v", startErr))
			}

			// The command ran against the live worktree. Snapshot it immediately
			// afterwards so a receipt names one immutable tested tree.
			if freshWM, loadErr := wss.Load(wm.ID); loadErr == nil {
				wm = freshWM
				if snap, snapErr := wss.ReviewSnapshot(wm); snapErr == nil {
					r.CheckpointRef, r.CheckpointOID, r.DiffSHA256 = snap.CheckpointRef, snap.CheckpointOID, snap.DiffSHA256
				} else {
					r.HistoryError = verify.AppendHistoryError(r.HistoryError, "post-command checkpoint: "+snapErr.Error())
				}
			} else {
				r.HistoryError = verify.AppendHistoryError(r.HistoryError, "reload workspace: "+loadErr.Error())
			}

			promoted, finishErr := verify.Finish(s, wss, r, lease)
			if finishErr != nil {
				return finishErr
			}
			if promoted {
				verify.SendNotification(s, m, r)
			}
			out := verifyOutput{OK: promoted && r.Passed, State: "completed", Receipt: r, Retry: verify.RetryArgv(m.ID, r.Argv)}
			if !promoted {
				out.State = "stale"
			}
			if asJSON {
				_ = printJSON(out)
			} else {
				printVerification(out)
			}
			if !out.OK {
				return commandError{code: 1, silent: true}
			}
			return nil
		},
	}
	c.Flags().StringVar(&timeout, "timeout", "30m", "wall-clock limit for the verification command")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func verifyError(asJSON bool, id string, err error) error {
	kind := verify.KindPrecondition
	var ve *verify.Error
	if errors.As(err, &ve) {
		kind = ve.Kind
	}
	return verifyRefusal(asJSON, id, kind, err.Error())
}

func verifyRefusal(asJSON bool, jobID, kind, detail string) error {
	if asJSON {
		_ = printJSON(verifyOutput{OK: false, State: "blocked", Blocked: &verifyBlocked{Kind: kind, Detail: detail}})
	} else {
		fmt.Fprintln(os.Stderr, "legwork: "+detail)
	}
	return commandError{code: 2, silent: true}
}

func printVerification(out verifyOutput) {
	if out.Receipt == nil {
		return
	}
	state := "failed"
	if out.Receipt.Passed {
		state = "passed"
	} else if out.Receipt.TimedOut {
		state = "timed out"
	}
	if out.State == "stale" {
		state += " (stale; not current)"
	}
	fmt.Printf("verification %s: %s (%dms)\n", state, out.Receipt.ReceiptID, out.Receipt.DurationMS)
	if out.Receipt.Output != "" {
		fmt.Print(out.Receipt.Output)
		if !strings.HasSuffix(out.Receipt.Output, "\n") {
			fmt.Println()
		}
	}
	if out.Receipt.OutputCut {
		fmt.Printf("output truncated at %d bytes\n", verify.OutputLimit)
	}
	fmt.Printf("retry: %s\n", shellCommand(out.Retry))
	if out.Receipt.HistoryError != "" {
		fmt.Printf("history warning: %s\n", out.Receipt.HistoryError)
	}
}

func shellCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		if arg == "" {
			quoted[i] = "''"
		} else if strings.ContainsAny(arg, " \t\n'\"\\$&;|<>()*?[]{}!") {
			quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
		} else {
			quoted[i] = arg
		}
	}
	return strings.Join(quoted, " ")
}
