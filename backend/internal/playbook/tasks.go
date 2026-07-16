package playbook

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode/utf16"
)

// Params holds task-specific parameters (e.g. service name, username).
type Params map[string]string

// Task describes a single idempotent operation that can be fanned out to many
// hosts, modeled after an Ansible module.
type Task struct {
	Name        string
	Label       string
	Description string
	NeedsRoot   bool
	// Platform is "linux", "windows", or "" (any). Used by the UI to badge
	// tasks and help users avoid running a Linux task on a Windows host.
	Platform string
	// Fields declares the parameter keys the UI should collect, in order.
	Fields []Field
	// Check returns a command that exits 0 when the host is already in the
	// desired state (so the mutation can be skipped). Empty string = no check.
	Check func(Params) string
	// Run returns the command that applies the change.
	Run func(Params) (string, error)
}

// Field describes a single parameter the UI should render for a task.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`
}

// ---- Shell helpers (mirror the approved patterns in handlers) ----

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// rootGuard wraps a command so it runs as root or via passwordless sudo.
// It never pipes passwords -- consistent with the project security model.
func rootGuard(command string) string {
	quoted := shellQuote(command)
	return "if [ \"$(id -u)\" -eq 0 ]; then /bin/sh -c " + quoted + "; else sudo -n /bin/sh -c " + quoted + "; fi"
}

// pkmgr dispatches to apt/dnf/yum so tasks work across distros.
func pkmgr(apt, dnf, yum string) string {
	return fmt.Sprintf(
		"if command -v apt-get >/dev/null 2>&1; then %s; elif command -v dnf >/dev/null 2>&1; then %s; elif command -v yum >/dev/null 2>&1; then %s; else echo 'No supported package manager found.'; exit 2; fi",
		apt, dnf, yum)
}

// psSingleQuote makes a PowerShell single-quoted string literal. Inside
// single quotes PowerShell treats everything literally; the only escape is
// doubling a single quote. This is injection-safe for passwords.
func psSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// pwshEncoded builds a powershell.exe -EncodedCommand invocation from a
// PowerShell script string. The script is UTF-16LE base64-encoded per the
// PowerShell -EncodedCommand spec, which eliminates all cmd.exe / SSH shell
// escaping problems. This is the only safe way to pass a password (which may
// contain quotes, ampersands, etc.) to a Windows host over SSH.
func pwshEncoded(script string) string {
	codepoints := utf16.Encode([]rune(script))
	le := make([]byte, len(codepoints)*2)
	for i, v := range codepoints {
		binary.LittleEndian.PutUint16(le[i*2:], v)
	}
	return "powershell.exe -NoProfile -EncodedCommand " + base64.StdEncoding.EncodeToString(le)
}

// ---- Task registry ----

var (
	registryOnce sync.Once
	tasks        []Task
)

func Tasks() []Task {
	registryOnce.Do(registerTasks)
	return tasks
}

func FindTask(name string) (Task, bool) {
	for _, t := range Tasks() {
		if t.Name == name {
			return t, true
		}
	}
	return Task{}, false
}

func registerTasks() {
	tasks = []Task{
		// ===== Linux: service control =====
		linuxTask("service_stop", "Stop service", "Stop a systemd unit (e.g. stop CUPS print server).",
			[]Field{{Key: "service", Label: "Service name", Placeholder: "cups", Required: true}},
			func(p Params) string {
				svc := shellQuote(p["service"])
				return fmt.Sprintf("systemctl is-active --quiet %s || exit 1", svc)
			},
			func(p Params) (string, error) {
				svc := p["service"]
				if !validUnitName(svc) {
					return "", fmt.Errorf("invalid service name")
				}
				return rootGuard(fmt.Sprintf("systemctl stop %s; echo 'Stopped service: %s'", shellQuote(svc), svc)), nil
			}),
		linuxTask("service_disable", "Disable service", "Stop and disable a service so it does not start on boot (e.g. disable CUPS).",
			[]Field{{Key: "service", Label: "Service name", Placeholder: "cups", Required: true}},
			func(p Params) string {
				svc := shellQuote(p["service"])
				return fmt.Sprintf("systemctl is-enabled %s 2>/dev/null | grep -q disabled && systemctl is-active --quiet %s || exit 1", svc, svc)
			},
			func(p Params) (string, error) {
				svc := p["service"]
				if !validUnitName(svc) {
					return "", fmt.Errorf("invalid service name")
				}
				return rootGuard(fmt.Sprintf("systemctl disable --now %s; echo 'Disabled service: %s'", shellQuote(svc), svc)), nil
			}),
		linuxTask("service_enable", "Enable service", "Enable and start a systemd unit.",
			[]Field{{Key: "service", Label: "Service name", Placeholder: "ssh", Required: true}},
			func(p Params) string {
				svc := shellQuote(p["service"])
				return fmt.Sprintf("systemctl is-enabled %s 2>/dev/null | grep -q enabled && systemctl is-active --quiet %s || exit 1", svc, svc)
			},
			func(p Params) (string, error) {
				svc := p["service"]
				if !validUnitName(svc) {
					return "", fmt.Errorf("invalid service name")
				}
				return rootGuard(fmt.Sprintf("systemctl enable --now %s; echo 'Enabled service: %s'", shellQuote(svc), svc)), nil
			}),
		linuxTask("service_restart", "Restart service", "Restart a systemd unit.",
			[]Field{{Key: "service", Label: "Service name", Placeholder: "nginx", Required: true}},
			nil,
			func(p Params) (string, error) {
				svc := p["service"]
				if !validUnitName(svc) {
					return "", fmt.Errorf("invalid service name")
				}
				return rootGuard(fmt.Sprintf("systemctl restart %s; echo 'Restarted service: %s'", shellQuote(svc), svc)), nil
			}),

		// ===== Linux: password change =====
		linuxTask("change_password", "Change user password", "Set a new password for a Linux account. The plaintext password is never logged; it is piped into chpasswd only.",
			[]Field{
				{Key: "username", Label: "Username", Placeholder: "root", Required: true},
				{Key: "password", Label: "New password", Placeholder: "********", Required: true, Secret: true},
			},
			nil,
			func(p Params) (string, error) {
				user := p["username"]
				pw := p["password"]
				if !validLinuxName(user) {
					return "", fmt.Errorf("invalid username")
				}
				if pw == "" {
					return "", fmt.Errorf("password is required")
				}
				quoted := shellQuote(user + ":" + pw)
				cmd := fmt.Sprintf("printf '%%s\\n' %s | chpasswd; echo 'Password updated for user: %s'", quoted, user)
				return rootGuard(cmd), nil
			}),

		// ===== Linux: package management =====
		linuxTask("package_install", "Install package", "Install a package across apt/dnf/yum hosts.",
			[]Field{{Key: "package", Label: "Package name", Placeholder: "htop", Required: true}},
			nil,
			func(p Params) (string, error) {
				pkg := p["package"]
				if !validPackageName(pkg) {
					return "", fmt.Errorf("invalid package name")
				}
				q := shellQuote(pkg)
				return pkmgr(
					"DEBIAN_FRONTEND=noninteractive apt-get install -y "+q+"; echo 'Installed: "+pkg+"'",
					"dnf install -y "+q+"; echo 'Installed: "+pkg+"'",
					"yum install -y "+q+"; echo 'Installed: "+pkg+"'",
				), nil
			}),
		linuxTask("package_remove", "Remove package", "Remove a package across apt/dnf/yum hosts.",
			[]Field{{Key: "package", Label: "Package name", Placeholder: "cups", Required: true}},
			nil,
			func(p Params) (string, error) {
				pkg := p["package"]
				if !validPackageName(pkg) {
					return "", fmt.Errorf("invalid package name")
				}
				q := shellQuote(pkg)
				return pkmgr(
					"DEBIAN_FRONTEND=noninteractive apt-get remove -y "+q+"; echo 'Removed: "+pkg+"'",
					"dnf remove -y "+q+"; echo 'Removed: "+pkg+"'",
					"yum remove -y "+q+"; echo 'Removed: "+pkg+"'",
				), nil
			}),
		linuxTask("package_upgrade", "Upgrade packages", "Run a full system package upgrade across apt/dnf/yum hosts.",
			nil, nil,
			func(p Params) (string, error) {
				return pkmgr(
					"DEBIAN_FRONTEND=noninteractive apt-get -y upgrade; echo 'Upgrade completed'",
					"dnf -y upgrade; echo 'Upgrade completed'",
					"yum -y update; echo 'Upgrade completed'",
				), nil
			}),

		// ===== Linux: system =====
		linuxTask("system_reboot", "Reboot host", "Reboot the host. The SSH session will disconnect.",
			nil, nil,
			func(p Params) (string, error) {
				return rootGuard("echo 'Reboot command accepted. The SSH session may disconnect.'; sh -c 'nohup systemctl reboot >/dev/null 2>&1 &'"), nil
			}),
		linuxTask("apt_update_upgrade", "apt update + upgrade", "Refresh package cache and upgrade all packages (apt only; skipped on dnf/yum hosts).",
			nil, nil,
			func(p Params) (string, error) {
				return rootGuard("DEBIAN_FRONTEND=noninteractive apt-get update && DEBIAN_FRONTEND=noninteractive apt-get -y upgrade; echo 'apt update + upgrade completed'"), nil
			}),

		// ===== Windows tasks =====
		windowsTask("win_change_password", "Change Windows local password", "Set a new password for a local Windows account (e.g. Administrator). Requires OpenSSH Server on the target. The password is delivered as a base64-encoded PowerShell command and never exposed to cmd.exe.",
			[]Field{
				{Key: "username", Label: "Username", Placeholder: "Administrator", Required: true},
				{Key: "password", Label: "New password", Placeholder: "********", Required: true, Secret: true},
			},
			nil,
			func(p Params) (string, error) {
				user := p["username"]
				pw := p["password"]
				if !validWindowsUser(user) {
					return "", fmt.Errorf("invalid Windows username")
				}
				if pw == "" {
					return "", fmt.Errorf("password is required")
				}
				psUser := psSingleQuote(user)
				psPw := psSingleQuote(pw)
				script := fmt.Sprintf(
					"$u = %s; $p = ConvertTo-SecureString %s -AsPlainText -Force; "+
						"try { Set-LocalUser -Name $u -Password $p -ErrorAction Stop; "+
						"Write-Output \"Password updated for local user: $u\" } "+
						"catch { Write-Output $_.Exception.Message; exit 1 }",
					psUser, psPw)
				return pwshEncoded(script), nil
			}),
		windowsTask("win_change_admin_password", "Change Windows Administrator password", "Rotate the built-in local Administrator password across all selected Windows hosts.",
			[]Field{{Key: "password", Label: "New password", Placeholder: "********", Required: true, Secret: true}},
			nil,
			func(p Params) (string, error) {
				pw := p["password"]
				if pw == "" {
					return "", fmt.Errorf("password is required")
				}
				psPw := psSingleQuote(pw)
				script := fmt.Sprintf(
					"$p = ConvertTo-SecureString %s -AsPlainText -Force; "+
						"try { Set-LocalUser -Name 'Administrator' -Password $p -ErrorAction Stop; "+
						"Write-Output 'Password updated for local user: Administrator' } "+
						"catch { Write-Output $_.Exception.Message; exit 1 }",
					psPw)
				return pwshEncoded(script), nil
			}),
		windowsTask("win_enable_admin", "Enable Windows local Administrator", "Enable the built-in local Administrator account on Windows hosts.",
			nil, nil,
			func(p Params) (string, error) {
				return pwshEncoded("try { Enable-LocalUser -Name 'Administrator' -ErrorAction Stop; "+
					"Write-Output 'Enabled local user: Administrator' } "+
					"catch { Write-Output $_.Exception.Message; exit 1 }"), nil
			}),
		windowsTask("win_disable_admin", "Disable Windows local Administrator", "Disable the built-in local Administrator account (security hardening).",
			nil, nil,
			func(p Params) (string, error) {
				return pwshEncoded("try { Disable-LocalUser -Name 'Administrator' -ErrorAction Stop; "+
					"Write-Output 'Disabled local user: Administrator' } "+
					"catch { Write-Output $_.Exception.Message; exit 1 }"), nil
			}),
		windowsTask("win_restart_service", "Restart Windows service", "Restart a Windows service (e.g. Spooler, sshd).",
			[]Field{{Key: "service", Label: "Service name", Placeholder: "Spooler", Required: true}},
			nil,
			func(p Params) (string, error) {
				svc := p["service"]
				if !validWindowsUser(svc) {
					return "", fmt.Errorf("invalid service name")
				}
				psSvc := psSingleQuote(svc)
				return pwshEncoded(fmt.Sprintf("try { Restart-Service -Name %s -Force -ErrorAction Stop; "+
					"Write-Output \"Restarted service: %s\" } catch { Write-Output $_.Exception.Message; exit 1 }",
					psSvc, svc)), nil
			}),
		windowsTask("win_reboot", "Reboot Windows host", "Restart the Windows machine. The SSH session will disconnect.",
			nil, nil,
			func(p Params) (string, error) {
				return pwshEncoded("Write-Output 'Reboot command accepted. The SSH session may disconnect.'; "+
					"Start-Process -FilePath shutdown.exe -ArgumentList '/r','/t','5','/c','SiteOps Playbook reboot' -NoNewWindow"), nil
			}),
		windowsTask("win_windows_update", "Windows Update (scan + install)", "Scan for and install all available Windows updates. Uses PSWindowsUpdate module if available; falls back to the built-in COM API.",
			nil, nil,
			func(p Params) (string, error) {
				script := "if (Get-Module -ListAvailable -Name PSWindowsUpdate) {\n"+
					"    Import-Module PSWindowsUpdate\n"+
					"    $r = Install-WindowsUpdate -AcceptAll -AutoReboot:$false -ErrorAction SilentlyContinue\n"+
					"    if ($r) { Write-Output \"Installed $($r.Count) update(s):\"; $r | Format-Table Title, Result -AutoSize }\n"+
					"    else { Write-Output 'No updates available or already fully patched.' }\n"+
					"} else {\n"+
					"    Write-Output 'PSWindowsUpdate module not found; using built-in COM API.'\n"+
					"    $session = New-Object -ComObject Microsoft.Update.Session\n"+
					"    $searcher = $session.CreateUpdateSearcher()\n"+
					"    $result = $searcher.Search(\"IsInstalled=0 and Type='Software'\")\n"+
					"    if ($result.Updates.Count -eq 0) { Write-Output 'No updates available.'; exit 0 }\n"+
					"    Write-Output \"$($result.Updates.Count) update(s) available; downloading...\"\n"+
					"    $toInstall = New-Object -ComObject Microsoft.Update.UpdateColl\n"+
					"    foreach ($u in $result.Updates) { $u | ForEach-Object { $_.AcceptEula(); $toInstall.Add($_) | Out-Null } }\n"+
					"    $downloader = $session.CreateUpdateDownloader()\n"+
					"    $downloader.Updates = $toInstall\n"+
					"    $downloader.Download() | Out-Null\n"+
					"    $installer = $session.CreateUpdateInstaller()\n"+
					"    $installer.Updates = $toInstall\n"+
					"    $installResult = $installer.Install()\n"+
					"    Write-Output \"Install result code: $($installResult.ResultCode) (2=Succeeded, 3=SucceededWithErrors)\"\n"+
					"}"
				return pwshEncoded(script), nil
			}),
		windowsTask("win_install_pswindowsupdate", "Install PSWindowsUpdate module", "Installs the PSWindowsUpdate PowerShell module (requires NuGet provider). Run once per host before using Windows Update.",
			nil, nil,
			func(p Params) (string, error) {
				return pwshEncoded("try {\n"+
					"    Install-PackageProvider -Name NuGet -MinimumVersion 2.8.5.201 -Force -ErrorAction SilentlyContinue | Out-Null\n"+
					"    Set-PSRepository -Name PSGallery -InstallationPolicy Trusted -ErrorAction SilentlyContinue\n"+
					"    Install-Module -Name PSWindowsUpdate -Force -AllowClobber -ErrorAction Stop\n"+
					"    Import-Module PSWindowsUpdate -ErrorAction Stop\n"+
					"    Write-Output 'PSWindowsUpdate module installed successfully.'\n"+
					"} catch { Write-Output $_.Exception.Message; exit 1 }"), nil
			}),
	}
}

// linuxTask is a constructor that sets Platform to "linux".
func linuxTask(name, label, desc string, fields []Field, check func(Params) string, run func(Params) (string, error)) Task {
	return Task{Name: name, Label: label, Description: desc, NeedsRoot: true, Platform: "linux", Fields: fields, Check: check, Run: run}
}

// windowsTask is a constructor that sets Platform to "windows".
func windowsTask(name, label, desc string, fields []Field, check func(Params) string, run func(Params) (string, error)) Task {
	return Task{Name: name, Label: label, Description: desc, NeedsRoot: false, Platform: "windows", Fields: fields, Check: check, Run: run}
}

// ---- validators (kept local so this package is self-contained) ----

var (
	unitNameRE    = regexp.MustCompile(`^[A-Za-z0-9_.@:-]+$`)
	linuxNameRE   = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	packageNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:+-]{0,127}$`)
	winUsernameRE = regexp.MustCompile(`^[A-Za-z0-9_. -]{1,104}$`)
)

func validUnitName(v string) bool    { return v != "" && unitNameRE.MatchString(v) }
func validLinuxName(v string) bool   { return v != "" && linuxNameRE.MatchString(v) }
func validPackageName(v string) bool { return v != "" && packageNameRE.MatchString(v) }
func validWindowsUser(v string) bool { return v != "" && winUsernameRE.MatchString(v) }
