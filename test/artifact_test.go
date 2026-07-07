package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactSaveListGetAndJSON(t *testing.T) {
	e := newEnv(t)
	src := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(src, []byte("# Plan\n\nShip artifacts.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var saved map[string]any
	if err := json.Unmarshal([]byte(e.legwork(t, "artifact", "save",
		"--run", "pipe", "--name", "plan.md", "--json", src)), &saved); err != nil {
		t.Fatalf("save json: %v", err)
	}
	if saved["run"] != "pipe" || saved["name"] != "plan.md" || saved["size_bytes"].(float64) == 0 {
		t.Fatalf("unexpected save metadata: %+v", saved)
	}
	if strings.Contains(saved["path"].(string), "workspaces") {
		t.Fatalf("artifact path should live in run state, got %q", saved["path"])
	}

	var listed struct {
		Run       string           `json:"run"`
		Artifacts []map[string]any `json:"artifacts"`
	}
	if err := json.Unmarshal([]byte(e.legwork(t, "artifact", "list",
		"--run", "pipe", "--json")), &listed); err != nil {
		t.Fatalf("list json: %v", err)
	}
	if listed.Run != "pipe" || len(listed.Artifacts) != 1 || listed.Artifacts[0]["name"] != "plan.md" {
		t.Fatalf("unexpected list output: %+v", listed)
	}

	got := e.legwork(t, "artifact", "get", "--run", "pipe", "plan.md")
	if got != "# Plan\n\nShip artifacts.\n" {
		t.Fatalf("get content mismatch:\n%s", got)
	}

	var gotJSON struct {
		Artifact map[string]any `json:"artifact"`
		Content  string         `json:"content"`
	}
	if err := json.Unmarshal([]byte(e.legwork(t, "artifact", "get",
		"--run", "pipe", "--json", "plan.md")), &gotJSON); err != nil {
		t.Fatalf("get json: %v", err)
	}
	if gotJSON.Artifact["name"] != "plan.md" || gotJSON.Content != got {
		t.Fatalf("unexpected get json: %+v", gotJSON)
	}

	evs := e.legwork(t, "events", "pipe", "--run")
	if !strings.Contains(evs, "artifact") || !strings.Contains(evs, "plan.md") {
		t.Fatalf("save should record a run artifact event:\n%s", evs)
	}
}

func TestArtifactSaveFromStdin(t *testing.T) {
	e := newEnv(t)
	cmd := exec.Command(binPath, "artifact", "save", "--run", "pipe", "--name", "notes.md", "-")
	cmd.Env = append(os.Environ(),
		"LEGWORK_STATE_DIR="+e.state,
		"LEGWORK_FAKE_SCRIPT="+e.script,
	)
	cmd.Stdin = strings.NewReader("from stdin\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stdin save: %v\n%s", err, out)
	}
	if got := e.legwork(t, "artifact", "get", "--run", "pipe", "notes.md"); got != "from stdin\n" {
		t.Fatalf("stdin artifact mismatch: %q", got)
	}
}

func TestArtifactSaveFailsWhenRunEventCannotBeRecorded(t *testing.T) {
	e := newEnv(t)
	src := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(src, []byte("plan\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.state, "runs", "pipe", "events.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}

	if out, err := e.legworkErr("artifact", "save", "--run", "pipe", "--name", "plan.md", src); err == nil {
		t.Fatalf("artifact save succeeded despite unwritable run events log:\n%s", out)
	} else if !strings.Contains(out, "record artifact event") {
		t.Fatalf("artifact save error should mention event recording:\n%s", out)
	}
}

func TestArtifactRejectsTraversalAndBinary(t *testing.T) {
	e := newEnv(t)
	src := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(src, []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := e.legworkErr("artifact", "save", "--run", "pipe", "--name", "../plan.md", src); err == nil {
		t.Fatalf("traversal name should fail:\n%s", out)
	}
	if out, err := e.legworkErr("artifact", "list", "--run", "../pipe"); err == nil {
		t.Fatalf("traversal run should fail:\n%s", out)
	}

	bin := filepath.Join(t.TempDir(), "blob.bin")
	if err := os.WriteFile(bin, []byte{0xff, 0xfe, 0xfd}, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := e.legworkErr("artifact", "save", "--run", "pipe", "--name", "blob.bin", bin); err == nil {
		t.Fatalf("binary artifact should fail:\n%s", out)
	}
}

func TestArtifactOverwriteRequiresFlag(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "first.md")
	second := filepath.Join(dir, "second.md")
	if err := os.WriteFile(first, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	e.legwork(t, "artifact", "save", "--run", "pipe", "--name", "plan.md", first)
	if out, err := e.legworkErr("artifact", "save", "--run", "pipe", "--name", "plan.md", second); err == nil {
		t.Fatalf("overwrite without flag should fail:\n%s", out)
	}
	if got := e.legwork(t, "artifact", "get", "--run", "pipe", "plan.md"); got != "first\n" {
		t.Fatalf("artifact changed without overwrite: %q", got)
	}
	e.legwork(t, "artifact", "save", "--run", "pipe", "--name", "plan.md", "--overwrite", second)
	if got := e.legwork(t, "artifact", "get", "--run", "pipe", "plan.md"); got != "second\n" {
		t.Fatalf("overwrite did not replace artifact: %q", got)
	}
}
