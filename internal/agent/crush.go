package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const (
	crushProviderID           = "velin"
	CrushDefaultContextWindow = 128000
	crushMaxOutputBytes       = 2 * 1024 * 1024
	crushMaxErrorBytes        = 16 * 1024
)

type CrushConfig struct {
	Binary  string
	DataDir string
}

func (m *Manager) Backends() []BackendInfo {
	result := []BackendInfo{{ID: "native", Label: "Velin", Available: true}}
	_, err := m.crushBinary()
	crush := BackendInfo{ID: "crush", Label: "Crush", Available: err == nil}
	if err != nil {
		crush.Reason = "Crush 未安装或不可执行"
	}
	return append(result, crush)
}

func (m *Manager) chatCrush(ctx context.Context, history []ChatMessage, hostContext string, options ChatOptions) (ChatResponse, error) {
	config := m.AIConfig()
	if !config.Configured() {
		return ChatResponse{}, errors.New("AI model service is not configured")
	}
	if len(history) == 0 || len(history) > 40 {
		return ChatResponse{}, errors.New("invalid agent conversation")
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = config.Model
	}
	if len(model) > 256 || strings.ContainsRune(model, '\x00') {
		return ChatResponse{}, errors.New("invalid agent model")
	}
	reasoningEffort := strings.TrimSpace(options.ReasoningEffort)
	if reasoningEffort != "" && reasoningEffort != "low" && reasoningEffort != "medium" && reasoningEffort != "high" {
		return ChatResponse{}, errors.New("invalid reasoning effort")
	}
	for _, item := range history {
		role := strings.TrimSpace(item.Role)
		content := strings.TrimSpace(item.Content)
		if (role != "user" && role != "assistant") || content == "" || len(content) > 32000 {
			return ChatResponse{}, errors.New("invalid agent message")
		}
	}

	binary, err := m.crushBinary()
	if err != nil {
		return ChatResponse{}, fmt.Errorf("Crush backend is unavailable: %w", err)
	}
	workspaceKey := strings.TrimSpace(options.WorkspaceKey)
	if workspaceKey == "" {
		workspaceKey = "default"
	}
	unlock := m.lockCrush(workspaceKey)
	defer unlock()
	runDir, initialized, err := m.createCrushWorkspace(workspaceKey)
	if err != nil {
		return ChatResponse{}, err
	}

	configHome := filepath.Join(runDir, "config")
	workspaceDir := filepath.Join(runDir, "workspace")
	dataDir := filepath.Join(runDir, "data")
	cacheDir := filepath.Join(runDir, "cache")
	homeDir := filepath.Join(runDir, "home")
	for _, path := range []string{filepath.Join(configHome, "crush"), workspaceDir, dataDir, cacheDir, homeDir} {
		if err = os.MkdirAll(path, 0o700); err != nil {
			return ChatResponse{}, fmt.Errorf("prepare Crush workspace: %w", err)
		}
	}
	crushrc := crushRC(config, model, reasoningEffort, crushPromptCacheKey(workspaceKey, model))
	if err = os.WriteFile(filepath.Join(configHome, "crush", "crushrc"), []byte(crushrc), 0o600); err != nil {
		return ChatResponse{}, fmt.Errorf("write Crush configuration: %w", err)
	}

	prompt, err := crushPromptForTurn(history, hostContext, initialized)
	if err != nil {
		return ChatResponse{}, err
	}
	args := []string{
		"--cwd", workspaceDir,
		"--data-dir", dataDir,
		"run", "--quiet", "--model", crushProviderID + "/" + model,
	}
	if initialized {
		args = append(args, "--continue")
	}
	command := exec.CommandContext(ctx, binary, args...)
	command.Stdin = strings.NewReader(prompt)
	command.Env = crushEnvironment(config.APIKey, configHome, cacheDir, homeDir)
	var stdout, stderr cappedBuffer
	stdout.limit = crushMaxOutputBytes
	stderr.limit = crushMaxErrorBytes
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err = command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		message = redactSecret(message, config.APIKey)
		return ChatResponse{}, fmt.Errorf("Crush request failed: %s", message)
	}
	if stdout.truncated {
		return ChatResponse{}, errors.New("Crush returned an oversized response")
	}
	result, err := decodeCrushResponse(stdout.Bytes())
	if err != nil {
		return ChatResponse{}, err
	}
	result.Model = model
	result.Backend = "crush"
	if err = os.WriteFile(filepath.Join(runDir, ".initialized"), []byte("1\n"), 0o600); err != nil {
		return ChatResponse{}, fmt.Errorf("persist Crush session state: %w", err)
	}
	return result, nil
}

func (m *Manager) crushBinary() (string, error) {
	binary := strings.TrimSpace(m.crush.Binary)
	if binary == "" {
		binary = "crush"
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func (m *Manager) createCrushWorkspace(workspaceKey string) (string, bool, error) {
	root := strings.TrimSpace(m.crush.DataDir)
	if root == "" {
		root = filepath.Join(os.TempDir(), "velin-crush")
	}
	hash := sha256.Sum256([]byte(workspaceKey))
	scopeDir := filepath.Join(root, hex.EncodeToString(hash[:12]))
	if err := os.MkdirAll(scopeDir, 0o700); err != nil {
		return "", false, fmt.Errorf("create Crush data directory: %w", err)
	}
	initialized := false
	if _, err := os.Stat(filepath.Join(scopeDir, ".initialized")); err == nil {
		initialized = true
	}
	return scopeDir, initialized, nil
}

func crushRC(config AIConfig, model, reasoningEffort, promptCacheKey string) string {
	baseURL := strings.TrimRight(config.BaseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/chat/completions")
	modelRef := crushProviderID + "/" + model
	extraBody, _ := json.Marshal(map[string]string{"prompt_cache_key": promptCacheKey})
	var script strings.Builder
	script.WriteString("option metrics false\n")
	script.WriteString("option progress false\n")
	script.WriteString("option auto-lsp false\n")
	script.WriteString("option provider-auto-update false\n")
	script.WriteString("option default-providers false\n")
	script.WriteString("option auto-summarize true\n")
	script.WriteString("provider add ")
	script.WriteString(crushProviderID)
	script.WriteString(" --type openai-compat --base-url ")
	script.WriteString(shellQuote(baseURL))
	// Keep the provider cache partition stable across turns while isolating users,
	// hosts, conversations, and model changes through the derived key.
	script.WriteString(" --api-key \"$VELIN_CRUSH_API_KEY\" --discover-models false --extra-body ")
	script.WriteString(shellQuote(string(extraBody)))
	script.WriteByte('\n')
	script.WriteString("model add ")
	script.WriteString(shellQuote(modelRef))
	script.WriteString(" --name ")
	script.WriteString(shellQuote(model))
	script.WriteString(" --context-window ")
	script.WriteString(strconv.Itoa(CrushDefaultContextWindow))
	script.WriteString(" --can-reason true\n")
	script.WriteString("model large ")
	script.WriteString(shellQuote(modelRef))
	if reasoningEffort != "" {
		script.WriteString(" --reasoning-effort ")
		script.WriteString(reasoningEffort)
	}
	script.WriteByte('\n')
	script.WriteString("model small ")
	script.WriteString(shellQuote(modelRef))
	script.WriteByte('\n')
	script.WriteString("permissions deny agent bash crush_info crush_logs job_output job_kill download edit multiedit lsp_diagnostics lsp_references lsp_restart lsp_symbols lsp_definition lsp_call_hierarchy lsp_rename lsp_replace_symbol fetch agentic_fetch glob grep ls question sourcegraph todos view write list_mcp_resources read_mcp_resource\n")
	return script.String()
}

func crushPromptCacheKey(workspaceKey, model string) string {
	hash := sha256.Sum256([]byte("velin-prompt-cache\x00" + workspaceKey + "\x00" + model))
	return "velin-" + hex.EncodeToString(hash[:16])
}

func crushPrompt(history []ChatMessage, hostContext string) (string, error) {
	rawHistory, err := json.Marshal(history)
	if err != nil {
		return "", err
	}
	return `You are the planning backend for Velin SSH Agent. Help the user operate the connected remote host and reply in the user's language.

You cannot execute commands or inspect the host directly. When inspection or an operation is needed, propose SSH commands for Velin. Every proposed command requires explicit user approval before execution. Never claim a command has run unless its result is included in a later user update. Prefer small, auditable commands. Do not propose destructive operations unless the user explicitly requested them.

Return exactly one JSON object with no markdown fence and no text outside it:
{"message":"answer shown to the user","commands":[{"command":"exact shell command","reason":"short purpose shown to the user"}]}

The commands array may be empty and must contain at most 8 items. The message may be empty only when commands are present.

Connected host: ` + hostContext + `
Conversation JSON: ` + string(rawHistory), nil
}

func crushPromptForTurn(history []ChatMessage, hostContext string, continued bool) (string, error) {
	if !continued {
		return crushPrompt(history, hostContext)
	}
	lastAssistant := -1
	for index, item := range history {
		if item.Role == "assistant" {
			lastAssistant = index
		}
	}
	updates := history
	if lastAssistant >= 0 && lastAssistant+1 < len(history) {
		updates = history[lastAssistant+1:]
	}
	if len(updates) == 0 {
		updates = history[len(history)-1:]
	}
	rawUpdates, err := json.Marshal(updates)
	if err != nil {
		return "", err
	}
	return `Continue the existing Velin SSH Agent task. Process only these new user updates and command results; do not repeat a previous answer or previously completed command unless the new results show it is still required.

Return exactly one JSON object with no markdown fence and no text outside it:
{"message":"answer shown to the user","commands":[{"command":"exact shell command","reason":"short purpose shown to the user"}]}

The commands array may be empty and must contain at most 8 items. The message may be empty only when commands are present.

Connected host: ` + hostContext + `
New updates JSON: ` + string(rawUpdates), nil
}

func decodeCrushResponse(raw []byte) (ChatResponse, error) {
	value := strings.TrimSpace(string(raw))
	if strings.HasPrefix(value, "```") {
		if newline := strings.IndexByte(value, '\n'); newline >= 0 && strings.HasSuffix(value, "```") {
			value = strings.TrimSpace(strings.TrimSuffix(value[newline+1:], "```"))
		}
	}
	var envelope struct {
		Message  string `json:"message"`
		Commands []struct {
			Command string `json:"command"`
			Reason  string `json:"reason"`
		} `json:"commands"`
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return ChatResponse{}, errors.New("Crush returned an invalid structured response")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ChatResponse{}, errors.New("Crush returned trailing response data")
	}
	envelope.Message = strings.TrimSpace(envelope.Message)
	if len(envelope.Message) > 64000 || len(envelope.Commands) > 8 {
		return ChatResponse{}, errors.New("Crush returned an invalid structured response")
	}
	result := ChatResponse{Message: envelope.Message, Commands: make([]CommandProposal, 0, len(envelope.Commands))}
	for _, item := range envelope.Commands {
		command := strings.TrimSpace(item.Command)
		reason := strings.TrimSpace(item.Reason)
		if command == "" || len(command) > 6000 || strings.ContainsRune(command, '\x00') || len(reason) > 500 {
			return ChatResponse{}, errors.New("Crush returned an invalid SSH command proposal")
		}
		result.Commands = append(result.Commands, CommandProposal{
			ID: uuid.NewString(), Command: command, Reason: reason,
			RequiresApproval: commandRequiresApproval(command),
		})
	}
	if result.Message == "" && len(result.Commands) == 0 {
		return ChatResponse{}, errors.New("Crush returned an empty response")
	}
	return result, nil
}

func crushEnvironment(apiKey, configHome, cacheDir, homeDir string) []string {
	blocked := map[string]struct{}{
		"HOME": {}, "XDG_CONFIG_HOME": {}, "XDG_CACHE_HOME": {}, "XDG_DATA_HOME": {},
		"VELIN_CRUSH_API_KEY": {}, "DO_NOT_TRACK": {},
	}
	environment := make([]string, 0, len(os.Environ())+7)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if _, found := blocked[key]; found || strings.HasPrefix(key, "CRUSH_") {
			continue
		}
		environment = append(environment, item)
	}
	return append(environment,
		"HOME="+homeDir,
		"XDG_CONFIG_HOME="+configHome,
		"XDG_CACHE_HOME="+cacheDir,
		"XDG_DATA_HOME="+filepath.Join(homeDir, ".local", "share"),
		"VELIN_CRUSH_API_KEY="+apiKey,
		"CRUSH_DISABLE_METRICS=1",
		"DO_NOT_TRACK=1",
	)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func redactSecret(value, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[redacted]")
}

type cappedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(value []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if remaining > len(value) {
			remaining = len(value)
		}
		_, _ = b.buffer.Write(value[:remaining])
	}
	if remaining < len(value) {
		b.truncated = true
	}
	return len(value), nil
}

func (b *cappedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *cappedBuffer) String() string { return b.buffer.String() }
