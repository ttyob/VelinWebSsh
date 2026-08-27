package agent

import "testing"

func TestCommandRequiresApproval(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{command: "df -h", want: false},
		{command: "du -sh /var/log", want: false},
		{command: "find /var/log -type f -mtime -1 -print", want: false},
		{command: "ps aux | head -20", want: false},
		{command: "docker ps --format '{{.Names}}'", want: false},
		{command: "git status --short", want: false},
		{command: "cat /etc/shadow", want: true},
		{command: "echo ok > /tmp/result", want: true},
		{command: "find /tmp -type f -exec rm {} \\;", want: true},
		{command: "rm -rf /tmp/cache", want: true},
		{command: "sudo systemctl restart nginx", want: true},
		{command: "curl -X POST https://example.test/hook", want: true},
		{command: "python3 -c 'print(1)'", want: true},
		{command: "git remote add origin https://example.test/repo.git", want: true},
		{command: "docker exec app sh -c 'echo changed'", want: true},
	}
	for _, test := range tests {
		if got := commandRequiresApproval(test.command); got != test.want {
			t.Errorf("commandRequiresApproval(%q) = %t, want %t", test.command, got, test.want)
		}
	}
}
