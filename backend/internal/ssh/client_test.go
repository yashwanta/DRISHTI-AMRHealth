package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestProxmoxVMSelectorDefaultsToAllVMs(t *testing.T) {
	expr, loop := proxmoxVMSelector("")
	if expr != "[0-9]+" {
		t.Fatalf("expr = %q, want all VM regex", expr)
	}
	if loop != "for id in $(qm list 2>/dev/null | awk 'NR>1 {print $1}'); do" {
		t.Fatalf("loop = %q, want qm list loop", loop)
	}
}

func TestProxmoxVMSelectorSupportsSanitizedGlobalList(t *testing.T) {
	expr, loop := proxmoxVMSelector("113, 114\n260003; bad-id 113")
	if expr != "(113|114|260003)" {
		t.Fatalf("expr = %q, want sanitized VM regex", expr)
	}
	if loop != "for id in 113 114 260003; do" {
		t.Fatalf("loop = %q, want sanitized VM loop", loop)
	}
}

func TestKnownHostsCallbackRejectsUnknownHostWithFingerprint(t *testing.T) {
	knownHostsFile := filepath.Join(t.TempDir(), "known_hosts")
	t.Setenv("KNOWN_HOSTS_FILE", knownHostsFile)

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}

	key := signer.PublicKey()
	err = knownHostsCallback()("example.com:22", &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 22}, key)
	if err == nil {
		t.Fatal("expected unknown host error")
	}
	if !strings.Contains(err.Error(), ssh.FingerprintSHA256(key)) {
		t.Fatalf("error %q does not include fingerprint %q", err.Error(), ssh.FingerprintSHA256(key))
	}
}
