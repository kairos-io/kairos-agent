// Package agenterror provides a small typed-error layer and a classifier that
// maps failures to a stable code and an actionable recovery hint.
//
// Two paths feed into it:
//
//   - Call sites that know what went wrong wrap their cause in an *AgentError,
//     carrying an explicit Code and Hint. This is the preferred path: the code
//     is stable across versions, unlike the raw message text.
//   - Everything else is classified heuristically by Classify, which matches
//     known substrings in the error chain. This catches errors bubbling up from
//     dependencies that have no knowledge of this package.
//
// The top-level CLI sink calls Format to render a failure as a short block of
// "Error / Code / Hint" lines so an operator staring at a failed install has a
// next step instead of a bare Go error chain.
package agenterror

import (
	"errors"
	"fmt"
	"strings"
)

// Code is a stable, machine-readable identifier for a class of failure. Codes
// are part of the CLI contract: scripts may match on them, so existing values
// must not change meaning.
type Code string

const (
	// CodeUnknown is used when a failure cannot be classified.
	CodeUnknown Code = "ERR_UNKNOWN"
	// CodeNeedsRoot indicates the command must run as root.
	CodeNeedsRoot Code = "ERR_NEEDS_ROOT"
	// CodeTargetNotFound indicates no install target disk could be resolved.
	CodeTargetNotFound Code = "ERR_TARGET_NOT_FOUND"
	// CodeBadSource indicates a malformed or unprefixed image source.
	CodeBadSource Code = "ERR_BAD_SOURCE"
	// CodeLiveMedia indicates an operation was attempted from live media.
	CodeLiveMedia Code = "ERR_LIVE_MEDIA"
	// CodeNetwork indicates a network/registry connectivity failure.
	CodeNetwork Code = "ERR_NETWORK"
	// CodeNoSpace indicates the target ran out of disk space.
	CodeNoSpace Code = "ERR_NO_SPACE"
	// CodePermission indicates a filesystem/device permission failure.
	CodePermission Code = "ERR_PERMISSION"
	// CodeMissingBinary indicates a required external binary was not found.
	CodeMissingBinary Code = "ERR_MISSING_BINARY"
)

// AgentError wraps a cause with a stable Code and an actionable Hint. Op names
// the operation that failed and is used only for the human-readable message.
type AgentError struct {
	Code  Code
	Op    string
	Hint  string
	Cause error
}

// New builds an *AgentError. cause may be nil.
func New(code Code, op, hint string, cause error) *AgentError {
	return &AgentError{Code: code, Op: op, Hint: hint, Cause: cause}
}

// Error implements error. It renders as "op: cause", or just the cause when op
// is empty, so wrapping stays readable in a chain.
func (e *AgentError) Error() string {
	msg := ""
	if e.Cause != nil {
		msg = e.Cause.Error()
	}
	if e.Op == "" {
		return msg
	}
	return fmt.Sprintf("%s: %s", e.Op, msg)
}

// Unwrap exposes the cause for errors.Is/errors.As.
func (e *AgentError) Unwrap() error { return e.Cause }

// rule maps a lowercased substring of an error message to a code and hint.
type rule struct {
	match string
	code  Code
	hint  string
}

// rules are evaluated in order; the first whose substring is present in the
// (lowercased) error message wins, so more specific patterns come first.
var rules = []rule{
	{
		match: "requires root privileges",
		code:  CodeNeedsRoot,
		hint:  "Re-run the command as root (e.g. with sudo).",
	},
	{
		match: "no device found",
		code:  CodeTargetNotFound,
		hint:  "No install disk was found. Attach a block device and confirm it appears in `lsblk`. Re-run with --debug for the full detection trace.",
	},
	{
		match: "no target device",
		code:  CodeTargetNotFound,
		hint:  "No install disk was found. Attach a block device and confirm it appears in `lsblk`. Re-run with --debug for the full detection trace.",
	},
	{
		match: "does not match any of oci",
		code:  CodeBadSource,
		hint:  "Prefix the source with its type, e.g. oci:repo/image:tag, dir:/path, file:/path.img, or ocifile:/path.tar.",
	},
	{
		match: "upgrade from live media",
		code:  CodeLiveMedia,
		hint:  "Boot the installed system (not the live ISO) before upgrading.",
	},
	{
		match: "not found in path",
		code:  CodeMissingBinary,
		hint:  "A required external tool is missing. Install it or ensure it is on PATH.",
	},
	{
		match: "no space left on device",
		code:  CodeNoSpace,
		hint:  "Free space on the target disk/partition, or use a smaller image.",
	},
	{
		match: "connection refused",
		code:  CodeNetwork,
		hint:  "Check network connectivity, the registry URL, and any proxy settings. For an insecure/self-signed registry pass --allow-insecure-registries.",
	},
	{
		match: "no such host",
		code:  CodeNetwork,
		hint:  "Check network connectivity, the registry URL, and any proxy settings. For an insecure/self-signed registry pass --allow-insecure-registries.",
	},
	{
		match: "dial tcp",
		code:  CodeNetwork,
		hint:  "Check network connectivity, the registry URL, and any proxy settings. For an insecure/self-signed registry pass --allow-insecure-registries.",
	},
	{
		match: "tls handshake",
		code:  CodeNetwork,
		hint:  "Check network connectivity, the registry URL, and any proxy settings. For an insecure/self-signed registry pass --allow-insecure-registries.",
	},
	{
		match: "permission denied",
		code:  CodePermission,
		hint:  "Check file/device permissions; the agent likely needs root.",
	},
}

// Classify resolves err to a code and hint. An explicit *AgentError anywhere in
// the chain takes precedence over the heuristic substring rules. The boolean is
// false when err is nil or no classification could be made.
func Classify(err error) (Code, string, bool) {
	if err == nil {
		return CodeUnknown, "", false
	}

	var ae *AgentError
	if errors.As(err, &ae) && ae.Code != "" {
		return ae.Code, ae.Hint, true
	}

	msg := strings.ToLower(err.Error())
	for _, r := range rules {
		if strings.Contains(msg, r.match) {
			return r.code, r.hint, true
		}
	}

	return CodeUnknown, "", false
}

// Format renders err as a short, operator-facing block:
//
//	Error: <message>
//	Code:  <code>   (only when classified)
//	Hint:  <hint>   (only when a hint is available)
//
// It returns an empty string for a nil error or an empty-message sentinel
// error, so callers can suppress output in those cases.
func Format(err error) string {
	if err == nil || err.Error() == "" {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Error: %s", err.Error())

	if code, hint, matched := Classify(err); matched {
		fmt.Fprintf(&b, "\nCode:  %s", code)
		if hint != "" {
			fmt.Fprintf(&b, "\nHint:  %s", hint)
		}
	}

	return b.String()
}
