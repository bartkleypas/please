package engine

import "github.com/bartkleypas/please/internal/tools"

// Re-export sandbox policies and validators from internal/tools for backward compatibility.
const (
	SandboxPolicyStrict     = tools.SandboxPolicyStrict
	SandboxPolicyStandard   = tools.SandboxPolicyStandard
	SandboxPolicyPermissive = tools.SandboxPolicyPermissive
)

var (
	ValidateSafePath         = tools.ValidateSafePath
	ParseAndValidatePipeline = tools.ParseAndValidatePipeline
)
