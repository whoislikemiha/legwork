package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallScriptSyntax(t *testing.T) {
	if out, err := exec.Command("sh", "-n", "install.sh").CombinedOutput(); err != nil {
		t.Fatalf("install.sh syntax: %v\n%s", err, out)
	}
}

func TestInstallScriptInstallsSkillForDetectedHarness(t *testing.T) {
	tmp := t.TempDir()
	archive := fakeLegworkArchive(t, tmp)
	fakebin := fakeInstallerPath(t, tmp, archive)
	writeExecutable(t, filepath.Join(fakebin, "claude"), "#!/bin/sh\nexit 0\n")

	home := filepath.Join(tmp, "home")
	installDir := filepath.Join(tmp, "install")
	cmd := exec.Command("sh", "install.sh")
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"LEGWORK_INSTALL_DIR="+installDir,
		"PATH="+fakebin+":"+os.Getenv("PATH"),
		"FAKE_ARCHIVE="+archive,
		"FAKE_LOG="+filepath.Join(tmp, "skill.log"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	if got := mustReadString(t, filepath.Join(tmp, "skill.log")); !strings.Contains(got, "skill install --target claude") {
		t.Fatalf("skill install not called for claude:\n%s\noutput:\n%s", got, out)
	}
}

func TestInstallScriptSkillConflictDoesNotBreakBinaryInstall(t *testing.T) {
	tmp := t.TempDir()
	archive := fakeLegworkArchive(t, tmp)
	fakebin := fakeInstallerPath(t, tmp, archive)
	writeExecutable(t, filepath.Join(fakebin, "codex"), "#!/bin/sh\nexit 0\n")

	installDir := filepath.Join(tmp, "install")
	cmd := exec.Command("sh", "install.sh")
	cmd.Env = append(os.Environ(),
		"HOME="+filepath.Join(tmp, "home"),
		"LEGWORK_INSTALL_DIR="+installDir,
		"PATH="+fakebin+":"+os.Getenv("PATH"),
		"FAKE_ARCHIVE="+archive,
		"FAKE_LOG="+filepath.Join(tmp, "skill.log"),
		"FAKE_SKILL_FAIL=codex",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("skill conflict must not fail installer: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(installDir, "legwork")); err != nil {
		t.Fatalf("binary not installed after skill conflict: %v", err)
	}
	if !strings.Contains(string(out), "NOTE: legwork skill for codex was not installed") {
		t.Fatalf("missing conflict note:\n%s", out)
	}
}

func fakeLegworkArchive(t *testing.T, tmp string) string {
	t.Helper()
	src := filepath.Join(tmp, "archive-src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(src, "legwork"), `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "legwork test"
  exit 0
fi
if [ "$1" = "skill" ] && [ "$2" = "install" ]; then
  echo "$@" >> "$FAKE_LOG"
  if [ "${FAKE_SKILL_FAIL:-}" = "$4" ]; then
    echo "conflict" >&2
    exit 1
  fi
  exit 0
fi
exit 2
`)
	archive := filepath.Join(tmp, "legwork.tar.gz")
	cmd := exec.Command("tar", "-czf", archive, "-C", src, "legwork")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create archive: %v\n%s", err, out)
	}
	return archive
}

func fakeInstallerPath(t *testing.T, tmp, archive string) string {
	t.Helper()
	_ = archive
	fakebin := filepath.Join(tmp, "fakebin")
	if err := os.MkdirAll(fakebin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(fakebin, "curl"), `#!/bin/sh
out=
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    out=$1
  fi
  shift
done
cp "$FAKE_ARCHIVE" "$out"
`)
	return fakebin
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
