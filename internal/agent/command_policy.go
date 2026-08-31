package agent

// Model-generated shell commands always require a user decision. Shell syntax
// is too expressive to prove read-only with an allowlist, and host output can
// influence the model through prompt injection.
func commandRequiresApproval(_ string) bool {
	return true
}
