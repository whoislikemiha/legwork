package main

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

var (
	commit = ""
	date   = ""
	dirty  = ""
)

type versionOut struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Dirty   bool   `json:"dirty"`
	Date    string `json:"date"`
}

func buildVersion() versionOut {
	out := versionOut{Version: version, Commit: commit, Dirty: parseDirty(dirty), Date: date}
	if out.Version == "" {
		out.Version = "dev"
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		settings := map[string]string{}
		for _, s := range info.Settings {
			settings[s.Key] = s.Value
		}
		if out.Commit == "" {
			out.Commit = settings["vcs.revision"]
		}
		if out.Date == "" {
			out.Date = settings["vcs.time"]
		}
		if dirty == "" {
			out.Dirty = settings["vcs.modified"] == "true"
		}
	}
	out.Commit = shortCommit(out.Commit)
	return out
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

func parseDirty(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "dirty", "modified":
		return true
	default:
		return false
	}
}

func versionSummary() string {
	v := buildVersion()
	parts := []string{v.Version}
	if v.Commit != "" {
		commitPart := v.Commit
		if v.Dirty {
			commitPart += "-dirty"
		}
		parts = append(parts, "commit "+commitPart)
	}
	if v.Date != "" {
		parts = append(parts, "date "+v.Date)
	}
	return strings.Join(parts, ", ")
}

func versionCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "version",
		Short: "Print build version, commit, dirty flag, and date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v := buildVersion()
			if asJSON {
				return printJSON(v)
			}
			fmt.Printf("version: %s\ncommit: %s\ndirty: %t\ndate: %s\n",
				v.Version, v.Commit, v.Dirty, v.Date)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}
