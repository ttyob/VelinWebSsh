package agent

import "strings"

// commandRequiresApproval keeps automatic execution limited to simple, read-only inspection.
// Unknown commands require approval by default.
func commandRequiresApproval(command string) bool {
	if containsSensitiveReference(command) {
		return true
	}
	segments, ok := splitReadOnlyPipeline(command)
	if !ok || len(segments) == 0 {
		return true
	}
	for _, segment := range segments {
		if !readOnlyCommand(segment) {
			return true
		}
	}
	return false
}

func splitReadOnlyPipeline(command string) ([]string, bool) {
	var segments []string
	start := 0
	var quote byte
	escaped := false
	for index := 0; index < len(command); index++ {
		char := command[index]
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '|':
			if index+1 < len(command) && command[index+1] == '|' {
				return nil, false
			}
			segments = append(segments, command[start:index])
			start = index + 1
		case ';', '&', '>', '<', '`', '\n', '\r':
			return nil, false
		case '$':
			if index+1 < len(command) && command[index+1] == '(' {
				return nil, false
			}
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	segments = append(segments, command[start:])
	return segments, true
}

func readOnlyCommand(segment string) bool {
	fields := strings.Fields(segment)
	if len(fields) == 0 {
		return false
	}
	for len(fields) > 0 && shellAssignment(fields[0]) {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return false
	}
	command := strings.ToLower(fields[0])
	if strings.Contains(command, "/") {
		command = command[strings.LastIndexByte(command, '/')+1:]
	}
	if !readOnlyCommandName(command) {
		return false
	}

	// These commands have mutating subcommands even though the binary itself is common
	// in read-only diagnostics, so only their explicit inspection verbs are allowed.
	if command == "find" {
		return findIsReadOnly(fields[1:])
	}
	if command == "git" || command == "docker" || command == "systemctl" || command == "service" || command == "kubectl" {
		return readOnlySubcommand(fields[1:], command)
	}
	return true
}

func readOnlyCommandName(command string) bool {
	for _, allowed := range []string{
		"cat", "cut", "date", "df", "diff", "dirname", "dmesg", "du", "echo", "file", "find", "free", "getent", "grep", "head", "hostname", "id", "journalctl", "jq", "last", "lsof", "ls", "nproc", "nslookup", "od", "printf", "ps", "pwd", "readlink", "realpath", "seq", "sha256sum", "sort", "ss", "stat", "strings", "tail", "tr", "tree", "tty", "uname", "uniq", "uptime", "users", "vmstat", "w", "wc", "who", "whoami", "which", "whereis", "xxd",
		"git", "docker", "systemctl", "service", "kubectl",
	} {
		if command == allowed {
			return true
		}
	}
	return false
}

func findIsReadOnly(fields []string) bool {
	for _, field := range fields {
		lower := strings.ToLower(field)
		if lower == "-delete" || lower == "-exec" || lower == "-execdir" || lower == "-ok" || lower == "-okdir" || lower == "-fprint" || lower == "-fprintf" || lower == "-fls" {
			return false
		}
		if strings.HasPrefix(lower, "-exec") || strings.HasPrefix(lower, "-ok") || strings.HasPrefix(lower, "-fprint") || strings.HasPrefix(lower, "-fls") {
			return false
		}
	}
	return true
}

func readOnlySubcommand(fields []string, command string) bool {
	for len(fields) > 0 && strings.HasPrefix(fields[0], "-") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return false
	}
	verb := strings.ToLower(fields[0])
	switch command {
	case "git":
		if !containsWord([]string{"branch", "diff", "log", "ls-files", "remote", "rev-parse", "show", "status"}, verb) || containsAny(fields[1:], "--output", "--exec", "--upload-pack", "-d", "-D", "-m", "-M", "-c", "-C") {
			return false
		}
		if verb == "remote" && len(fields) > 1 && !containsWord([]string{"-v", "--verbose", "get-url", "show"}, fields[1]) {
			return false
		}
		return true
	case "docker":
		if verb == "system" {
			return len(fields) > 1 && fields[1] == "df"
		}
		return containsWord([]string{"images", "info", "inspect", "logs", "ps", "stats", "top", "version"}, verb)
	case "systemctl", "service":
		return containsWord([]string{"cat", "is-active", "is-enabled", "list-dependencies", "list-unit-files", "list-units", "show", "status"}, verb)
	case "kubectl":
		return containsWord([]string{"api-resources", "cluster-info", "describe", "get", "logs", "version"}, verb)
	default:
		return false
	}
}

func shellAssignment(value string) bool {
	index := strings.IndexByte(value, '=')
	if index <= 0 {
		return false
	}
	for _, char := range value[:index] {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func containsSensitiveReference(command string) bool {
	lower := strings.ToLower(command)
	for _, marker := range []string{
		"/etc/shadow", "/etc/gshadow", "/root", ".ssh", ".env", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "authorized_keys", "credentials", "credential", "password", "secret", "token", "private_key",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func containsWord(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsAny(values []string, wanted ...string) bool {
	for _, value := range values {
		for _, item := range wanted {
			if value == item || strings.HasPrefix(value, item+"=") {
				return true
			}
		}
	}
	return false
}
