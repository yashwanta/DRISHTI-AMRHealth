package ssh

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Client struct {
	conn *ssh.Client
}

type Config struct {
	Host       string
	Port       int
	Username   string
	AuthType   string
	Password   string
	PrivateKey string
}

func Connect(cfg Config) (*Client, error) {
	var auth []ssh.AuthMethod

	switch cfg.AuthType {
	case "key":
		privateKey := strings.TrimSpace(cfg.PrivateKey)
		if privateKey == "" {
			return nil, fmt.Errorf("private key is empty. Paste the full private key block that starts with -----BEGIN OPENSSH PRIVATE KEY-----")
		}
		if strings.HasPrefix(privateKey, "ssh-ed25519 ") || strings.HasPrefix(privateKey, "ssh-rsa ") || strings.HasPrefix(privateKey, "ecdsa-sha2-") {
			return nil, fmt.Errorf("private key field contains a public key. Paste the private key block, not the public key line")
		}
		if !strings.Contains(privateKey, "BEGIN") || !strings.Contains(privateKey, "PRIVATE KEY") {
			return nil, fmt.Errorf("private key format is invalid. Paste the full private key block, not a file path or public key")
		}
		signer, err := ssh.ParsePrivateKey([]byte(privateKey))
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w. Paste the full private key including BEGIN and END lines", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	default:
		auth = append(auth, ssh.Password(cfg.Password))
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            auth,
		HostKeyCallback: knownHostsCallback(),
		Timeout:         15 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	conn, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return &Client{conn: conn}, nil
}

func knownHostsCallback() ssh.HostKeyCallback {
	knownHostsFile := os.Getenv("KNOWN_HOSTS_FILE")
	if strings.TrimSpace(knownHostsFile) == "" {
		knownHostsFile = "./known_hosts"
	}

	created := false
	if _, err := os.Stat(knownHostsFile); errors.Is(err, os.ErrNotExist) {
		created = true
	} else if err != nil {
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return fmt.Errorf("stat known hosts file %s: %w", knownHostsFile, err)
		}
	}

	file, err := os.OpenFile(knownHostsFile, os.O_RDONLY|os.O_CREATE, 0600)
	if err != nil {
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return fmt.Errorf("open known hosts file %s: %w", knownHostsFile, err)
		}
	}
	_ = file.Close()
	if created {
		log.Printf("known hosts file %s did not exist; created empty file, no SSH hosts are trusted yet", knownHostsFile)
	}

	callback, err := knownhosts.New(knownHostsFile)
	if err != nil {
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return fmt.Errorf("load known hosts file %s: %w", knownHostsFile, err)
		}
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := callback(hostname, remote, key); err != nil {
			var keyErr *knownhosts.KeyError
			fingerprint := ssh.FingerprintSHA256(key)
			if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
				return fmt.Errorf("unknown SSH host %s with key fingerprint %s; add it manually with: ssh-keyscan -H %s >> %s", hostname, fingerprint, hostWithoutPort(hostname), knownHostsFile)
			}
			return fmt.Errorf("SSH host key mismatch for %s with presented fingerprint %s; possible MITM or host key rotation, verify the server and update %s manually: %w", hostname, fingerprint, knownHostsFile, err)
		}
		return nil
	}
}

func hostWithoutPort(hostname string) string {
	if host, _, err := net.SplitHostPort(hostname); err == nil {
		return host
	}
	return hostname
}

func (c *Client) Close() {
	c.conn.Close()
}

func (c *Client) Run(cmd string) (string, error) {
	sess, err := c.conn.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer sess.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	_ = sess.RequestPty("xterm", 80, 40, modes)

	out, err := sess.CombinedOutput(cmd)
	if err != nil {
		return string(out), fmt.Errorf("remote command failed: %w", err)
	}
	return string(out), nil
}

// FetchLogs pulls Ubuntu and FleetManager/application logs since the given time.
// Covers: AMR/RDS connection state, Roboshop app logs, kernel/OOM/crash,
// disk errors, service failures, MySQL health, live TCP connections.
func (c *Client) FetchLogs(since time.Time, appLogPaths string) (map[string]string, error) {
	sinceStr := since.UTC().Format("2006-01-02 15:04:05")
	startTS := since.UTC().Format("20060102150405")
	nowTS := time.Now().UTC().Format("20060102150405")

	logs := make(map[string]string)

	run := func(key, cmd string) {
		out, err := c.Run(cmd)
		if err == nil && strings.TrimSpace(out) != "" {
			logs[key] = out
		}
	}

	// -- journald: AMR / RDS / Roboshop connection + crash events
	amrGrep := "Roboshop|rds|AMR|10[.]216[.]35|SocketState|ConnectedState|UnconnectedState" +
		"|ClosingState|remote host closed|Connect timeout|Add device failed|Not connected" +
		"|slotTcpError|setLastError|timeout|disconnect|connected|map|smap|scene|upload|deploy|push|19204|19205|19206|19207" +
		"|model|models|robot[.]cp|MD5|md5|checksum|chargeDI|charge[_ -]?DI|chargingDI|charging[_ -]?DI|charge|charging|charger|dock|docking|command|cmd" +
		"|robot_other_setchargingrelay_req|setchargingrelay|chargingrelay|charge_req|goCharge|go_charge|dock_req|robot_task_gotarget_req|gotarget|battery|battery_level|batteryLevel|GetBatteryLevel|robot_status_battery_req|voltage|soc|power low" +
		"|default|factory|restore|reset|reloadRobodMakeIni|active:false|echoid|robot_core_upgrade_robot_req|upgrade[.]zip|upgradeStatus|startup[.]sh|core is not activated|license inactive|activation failed|rds[.]scene|model_md5" +
		"|WarLink|shingo-edge|PLC|deadman|WriteTag|SendUnitDataTransaction|countgroup|Crosswalk"
	run("journald_amr", fmt.Sprintf(
		"journalctl --since %q --no-pager -o short-iso 2>/dev/null | grep -Ei %q || true",
		sinceStr, amrGrep))

	run("journald_warlink", fmt.Sprintf(
		"journalctl -u shingo-edge --since %q --no-pager -o short-iso 2>/dev/null || true",
		sinceStr))

	// -- journald -k: kernel OOM / panic / disk / hardware
	kernGrep := "oom|out of memory|killed process|segfault|core dumped|error|fail|panic" +
		"|BUG:|OOPS:|watchdog|soft lockup|I.O error|EXT4|XFS|BTRFS|MCE|NMI|blocked" +
		"|link is down|link is up"
	run("journald_kernel", fmt.Sprintf(
		"journalctl -k --since %q --no-pager -o short-iso 2>/dev/null | grep -Ei %q || true",
		sinceStr, kernGrep))

	// -- startup_robod service
	run("journald_robod", fmt.Sprintf(
		"journalctl -u startup_robod --since %q --no-pager -o short-iso 2>/dev/null || true",
		sinceStr))

	// -- all units: warning and above
	run("journald_warnings", fmt.Sprintf(
		"journalctl --since %q -p warning --no-pager -o short-iso 2>/dev/null || true",
		sinceStr))

	// -- Roboshop app log files (timestamp-range filtered)
	roboshopGrep := "AMR|10[.]216[.]35|SocketState|ConnectedState|UnconnectedState|ClosingState" +
		"|remote host closed|Connect timeout|Add device failed|Not connected|slotTcpError" +
		"|setLastError|timeout|disconnect|connected|19204|19205|19206|19207" +
		"|error|failed|fatal|exception|segfault|scene|smap|map|upload|deploy|push" +
		"|model|models|robot[.]cp|MD5|md5|checksum|chargeDI|charge[_ -]?DI|chargingDI|charging[_ -]?DI|charge|charging|charger|dock|docking|command|cmd" +
		"|robot_other_setchargingrelay_req|setchargingrelay|chargingrelay|charge_req|goCharge|go_charge|dock_req|robot_task_gotarget_req|gotarget|battery|battery_level|batteryLevel|GetBatteryLevel|robot_status_battery_req|voltage|soc|power low" +
		"|default|factory|restore|reset|reloadRobodMakeIni|active:false|echoid|robot_core_upgrade_robot_req|upgrade[.]zip|upgradeStatus|startup[.]sh|core is not activated|license inactive|activation failed|rds[.]scene|model_md5"
	run("roboshop_app", fmt.Sprintf(
		"find /opt/Roboshop/bin/location/appInfo/log -type f -iname '*.log' -print0 2>/dev/null"+
			" | xargs -0 awk -v start=%s -v end=%s"+
			" '{if (match($0,/\\[([0-9]{8}) ([0-9]{6})\\./,a)){ts=a[1] a[2];if(ts>=start&&ts<=end)print FILENAME\": \"$0}}'"+
			" 2>/dev/null | grep -Ei %q || true",
		startTS, nowTS, roboshopGrep))

	// -- RDS / rdscore / robod file logs
	rdsGrep := "AMR|10[.]216[.]35|SocketState|ConnectedState|UnconnectedState|ClosingState" +
		"|remote host closed|Connect timeout|Add device failed|Not connected|slotTcpError" +
		"|setLastError|timeout|disconnect|connected|19204|19205|19206|19207" +
		"|error|failed|fatal|exception|scene|smap|map|upload|deploy|push|mysql|database|segfault" +
		"|model|models|robot[.]cp|MD5|md5|checksum|chargeDI|charge[_ -]?DI|chargingDI|charging[_ -]?DI|charge|charging|charger|dock|docking|command|cmd" +
		"|robot_other_setchargingrelay_req|setchargingrelay|chargingrelay|charge_req|goCharge|go_charge|dock_req|robot_task_gotarget_req|gotarget|battery|battery_level|batteryLevel|GetBatteryLevel|robot_status_battery_req|voltage|soc|power low" +
		"|default|factory|restore|reset|reloadRobodMakeIni|active:false|echoid|robot_core_upgrade_robot_req|upgrade[.]zip|upgradeStatus|startup[.]sh|core is not activated|license inactive|activation failed|rds[.]scene|model_md5"
	run("rds_file_logs",
		"find /opt/data/rds /opt/data/rdscore /opt/data/robod -type f"+
			" \\( -iname '*.log' -o -iname '*.out' -o -iname '*.err' \\) -mmin -1440 -print0 2>/dev/null"+
			" | xargs -0 grep -HinEi "+fmt.Sprintf("%q", rdsGrep)+" 2>/dev/null || true")

	warlinkGrep := "WarLink|shingo-edge|PLC|deadman|WriteTag|SendUnitDataTransaction|countgroup|Crosswalk|panic|fatal|segfault|core dumped|failed|error|timeout|not connected"
	run("warlink_file_logs", fmt.Sprintf(
		"find /opt /var/log /home/pi -type f"+
			" \\( -iname '*warlink*.log*' -o -iname '*shingo-edge*.log*' -o -iname '*edge*.log*' \\) -mmin -10080 -print0 2>/dev/null"+
			" | xargs -0 zgrep -HinEi %q 2>/dev/null | tail -n 8000 || true",
		warlinkGrep))

	// -- RDS / Roboshop API, access, and audit logs. These are the most likely
	// places to include who pushed a map and the client IP used for the upload.
	mapAuditGrep := "map|smap|scene|model|models|robot[.]cp|MD5|md5|checksum|chargeDI|charge[_ -]?DI|chargingDI|charging[_ -]?DI|charge|charging|charger|dock|docking|command|cmd|robot_other_setchargingrelay_req|setchargingrelay|chargingrelay|charge_req|goCharge|go_charge|dock_req|robot_task_gotarget_req|gotarget|battery|battery_level|batteryLevel|GetBatteryLevel|robot_status_battery_req|voltage|soc|power low|default|factory|restore|reset|reloadRobodMakeIni|active:false|echoid|robot_core_upgrade_robot_req|upgrade[.]zip|upgradeStatus|startup[.]sh|core is not activated|license inactive|activation failed|rds[.]scene|model_md5|upload|deploy|push|publish|import|POST|PUT|PATCH|user|username|operator|account|client|remote|remote_addr|source|ip|mac"
	mapActionGrep := "map|smap|scene|model|models|robot[.]cp|MD5|md5|checksum|chargeDI|charge[_ -]?DI|chargingDI|charging[_ -]?DI|charge|charging|charger|dock|docking|command|cmd|robot_other_setchargingrelay_req|setchargingrelay|chargingrelay|charge_req|goCharge|go_charge|dock_req|robot_task_gotarget_req|gotarget|battery|battery_level|batteryLevel|GetBatteryLevel|robot_status_battery_req|voltage|soc|power low|default|factory|restore|reset|reloadRobodMakeIni|active:false|echoid|robot_core_upgrade_robot_req|upgrade[.]zip|upgradeStatus|startup[.]sh|core is not activated|license inactive|activation failed|rds[.]scene|model_md5|upload|deploy|push|publish|import|POST|PUT|PATCH"
	run("rds_access_logs", fmt.Sprintf(
		"find /var/log/nginx /var/log/apache2 /var/log/httpd /opt/data/rds /opt/data/rdscore /opt/data/robod /opt/Roboshop -type f"+
			" \\( -iname '*access*.log*' -o -iname '*api*.log*' -o -iname '*http*.log*' -o -iname '*web*.log*' \\) -mmin -10080 -print0 2>/dev/null"+
			" | xargs -0 zgrep -HinEi %q 2>/dev/null"+
			" | grep -Ei %q | tail -n 5000 || true",
		mapAuditGrep, mapActionGrep))
	run("rds_audit_logs", fmt.Sprintf(
		"find /opt/data/rds /opt/data/rdscore /opt/data/robod /opt/Roboshop /var/log -type f"+
			" \\( -iname '*audit*.log*' -o -iname '*history*.log*' -o -iname '*operation*.log*' -o -iname '*operator*.log*' -o -iname '*user*.log*' \\) -mmin -10080 -print0 2>/dev/null"+
			" | xargs -0 zgrep -HinEi %q 2>/dev/null"+
			" | grep -Ei %q | tail -n 5000 || true",
		mapAuditGrep, mapActionGrep))

	if strings.TrimSpace(appLogPaths) != "" {
		for i, path := range strings.Split(appLogPaths, "\n") {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			run(fmt.Sprintf("app_custom_%d", i+1), fmt.Sprintf(
				"find %q -type f \\( -iname '*.log' -o -iname '*.out' -o -iname '*.err' \\) -mmin -10080 -print0 2>/dev/null"+
					" | xargs -0 grep -HinEi %q 2>/dev/null || true",
				path, roboshopGrep+"|oom|killed|reboot|shutdown|backup|network|disk|ssh|failed|fatal"))
		}
	}

	// -- live AMR TCP connections
	run("live_amr_tcp",
		"ss -tnp 2>/dev/null | grep -Ei '10[.]216[.]35|19204|19205|19206|19207|Roboshop|rds|rbk' || true")

	// -- Authoritative per-robot status from RDS Core's MySQL (t_robotstatusrecord).
	// This is the real robot state machine: uuid + status transitions + odometer,
	// keyed by genuine UUID (unlike the 192xx log ports which are protocol
	// channels, not robot slots). The ro_read account is SELECT-only on rds.* and
	// localhost-bound (created by the operator). TSV output, newest rows first.
	// mysql may be absent or the grant missing on some hosts — failures are silent.
	run("mysql_ro_read_defaults",
		"if [ ! -f ~/.my.cnf.ro_read ]; then install -m 600 /dev/null ~/.my.cnf.ro_read && printf '[client]\nuser=ro_read\npassword=ro_read\n' > ~/.my.cnf.ro_read; fi")
	run("rds_robot_status",
		"mysql --defaults-extra-file=~/.my.cnf.ro_read rds -N -B -e "+
			"\"SELECT uuid, COALESCE(vehicle_name,''), new_status, COALESCE(old_status,0), "+
			"started_on, COALESCE(ended_on,started_on), COALESCE(duration,0), "+
			"COALESCE(odo,0), COALESCE(today_odo,0) FROM t_robotstatusrecord "+
			"ORDER BY started_on DESC LIMIT 4000;\" 2>/dev/null || true")

	// -- syslog fallback
	run("syslog", fmt.Sprintf(
		"journalctl --since %q --no-pager -o short-iso 2>/dev/null | grep -Ei 'shutdown|reboot|power|halt|stopped|stopping|failed|failure|error|critical|panic|segfault|oom|out of memory|killed process|watchdog|thermal|acpi|sigterm|sigkill|mysql|postgres|nginx|docker|containerd|fleet|roboshop|rds|seer|robod|java' || true",
		sinceStr))

	// -- kern.log
	run("kern.log", "zgrep -hEi 'oom|out of memory|killed process|panic|segfault|watchdog|I/O error|EXT4|XFS|BTRFS|MCE|NMI|link is down|link is up|failed|error' /var/log/kern.log* 2>/dev/null | tail -n 5000 || true")

	// -- auth.log
	run("auth.log", "zgrep -hEi 'sshd|accepted password|accepted publickey|failed password|session opened|session closed|sudo:' /var/log/auth.log* 2>/dev/null | tail -n 2000 || true")

	// -- Neighbor table: can help map a client IP to a MAC only when the host has
	// recently talked to that client on the same L2 network.
	run("rds_network_neighbors",
		"echo '=ip_neigh='; ip -color=never neigh show 2>/dev/null || ip neigh show 2>/dev/null || true;"+
			" echo '=arp='; arp -an 2>/dev/null || true")

	// -- system info snapshot
	run("system_info",
		"echo '=uptime='; uptime;"+
			" echo '=df='; df -h;"+
			" echo '=free='; free -h;"+
			" echo '=services_failed='; systemctl list-units --type=service --state=failed 2>/dev/null || true;"+
			" echo '=journal_boots='; journalctl --list-boots --no-pager 2>/dev/null || true;"+
			" echo '=shutdown_history='; last -x -F 2>/dev/null | egrep -i 'shutdown|reboot|runlevel|crash' | head -n 50 || true;"+
			" echo '=last_reboot='; last reboot | head -n 5;"+
			" echo '=coredumps='; coredumpctl list 2>/dev/null | tail -n 20 || true")

	// -- MySQL / RDS database health
	run("mysql_health",
		"mysql -e 'SHOW DATABASES;' 2>/dev/null;"+
			" mysql -D rds -e 'SELECT COUNT(*) AS scene_records, MAX(id) AS max_id,"+
			" MAX(create_time) AS last_scene_save FROM t_scene_record;' 2>/dev/null || true")
	run("rds_db_audit",
		"mysql -N -B -e \"SELECT table_schema, table_name, column_name FROM information_schema.columns WHERE table_schema NOT IN ('information_schema','mysql','performance_schema','sys') AND (table_name REGEXP 'map|scene|audit|log|history|operation|operator|user' OR column_name REGEXP 'map|scene|user|operator|account|ip|mac|client|remote|created|updated') ORDER BY table_schema, table_name, ordinal_position LIMIT 300;\" 2>/dev/null || true")

	return logs, nil
}

// FetchProxmoxLogs pulls host, VM, task, backup, HA, storage, and QEMU context
// from a Proxmox/PVE host. It is intentionally best-effort because not every
// PVE install keeps the same log files or enables HA/backup tooling.
func (c *Client) FetchProxmoxLogs(since time.Time, vmid string) map[string]string {
	sinceStr := since.UTC().Format("2006-01-02 15:04:05")
	logs := make(map[string]string)

	run := func(key, cmd string) {
		out, err := c.Run(cmd)
		if err == nil && strings.TrimSpace(out) != "" {
			logs[key] = out
		}
	}

	vmExpr, vmLoop := proxmoxVMSelector(vmid)

	pveGrep := "qemu|kvm|qm |vm |VM |oom|out of memory|killed process|memory|swap|backup|vzdump|pbs|ha-manager|pve-ha|shutdown|reboot|stopped|started|task|disk|smart|zfs|network|dhcp|link is down|link is up|failed|error"
	vmGrep := fmt.Sprintf("UPID.*:%s:|qemu/%s|VM %s|vm:%s|status/(stop|shutdown|start|reset)|qm(stop|shutdown|start|reset):%s|qm (stop|shutdown|start|reset) %s|vzdump:%s|backup|vzdump|lock|qmp|agent|freeze|thaw|oom|out of memory|killed process|%s.scope|qemu.slice|kvm", vmExpr, vmExpr, vmExpr, vmExpr, vmExpr, vmExpr, vmExpr, vmExpr)

	run("proxmox_journal", fmt.Sprintf(
		"journalctl --since %q --no-pager -o short-iso 2>/dev/null | grep -Ei %q || true",
		sinceStr, pveGrep))
	run("proxmox_syslog", fmt.Sprintf(
		"zgrep -hEi %q /var/log/syslog* /var/log/messages* /var/log/daemon.log* /var/log/pvedaemon.log* 2>/dev/null | tail -n 12000 || true",
		pveGrep))
	run("proxmox_tasks", fmt.Sprintf(
		"grep -RniE %q /var/log/pve/tasks /var/log/pveproxy/access.log* /var/log/pvedaemon.log* /var/log/syslog* /var/log/daemon.log* 2>/dev/null | tail -n 12000 || true",
		vmGrep))
	run("proxmox_api_proxy", fmt.Sprintf(
		"zgrep -hEi %q /var/log/pveproxy/access.log* /var/log/pvedaemon.log* 2>/dev/null | tail -n 8000 || true",
		vmGrep+"|POST|PUT|DELETE|API2|root@pam|login|auth"))
	run("proxmox_ha", fmt.Sprintf(
		"zgrep -hEi %q /var/log/syslog* /var/log/daemon.log* /var/log/pve-ha-* 2>/dev/null | tail -n 6000 || true",
		fmt.Sprintf("vm:%s|%s|ha-manager|lrm|crm|fence|recovery|migrate|error|failed", vmExpr, vmExpr)))
	run("proxmox_backup", fmt.Sprintf(
		"zgrep -hEi %q /var/log/vzdump/*.log /var/log/pve/tasks/*/* /var/log/syslog* /var/log/daemon.log* 2>/dev/null | tail -n 10000 || true",
		fmt.Sprintf("vzdump:%s|backup|pbs|snapshot|VM is locked|not running|stopped|failed|error|OK|%s", vmExpr, vmExpr)))
	run("proxmox_qemu", fmt.Sprintf(
		"{ %s echo \"===== VM $id STATUS =====\"; qm status \"$id\" 2>/dev/null || true; echo \"===== VM $id CONFIG =====\"; qm config \"$id\" 2>/dev/null || true; echo \"===== VM $id QEMU LOG =====\"; cat \"/var/log/pve/qemu-server/$id.log\" 2>/dev/null | tail -n 500 || true; done; } | grep -Ei %q || true",
		vmLoop, "name:|memory:|balloon:|status:|oom|out of memory|killed|shutdown|stop|start|reset|error|failed|qmp|agent"))
	run("proxmox_vm_status", fmt.Sprintf(
		"echo '=qm_list='; qm list 2>/dev/null || true; %s echo \"----- VM $id -----\"; qm status \"$id\" 2>/dev/null || true; qm config \"$id\" 2>/dev/null | grep -Ei 'name:|memory:|balloon:|cores:|sockets:' || true; done",
		vmLoop))
	run("proxmox_root_history", fmt.Sprintf(
		"grep -RniE %q /root/.*history /home/*/.*history 2>/dev/null | tail -n 200 || true",
		fmt.Sprintf("qm (stop|shutdown|reset|start|set) %s|qemu/%s|%s|memory|balloon", vmExpr, vmExpr, vmExpr)))
	run("proxmox_host_memory", fmt.Sprintf(
		"echo '=free='; free -h; echo '=swapon='; swapon --show; echo '=vm_memory_config='; %s echo \"----- VM $id -----\"; qm config \"$id\" 2>/dev/null | grep -Ei 'name:|memory:|balloon:' || true; done; echo '=current_vm_rss='; for id in $(qm list 2>/dev/null | awk 'NR>1 && $3==\"running\" {print $1}'); do pid=$(cat /var/run/qemu-server/$id.pid 2>/dev/null); name=$(qm config \"$id\" 2>/dev/null | awk -F': ' '/^name:/ {print $2}'); mem=$(qm config \"$id\" 2>/dev/null | awk -F': ' '/^memory:/ {print $2}'); if [ -n \"$pid\" ]; then rss_kb=$(ps -o rss= -p \"$pid\" | awk '{print $1}'); rss_gb=$(awk \"BEGIN {printf \\\"%%.2f\\\", $rss_kb/1024/1024}\"); echo \"VMID=$id NAME=$name PID=$pid RSS_GB=$rss_gb CONFIG_MB=$mem\"; fi; done; echo '=top_mem='; ps -eo pid,user,%%mem,rss,vsz,cmd --sort=-rss | head -n 25; echo '=journal_oom='; journalctl --since %q --no-pager 2>/dev/null | grep -Ei 'oom|out of memory|killed process|qemu.slice|kvm|[0-9]+.scope' | tail -n 300 || true",
		vmLoop, sinceStr))
	run("proxmox_storage", "echo '=df='; df -h; echo '=zpool='; zpool status 2>/dev/null || true; echo '=smart='; for d in /dev/sd? /dev/nvme?n?; do smartctl -H $d 2>/dev/null; done")
	run("proxmox_boot_history", "echo '=boots='; journalctl --list-boots --no-pager 2>/dev/null || true; echo '=shutdown_history='; last -x -F 2>/dev/null | egrep -i 'shutdown|reboot|runlevel|crash' | head -n 50 || true")

	return logs
}

func proxmoxVMSelector(raw string) (string, string) {
	ids := parseProxmoxVMIDs(raw)
	if len(ids) == 0 {
		return "[0-9]+", "for id in $(qm list 2>/dev/null | awk 'NR>1 {print $1}'); do"
	}
	expr := strings.Join(ids, "|")
	if len(ids) > 1 {
		expr = "(" + expr + ")"
	}
	return expr, "for id in " + strings.Join(ids, " ") + "; do"
}

func parseProxmoxVMIDs(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	seen := map[string]bool{}
	var ids []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		valid := true
		for _, r := range part {
			if r < '0' || r > '9' {
				valid = false
				break
			}
		}
		if !valid || seen[part] {
			continue
		}
		seen[part] = true
		ids = append(ids, part)
	}
	return ids
}
