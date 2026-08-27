package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrushChatUsesIsolatedControlledProcess(t *testing.T) {
	captureDir := t.TempDir()
	binary := fakeCrushBinary(t)
	t.Setenv("VELIN_TEST_CAPTURE_DIR", captureDir)
	t.Setenv("VELIN_TEST_RESPONSE", `{"message":"需要检查磁盘。","commands":[{"command":"df -h","reason":"查看磁盘空间"}]}`)

	manager := NewManager(nil,
		AIConfig{BaseURL: "https://models.example.test/v1/chat/completions", APIKey: "super-secret-key", Model: "model-a"},
		CrushConfig{Binary: binary, DataDir: filepath.Join(t.TempDir(), "crush")},
	)
	result, err := manager.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "检查磁盘"}}, "hostname=server-1 os=linux", ChatOptions{
		Backend: "crush", ReasoningEffort: "high", WorkspaceKey: "user-1:host-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Backend != "crush" || result.Model != "model-a" || result.Message == "" || len(result.Commands) != 1 {
		t.Fatalf("unexpected Crush result: %#v", result)
	}
	if result.Commands[0].ID == "" || result.Commands[0].Command != "df -h" {
		t.Fatalf("unexpected command proposal: %#v", result.Commands[0])
	}
	if result.Commands[0].RequiresApproval {
		t.Fatalf("read-only command unexpectedly requires approval: %#v", result.Commands[0])
	}

	args := readTestCapture(t, captureDir, "args")
	config := readTestCapture(t, captureDir, "crushrc")
	prompt := readTestCapture(t, captureDir, "prompt")
	if !strings.Contains(args, "run\n--quiet\n--model\nvelin/model-a") {
		t.Fatalf("unexpected Crush arguments: %q", args)
	}
	if strings.Contains(args, "检查磁盘") || strings.Contains(args, "super-secret-key") {
		t.Fatalf("prompt or API key leaked into argv: %q", args)
	}
	if strings.Contains(config, "super-secret-key") || !strings.Contains(config, `--api-key "$VELIN_CRUSH_API_KEY"`) {
		t.Fatalf("API key was not kept out of crushrc: %q", config)
	}
	if !strings.Contains(config, "prompt_cache_key") || !strings.Contains(config, crushPromptCacheKey("user-1:host-1", "model-a")) {
		t.Fatalf("stable prompt cache key was not configured: %q", config)
	}
	if !strings.Contains(config, "option auto-summarize true") {
		t.Fatalf("automatic context summarization was not enabled: %q", config)
	}
	for _, denied := range []string{"bash", "edit", "write", "fetch", "download"} {
		if !strings.Contains(config, denied) {
			t.Fatalf("dangerous tool %q was not denied: %q", denied, config)
		}
	}
	if !strings.Contains(prompt, "检查磁盘") || !strings.Contains(prompt, "hostname=server-1") {
		t.Fatalf("conversation was not passed on stdin: %q", prompt)
	}
	if got := readTestCapture(t, captureDir, "api-key"); got != "super-secret-key" {
		t.Fatalf("API key was not passed through the isolated environment: %q", got)
	}

	_, err = manager.Chat(context.Background(), []ChatMessage{
		{Role: "user", Content: "检查磁盘"},
		{Role: "assistant", Content: `{"message":"需要检查磁盘。","commands":[{"command":"df -h","reason":"查看磁盘空间"}]}`},
		{Role: "user", Content: "用户已确认并执行 SSH 命令：\ndf -h\n\n执行结果：\n/dev/sda 20G 10G 10G 50%"},
	}, "hostname=server-1 os=linux", ChatOptions{
		Backend: "crush", WorkspaceKey: "user-1:host-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if args := readTestCapture(t, captureDir, "args"); !strings.Contains(args, "--continue") {
		t.Fatalf("persistent Crush session was not continued: %q", args)
	}
	prompt = readTestCapture(t, captureDir, "prompt")
	if strings.Contains(prompt, "检查磁盘") || !strings.Contains(prompt, "/dev/sda") {
		t.Fatalf("continued Crush prompt did not contain only new results: %q", prompt)
	}
}

func TestCrushChatRejectsInvalidStructuredResponse(t *testing.T) {
	binary := fakeCrushBinary(t)
	t.Setenv("VELIN_TEST_CAPTURE_DIR", t.TempDir())
	t.Setenv("VELIN_TEST_RESPONSE", `not json`)
	manager := NewManager(nil,
		AIConfig{BaseURL: "https://models.example.test/v1", Model: "model-a"},
		CrushConfig{Binary: binary, DataDir: t.TempDir()},
	)
	_, err := manager.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hello"}}, "test host", ChatOptions{Backend: "crush"})
	if err == nil || !strings.Contains(err.Error(), "invalid structured response") {
		t.Fatalf("expected structured response error, got %v", err)
	}
}

func TestCrushChatRedactsAPIKeyFromErrors(t *testing.T) {
	binary := fakeCrushBinary(t)
	t.Setenv("VELIN_TEST_CAPTURE_DIR", t.TempDir())
	t.Setenv("VELIN_TEST_RESPONSE", `{}`)
	t.Setenv("VELIN_TEST_FAIL", "1")
	manager := NewManager(nil,
		AIConfig{BaseURL: "https://models.example.test/v1", APIKey: "secret-in-stderr", Model: "model-a"},
		CrushConfig{Binary: binary, DataDir: t.TempDir()},
	)
	_, err := manager.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hello"}}, "test host", ChatOptions{Backend: "crush"})
	if err == nil || strings.Contains(err.Error(), "secret-in-stderr") || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("expected redacted Crush error, got %v", err)
	}
}

func TestBackendsReportsMissingCrush(t *testing.T) {
	manager := NewManager(nil, AIConfig{}, CrushConfig{Binary: filepath.Join(t.TempDir(), "missing-crush")})
	backends := manager.Backends()
	if len(backends) != 2 || !backends[0].Available || backends[1].Available || backends[1].Reason == "" {
		t.Fatalf("unexpected backend capabilities: %#v", backends)
	}
}

func TestDecodeCrushResponseRejectsUnsafeProposal(t *testing.T) {
	_, err := decodeCrushResponse([]byte(`{"message":"","commands":[{"command":"echo \u0000 bad","reason":"bad"}]}`))
	if err == nil {
		t.Fatal("expected invalid command proposal error")
	}
}

func fakeCrushBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crush")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$@" > "$VELIN_TEST_CAPTURE_DIR/args"
cat > "$VELIN_TEST_CAPTURE_DIR/prompt"
cp "$XDG_CONFIG_HOME/crush/crushrc" "$VELIN_TEST_CAPTURE_DIR/crushrc"
printf '%s' "$VELIN_CRUSH_API_KEY" > "$VELIN_TEST_CAPTURE_DIR/api-key"
if [ "${VELIN_TEST_FAIL:-}" = "1" ]; then
  printf 'provider rejected key %s\n' "$VELIN_CRUSH_API_KEY" >&2
  exit 1
fi
printf '%s' "$VELIN_TEST_RESPONSE"
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func readTestCapture(t *testing.T, dir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
