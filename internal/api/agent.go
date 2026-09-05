package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"velin-webssh/internal/agent"
	"velin-webssh/internal/terminal"
)

func (a *API) agentStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.agents.Status(currentUser(r).ID, chi.URLParam(r, "id")))
}

func (a *API) connectAgent(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	hostID := chi.URLParam(r, "id")
	ctx, cancel := contextWithAgentTimeout(r, 20*time.Second)
	defer cancel()
	status, err := a.agents.Connect(ctx, user.ID, hostID)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) agentSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithAgentTimeout(r, 15*time.Second)
	defer cancel()
	value, err := a.agents.Snapshot(ctx, currentUser(r).ID, chi.URLParam(r, "id"))
	if err != nil {
		writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (a *API) agentProcesses(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithAgentTimeout(r, 20*time.Second)
	defer cancel()
	value, err := a.agents.Processes(ctx, currentUser(r).ID, chi.URLParam(r, "id"))
	if err != nil {
		writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(value))
}

func (a *API) agentTerminateSSHSession(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PID      int    `json:"pid"`
		Terminal string `json:"terminal"`
		User     string `json:"user"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.PID < 2 || input.PID > 4194304 || !validRemoteToken(input.Terminal) || !validRemoteToken(input.User) {
		writeError(w, http.StatusBadRequest, "invalid_ssh_session", "SSH 会话参数无效")
		return
	}
	command := fmt.Sprintf(
		"pid=%s; tty=%s; user=%s; actual_tty=$(ps -p \"$pid\" -o tty= 2>/dev/null | tr -d '[:space:]'); actual_user=$(ps -p \"$pid\" -o user= 2>/dev/null | tr -d '[:space:]'); if [ \"$actual_tty\" != \"$tty\" ] || [ \"$actual_user\" != \"$user\" ]; then printf '目标 SSH 会话已变化或不存在\\n' >&2; exit 1; fi; kill -TERM \"$pid\"",
		shellQuote(strconv.Itoa(input.PID)), shellQuote(input.Terminal), shellQuote(input.User),
	)
	a.runSSHSessionAction(w, r, command)
}

func (a *API) agentBanSSHAddress(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Address string `json:"address"`
	}
	if !decode(w, r, &input) {
		return
	}
	address := net.ParseIP(strings.TrimSpace(input.Address))
	if address == nil || address.IsUnspecified() || address.IsMulticast() {
		writeError(w, http.StatusBadRequest, "invalid_ssh_address", "SSH 来源 IP 无效")
		return
	}
	a.runSSHSessionAction(w, r, sshAddressFirewallCommand(address, true))
}

func (a *API) agentUnbanSSHAddress(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Address string `json:"address"`
	}
	if !decode(w, r, &input) {
		return
	}
	address := net.ParseIP(strings.TrimSpace(input.Address))
	if address == nil || address.IsUnspecified() || address.IsMulticast() {
		writeError(w, http.StatusBadRequest, "invalid_ssh_address", "SSH 来源 IP 无效")
		return
	}
	a.runSSHSessionAction(w, r, sshAddressFirewallCommand(address, false))
}

func sshAddressFirewallCommand(address net.IP, block bool) string {
	ip := address.String()
	family, tableCommand := "ipv4", "iptables"
	if address.To4() == nil {
		family, tableCommand = "ipv6", "ip6tables"
	}
	quotedIP := shellQuote(ip)
	if block {
		return fmt.Sprintf(
			"ip=%s; if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -qi active; then ufw insert deny from \"$ip\" comment 'Velin SSH monitor'; elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then firewall-cmd --add-rich-rule=\"rule family='%s' source address='$ip' reject\" --permanent && firewall-cmd --reload; elif command -v %s >/dev/null 2>&1; then %s -C INPUT -s \"$ip\" -j DROP 2>/dev/null || %s -I INPUT -s \"$ip\" -j DROP; else printf '未找到可用防火墙工具（ufw、firewall-cmd 或 %s）\\n' >&2; exit 127; fi",
			quotedIP, family, tableCommand, tableCommand, tableCommand, tableCommand,
		)
	}
	command := fmt.Sprintf(
		"ip=%s; if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -qi active; then yes | ufw delete deny from \"$ip\"; elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then firewall-cmd --remove-rich-rule=\"rule family='%s' source address='$ip' reject\" --permanent && firewall-cmd --reload; elif command -v %s >/dev/null 2>&1; then while %s -C INPUT -s \"$ip\" -j DROP 2>/dev/null; do %s -D INPUT -s \"$ip\" -j DROP || break; done; else printf '未找到可用防火墙工具（ufw、firewall-cmd 或 %s）\\n' >&2; exit 127; fi",
		quotedIP, family, tableCommand, tableCommand, tableCommand, tableCommand,
	)
	return command
}

func (a *API) runSSHSessionAction(w http.ResponseWriter, r *http.Request, command string) {
	ctx, cancel := contextWithAgentTimeout(r, 30*time.Second)
	defer cancel()
	output, runErr := a.agents.Command(ctx, currentUser(r).ID, chi.URLParam(r, "id"), command)
	if runErr != nil {
		if output == "" {
			writeAgentError(w, runErr)
			return
		}
		writeError(w, http.StatusBadGateway, "ssh_session_action_failed", strings.TrimSpace(output+"\n"+runErr.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "output": output})
}

func validRemoteToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && !strings.ContainsRune("._:/-", char) {
			return false
		}
	}
	return true
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (a *API) agentModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithAgentTimeout(r, 35*time.Second)
	defer cancel()
	defaultModel := strings.TrimSpace(a.agents.AIConfig().Model)
	models, err := a.agents.Models(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "not configured") {
			writeError(w, http.StatusServiceUnavailable, "ai_not_configured", "AI 模型服务尚未配置")
			return
		}
		// A model list is optional metadata. Keep the configured model usable
		// when a compatible provider temporarily does not expose /models.
		fallback := make([]agent.ModelInfo, 0, 1)
		if defaultModel != "" {
			fallback = append(fallback, agent.ModelInfo{ID: defaultModel})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"defaultModel":         defaultModel,
			"defaultContextWindow": 0,
			"models":               fallback,
			"backends":             a.agents.Backends(),
			"warning":              err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"defaultModel":         defaultModel,
		"defaultContextWindow": 0,
		"models":               nonNil(models),
		"backends":             a.agents.Backends(),
	})
}

func (a *API) agentBackends(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"backends": a.agents.Backends()})
}

func (a *API) agentChat(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	hostID := chi.URLParam(r, "id")
	var input struct {
		Messages        []agent.ChatMessage `json:"messages"`
		Model           string              `json:"model"`
		ReasoningEffort string              `json:"reasoningEffort"`
		Backend         string              `json:"backend"`
		ConversationID  string              `json:"conversationID"`
	}
	if !decode(w, r, &input) {
		return
	}
	status := a.agents.Status(user.ID, hostID)
	if status.State != "connected" {
		writeError(w, http.StatusConflict, "agent_not_connected", "请先连接 Agent SSH 通道")
		return
	}
	ctx, cancel := contextWithAgentTimeout(r, 95*time.Second)
	defer cancel()
	hostContext := fmt.Sprintf("hostname=%s os=%s arch=%s kernel=%s", status.Hostname, status.OS, status.Arch, status.Kernel)
	workspaceKey := user.ID + ":" + hostID
	if strings.TrimSpace(input.ConversationID) != "" && len(input.ConversationID) <= 128 && !strings.ContainsRune(input.ConversationID, '\x00') {
		workspaceKey += ":" + strings.TrimSpace(input.ConversationID)
	}
	value, err := a.agents.Chat(ctx, input.Messages, hostContext, agent.ChatOptions{
		Model:           input.Model,
		ReasoningEffort: input.ReasoningEffort,
		Backend:         input.Backend,
		WorkspaceKey:    workspaceKey,
		ConversationID:  input.ConversationID,
	})
	if err != nil {
		if strings.Contains(err.Error(), "not configured") {
			writeError(w, http.StatusServiceUnavailable, "ai_not_configured", "AI 模型服务尚未配置")
			return
		}
		writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (a *API) agentCommand(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	hostID := chi.URLParam(r, "id")
	var input struct {
		Command string `json:"command"`
	}
	if !decode(w, r, &input) {
		return
	}
	ctx, cancel := contextWithAgentTimeout(r, 60*time.Second)
	defer cancel()
	output, runErr := a.agents.Command(ctx, user.ID, hostID, input.Command)
	if runErr != nil && output == "" {
		writeAgentError(w, runErr)
		return
	}
	result := map[string]any{"output": output, "success": runErr == nil}
	if runErr != nil {
		result["error"] = runErr.Error()
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) dockerLogin(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var input struct {
		Registry string `json:"registry"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	ctx, cancel := contextWithAgentTimeout(r, 60*time.Second)
	defer cancel()
	output, runErr := a.agents.DockerLogin(ctx, user.ID, chi.URLParam(r, "id"), input.Registry, input.Username, input.Password)
	if runErr != nil {
		writeAgentError(w, runErr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": output, "success": true})
}

func (a *API) disconnectAgent(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	hostID := chi.URLParam(r, "id")
	status := a.agents.Disconnect(user.ID, hostID)
	writeJSON(w, http.StatusOK, status)
}

func writeAgentError(w http.ResponseWriter, err error) {
	var hostKeyError *terminal.HostKeyError
	if errors.As(err, &hostKeyError) {
		writeJSONStatus(w, http.StatusConflict, hostKeyErrorBody(hostKeyError))
		return
	}
	code := "agent_operation_failed"
	status := http.StatusBadGateway
	message := err.Error()
	if strings.Contains(message, "credential required") {
		code = "saved_credential_required"
		status = http.StatusConflict
	}
	writeJSONStatus(w, status, map[string]string{"code": code, "message": message})
}

func contextWithAgentTimeout(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), timeout)
}
