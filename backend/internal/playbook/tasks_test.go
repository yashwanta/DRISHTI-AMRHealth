package playbook

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestFindTask(t *testing.T) {
	if _, ok := FindTask("service_disable"); !ok {
		t.Fatal("service_disable task not found")
	}
	if _, ok := FindTask("change_password"); !ok {
		t.Fatal("change_password task not found")
	}
	if _, ok := FindTask("does_not_exist"); ok {
		t.Fatal("unknown task should not be found")
	}
}

func TestServiceDisableProducesIdempotentRootGuardedCommand(t *testing.T) {
	task, ok := FindTask("service_disable")
	if !ok {
		t.Fatal("service_disable task not found")
	}

	// Idempotence check: should detect whether service is already disabled+stopped.
	check := task.Check(Params{"service": "cups"})
	if !strings.Contains(check, "cups") {
		t.Fatalf("check should reference the service name, got: %s", check)
	}

	// Mutation command.
	cmd, err := task.Run(Params{"service": "cups"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.Contains(cmd, "systemctl disable --now") {
		t.Fatalf("expected systemctl disable --now in command, got: %s", cmd)
	}
	if !strings.Contains(cmd, "cups") {
		t.Fatalf("expected service name 'cups' in command, got: %s", cmd)
	}
	// Must be root-guarded.
	if !strings.Contains(cmd, "id -u") || !strings.Contains(cmd, "sudo -n /bin/sh -c") {
		t.Fatalf("command must be root-guarded, got: %s", cmd)
	}
}

func TestServiceDisableRejectsInvalidServiceName(t *testing.T) {
	task, _ := FindTask("service_disable")
	if _, err := task.Run(Params{"service": "bad;rm -rf /"}); err == nil {
		t.Fatal("expected error for invalid service name")
	}
}

func TestChangePasswordPipesIntoChpasswd(t *testing.T) {
	task, ok := FindTask("change_password")
	if !ok {
		t.Fatal("change_password task not found")
	}

	cmd, err := task.Run(Params{"username": "root", "password": "S3cret!"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.Contains(cmd, "chpasswd") {
		t.Fatalf("expected chpasswd in command, got: %s", cmd)
	}
	if !strings.Contains(cmd, "root") {
		t.Fatalf("expected username 'root' in command, got: %s", cmd)
	}
	// Must be root-guarded.
	if !strings.Contains(cmd, "sudo -n /bin/sh -c") {
		t.Fatalf("password change must be root-guarded, got: %s", cmd)
	}
	// The plaintext password should appear inside the single-quoted literal
	// (it is piped to chpasswd and never logged elsewhere).
	if !strings.Contains(cmd, "S3cret!") {
		t.Fatalf("expected password value in chpasswd pipe, got: %s", cmd)
	}
}

func TestChangePasswordRejectsInvalidUsername(t *testing.T) {
	task, _ := FindTask("change_password")
	if _, err := task.Run(Params{"username": "root;evil", "password": "x"}); err == nil {
		t.Fatal("expected error for invalid username")
	}
}

func TestChangePasswordRejectsEmptyPassword(t *testing.T) {
	task, _ := FindTask("change_password")
	if _, err := task.Run(Params{"username": "root", "password": ""}); err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestPackageInstallIsCrossDistro(t *testing.T) {
	task, _ := FindTask("package_install")
	cmd, err := task.Run(Params{"package": "htop"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	for _, want := range []string{"apt-get", "dnf", "yum"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("expected %s branch in command, got: %s", want, cmd)
		}
	}
}

func TestShellQuoteEscapesSingleQuote(t *testing.T) {
	out := shellQuote("it's")
	if !strings.Contains(out, `'\''`) {
		t.Fatalf("single quote should be escaped, got: %s", out)
	}
}

func TestPasswordWithSingleQuoteIsSafelyEscaped(t *testing.T) {
	task, _ := FindTask("change_password")
	pw := "pa'ss"
	cmd, err := task.Run(Params{"username": "bob", "password": pw})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.Contains(cmd, `'\''`) {
		t.Fatalf("single quote in password must be escaped, got: %s", cmd)
	}
}

func TestFindWindowsTasks(t *testing.T) {
	for _, name := range []string{"win_change_password", "win_change_admin_password", "win_enable_admin", "win_disable_admin", "win_reboot"} {
		task, ok := FindTask(name)
		if !ok {
			t.Fatalf("expected Windows task %q in registry", name)
		}
		if task.Platform != "windows" {
			t.Fatalf("task %q should have Platform=windows, got %q", name, task.Platform)
		}
	}
}

func TestWindowsPasswordChangeUsesEncodedCommand(t *testing.T) {
	task, ok := FindTask("win_change_password")
	if !ok {
		t.Fatal("win_change_password task not found")
	}

	cmd, err := task.Run(Params{"username": "Administrator", "password": "P@ssw0rd!"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// Must use base64-encoded PowerShell to avoid cmd.exe escaping issues.
	if !strings.HasPrefix(cmd, "powershell.exe -NoProfile -EncodedCommand ") {
		t.Fatalf("expected EncodedCommand invocation, got: %s", cmd)
	}
	// The plaintext password must NOT appear on the command line (only base64 does).
	if strings.Contains(cmd, "P@ssw0rd!") {
		t.Fatalf("plaintext password must not appear in the command, got: %s", cmd)
	}
}

func TestWindowsPasswordChangeAdminShortcut(t *testing.T) {
	task, ok := FindTask("win_change_admin_password")
	if !ok {
		t.Fatal("win_change_admin_password task not found")
	}

	cmd, err := task.Run(Params{"password": "NewPass123#"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.HasPrefix(cmd, "powershell.exe -NoProfile -EncodedCommand ") {
		t.Fatalf("expected EncodedCommand invocation, got: %s", cmd)
	}
	if strings.Contains(cmd, "NewPass123#") {
		t.Fatalf("plaintext password must not appear in the command, got: %s", cmd)
	}
}

func TestWindowsPasswordChangeRejectsInvalidUsername(t *testing.T) {
	task, _ := FindTask("win_change_password")
	if _, err := task.Run(Params{"username": "user;evil", "password": "x"}); err == nil {
		t.Fatal("expected error for invalid Windows username")
	}
}

func TestWindowsPasswordChangeRejectsEmptyPassword(t *testing.T) {
	task, _ := FindTask("win_change_password")
	if _, err := task.Run(Params{"username": "Administrator", "password": ""}); err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestPwshEncodedProducesValidBase64(t *testing.T) {
	// "Write-Output 'hello'" should round-trip through base64 as valid UTF-16LE.
	cmd := pwshEncoded("Write-Output 'hello'")
	b64 := strings.TrimPrefix(cmd, "powershell.exe -NoProfile -EncodedCommand ")
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}
	if len(decoded) < 2 {
		t.Fatal("decoded too short to be UTF-16LE")
	}
	// UTF-16LE for 'W' (first char of "Write-Output") = 0x57 0x00
	if decoded[0] != 0x57 || decoded[1] != 0x00 {
		t.Fatalf("expected UTF-16LE encoding, first bytes: %x %x", decoded[0], decoded[1])
	}
}

func TestPsSingleQuoteDoublesSingleQuote(t *testing.T) {
	out := psSingleQuote("it's")
	// PowerShell single-quote escaping: ' -> ''
	if !strings.Contains(out, "it''s") {
		t.Fatalf("single quote should be doubled, got: %s", out)
	}
}

func TestWindowsTaskPlatformsAreTagged(t *testing.T) {
	for _, task := range Tasks() {
		if task.Platform == "windows" && !strings.HasPrefix(task.Name, "win_") {
			t.Fatalf("Windows task %q should be prefixed with win_", task.Name)
		}
		if strings.HasPrefix(task.Name, "win_") && task.Platform != "windows" {
			t.Fatalf("win_-prefixed task %q should have Platform=windows", task.Name)
		}
	}
}

