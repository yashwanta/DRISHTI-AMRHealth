package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPackageInstallRequiresRootWithoutPasswordHandling(t *testing.T) {
	handler := NewActionHandler(nil, "", false)
	req := actionRunRequest{
		Action:      "package_install",
		PackageName: "curl",
	}

	command, err := handler.buildCommand(req)
	if err != nil {
		t.Fatalf("buildCommand returned error: %v", err)
	}
	requireContains(t, command, "Run this script with sudo or as root")
	requireContains(t, command, "apt-get install -y")
	requireContains(t, command, "curl")
	requireContains(t, command, "sudo -n /bin/sh -c")
	requireNotContains(t, command, "sudo"+" -S")
	requireNotContains(t, command, "printf '"+"%s\\n"+"'")
}

func TestBuildPrivilegeCheckExplainsPatchReadiness(t *testing.T) {
	handler := NewActionHandler(nil, "", false)
	req := actionRunRequest{Action: "privilege_check"}

	command, err := handler.buildCommand(req)
	if err != nil {
		t.Fatalf("buildCommand returned error: %v", err)
	}
	requireContains(t, command, "Privilege check: PASS")
	requireContains(t, command, "passwordless sudo")
	requireContains(t, command, "Patch, install, upgrade, remediation, and reboot actions will fail")
	requireContains(t, command, "sudo -n /bin/sh -c")
	requireContains(t, command, "passwordless sudo for /bin/sh")
	requireNotContains(t, command, "sudo"+" -S")
}

func TestBuildCVERemediationDetectsKernelPackages(t *testing.T) {
	handler := NewActionHandler(nil, "", false)
	req := actionRunRequest{Action: "remediate_cve_2026_43494_linux_signed_upgrade"}

	command, err := handler.buildCommand(req)
	if err != nil {
		t.Fatalf("buildCommand returned error: %v", err)
	}
	requireContains(t, command, "apt-get update")
	requireContains(t, command, "dpkg-query")
	requireContains(t, command, "linux-generic-hwe-24.04")
	requireContains(t, command, "linux-image-[0-9]*")
	requireContains(t, command, "apt-get install -y --only-upgrade $packages")
	requireContains(t, command, "/var/run/reboot-required")
	requireContains(t, command, "uname -r")
	requireContains(t, command, "APT/dpkg lock is active")
	requireNotContains(t, command, "linux"+"-signed")
	requireNotContains(t, command, "sudo"+" -S")
}

func TestBuildCVE43494GenericKernelUsesSameDetectedPackageRemediation(t *testing.T) {
	handler := NewActionHandler(nil, "", false)
	req := actionRunRequest{Action: "remediate_cve_2026_43494_ubuntu_generic_kernel"}

	command, err := handler.buildCommand(req)
	if err != nil {
		t.Fatalf("buildCommand returned error: %v", err)
	}
	requireContains(t, command, "No supported installed Ubuntu kernel meta/image package found to upgrade.")
	requireContains(t, command, "Installed kernel packages after remediation:")
	requireNotContains(t, command, "linux"+"-signed")
}

func TestBuildSystemRebootRequiresRootWithoutPasswordHandling(t *testing.T) {
	handler := NewActionHandler(nil, "", false)
	req := actionRunRequest{Action: "system_reboot"}

	command, err := handler.buildCommand(req)
	if err != nil {
		t.Fatalf("buildCommand returned error: %v", err)
	}
	requireContains(t, command, "Run this script with sudo or as root")
	requireContains(t, command, "Reboot command accepted. The SSH session may disconnect.")
	requireContains(t, command, "nohup systemctl reboot")
	requireNotContains(t, command, "sudo"+" -S")
}

func TestBuildListUpgradesReturnsClearOutputWhenEmpty(t *testing.T) {
	handler := NewActionHandler(nil, "", false)
	req := actionRunRequest{Action: "package_list_upgrades"}

	command, err := handler.buildCommand(req)
	if err != nil {
		t.Fatalf("buildCommand returned error: %v", err)
	}
	requireContains(t, command, "Package manager: apt-get")
	requireContains(t, command, "No apt upgrades are currently available.")
	requireContains(t, command, "No dnf upgrades are currently available.")
	requireContains(t, command, "No yum upgrades are currently available.")
	requireNotContains(t, command, "sudo"+" -S")
}

func TestBuildPackageUpgradePrintsStatusAndRebootRequirement(t *testing.T) {
	handler := NewActionHandler(nil, "", false)
	req := actionRunRequest{Action: "package_upgrade"}

	command, err := handler.buildCommand(req)
	if err != nil {
		t.Fatalf("buildCommand returned error: %v", err)
	}
	requireContains(t, command, "Running apt system upgrade...")
	requireContains(t, command, "apt system upgrade completed.")
	requireContains(t, command, "Reboot required:")
	requireContains(t, command, "Running dnf system upgrade...")
	requireContains(t, command, "Running yum system update...")
}

func TestApprovedCustomCommandAllowsRootRequiredPackageTemplate(t *testing.T) {
	handler := NewActionHandler(nil, "", false)
	req := actionRunRequest{
		Action:  "approved_custom_command",
		Command: "apt-get update && env DEBIAN_FRONTEND=noninteractive apt-get install -y --only-upgrade linux-generic linux-image-generic linux-headers-generic",
	}

	command, err := handler.buildCommand(req)
	if err != nil {
		t.Fatalf("buildCommand returned error: %v", err)
	}
	requireContains(t, command, "Run this script with sudo or as root")
	requireContains(t, command, "apt-get update")
	requireContains(t, command, "linux-generic linux-image-generic linux-headers-generic")
	requireNotContains(t, command, "sudo"+" -S")
}

func TestApprovedCustomCommandRejectsShellMetacharacters(t *testing.T) {
	handler := NewActionHandler(nil, "", false)
	req := actionRunRequest{
		Action:  "approved_custom_command",
		Command: "apt-get update; cat /etc/shadow",
	}

	if _, err := handler.buildCommand(req); err == nil {
		t.Fatalf("expected shell metacharacter command to be rejected")
	}
}

func TestRepositoryDoesNotContainSudoCredentialPiping(t *testing.T) {
	root := filepath.Clean("../../..")
	forbidden := []string{"sudo" + " -S", "printf '" + "%s\\n" + "'"}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "dist", ".gocache":
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		for _, pattern := range forbidden {
			if strings.Contains(text, pattern) {
				t.Fatalf("forbidden sudo password pattern %q found in %s", pattern, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan repository: %v", err)
	}
}

func requireContains(t *testing.T, value, needle string) {
	t.Helper()
	if !strings.Contains(value, needle) {
		t.Fatalf("expected %q to contain %q", value, needle)
	}
}

func requireNotContains(t *testing.T, value, needle string) {
	t.Helper()
	if strings.Contains(value, needle) {
		t.Fatalf("expected %q not to contain %q", value, needle)
	}
}
