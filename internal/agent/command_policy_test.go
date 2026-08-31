package agent

import "testing"

func TestCommandRequiresApproval(t *testing.T) {
	commands := []string{
		"df -h",
		"ps aux | head -20",
		"docker ps --format '{{.Names}}'",
		"git status --short",
		"echo \"$(touch /tmp/pwned)\"",
		"echo \"`touch /tmp/pwned`\"",
		"PATH=/tmp ls",
		"/tmp/ls",
		"cat /etc/shadow",
		"rm -rf /tmp/cache",
	}
	for _, command := range commands {
		if !commandRequiresApproval(command) {
			t.Errorf("commandRequiresApproval(%q) = false", command)
		}
	}
}
