package jira

import (
	"fmt"
	"strings"
)

// FieldOp is the mutation operator in a field spec token.
type FieldOp int

const (
	OpReplace FieldOp = iota // "="  — replace (or set) the field value
	OpAdd                    // "+=" — append to a multi-value field
	OpRemove                 // "-=" — remove from a multi-value field
)

// FieldSpec is the parsed form of a single NAME<op>VALUE token.
type FieldSpec struct {
	Name string // field name or id (before the operator)
	Op   FieldOp
	Raw  string // value as typed; may start with "@" to indicate a file path
}

// ParseFieldSpec splits one token of the form NAME<op>VALUE.
// Operator detection order (first match wins, left-to-right): "+=", "-=", "=".
// Returns a descriptive error when no operator is present.
func ParseFieldSpec(token string) (FieldSpec, error) {
	if idx := strings.Index(token, "+="); idx >= 0 {
		return FieldSpec{Name: token[:idx], Op: OpAdd, Raw: token[idx+2:]}, nil
	}
	if idx := strings.Index(token, "-="); idx >= 0 {
		return FieldSpec{Name: token[:idx], Op: OpRemove, Raw: token[idx+2:]}, nil
	}
	if idx := strings.Index(token, "="); idx >= 0 {
		return FieldSpec{Name: token[:idx], Op: OpReplace, Raw: token[idx+1:]}, nil
	}
	return FieldSpec{}, fmt.Errorf(
		"invalid field spec %q — expected name=value, name+=value, or name-=value (see: jiracli field set --help)",
		token,
	)
}

// String returns the operator symbol for display in effect lines.
func (op FieldOp) String() string {
	switch op {
	case OpAdd:
		return "+="
	case OpRemove:
		return "-="
	default:
		return "="
	}
}
