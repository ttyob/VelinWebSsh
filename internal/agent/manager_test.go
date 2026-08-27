package agent

import (
	"strings"
	"testing"
)

func TestParseSnapshot(t *testing.T) {
	value, err := parseSnapshot("hostname\tserver-1\nsystem\tLinux\narch\tx86_64\nkernel\t6.8\nuptime\t3600.5\nload\t0.1\t0.2\t0.3\nmemory\t1000\t250\ndisk\t2000\t500\t1500\n")
	if err != nil {
		t.Fatal(err)
	}
	if value.System.Hostname != "server-1" || value.System.OS != "linux" || value.System.Arch != "amd64" {
		t.Fatalf("unexpected system info: %#v", value.System)
	}
	if value.Memory.UsedBytes != 750*1024 || value.Memory.UsedPercent != 75 {
		t.Fatalf("unexpected memory: %#v", value.Memory)
	}
}

func TestParseProcesses(t *testing.T) {
	values := parseProcesses(" 42 root Ss 1024 /usr/bin/test --flag value\ninvalid\n")
	if len(values) != 1 || values[0].PID != 42 || values[0].MemoryBytes != 1024*1024 || values[0].Command != "/usr/bin/test --flag value" {
		t.Fatalf("unexpected processes: %#v", values)
	}
}

func TestNormalizeRemoteSystem(t *testing.T) {
	if normalizeOS("Darwin") != "macos" || normalizeArch("aarch64") != "arm64" {
		t.Fatal("remote system normalization failed")
	}
}

func TestDockerLoginCommand(t *testing.T) {
	command := dockerLoginCommand("registry.example.com:5000", "user'name")
	if want := "docker login 'registry.example.com:5000' --username 'user'\"'\"'name' --password-stdin"; command != want {
		t.Fatalf("unexpected Docker login command: %q", command)
	}
	if strings.Contains(command, "secret-token") {
		t.Fatal("Docker login command must not contain the password")
	}
	if want := "docker login --username 'user' --password-stdin"; dockerLoginCommand("", "user") != want {
		t.Fatalf("unexpected Docker Hub login command: %q", dockerLoginCommand("", "user"))
	}
}
