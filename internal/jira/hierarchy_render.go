package jira

import (
	"fmt"
	"sort"
	"strings"
)

// childrenDisplayLimit is the max number of child rows in a rendered hierarchy.
const childrenDisplayLimit = 15

// RenderHierarchy returns the plain or colored tree representation.
// colorEnabled controls ANSI; pass ColorsEnabled()-style boolean from the caller.
func RenderHierarchy(chain HierarchyChain, colorEnabled bool) string {
	var sb strings.Builder

	if len(chain.Ancestors) == 0 && len(chain.Children) == 0 {
		// Subject row.
		writeSubjectRow(&sb, chain.Subject, colorEnabled)
		sb.WriteString("(standalone issue — no parent or children)\n")
		return sb.String()
	}

	// Ancestor rows (dim).
	for _, anc := range chain.Ancestors {
		writeAncestorRow(&sb, anc, colorEnabled)
	}

	// Subject row.
	writeSubjectRow(&sb, chain.Subject, colorEnabled)

	// Children error.
	if chain.ChildrenError != "" {
		fmt.Fprintf(&sb, "  (could not fetch children — %s)\n", chain.ChildrenError)
		return sb.String()
	}

	if len(chain.Children) == 0 {
		return sb.String()
	}

	// Sort children: Done last.
	sorted := make([]HierarchyNode, len(chain.Children))
	copy(sorted, chain.Children)
	sort.SliceStable(sorted, func(i, j int) bool {
		iDone := strings.EqualFold(sorted[i].StatusCategory, "Done")
		jDone := strings.EqualFold(sorted[j].StatusCategory, "Done")
		return !iDone && jDone
	})

	// Cap at display limit.
	display := sorted
	if len(sorted) > childrenDisplayLimit {
		display = sorted[:childrenDisplayLimit]
	}

	for i, ch := range display {
		connector := "├─"
		if i == len(display)-1 && len(chain.Children) <= childrenDisplayLimit {
			connector = "└─"
		}
		writeChildRow(&sb, ch, connector, colorEnabled)
	}

	// Overflow hint.
	if len(chain.Children) > childrenDisplayLimit || chain.ChildrenTotal > len(chain.Children) {
		remaining := chain.ChildrenTotal - childrenDisplayLimit
		if remaining < 0 {
			remaining = chain.ChildrenTotal - len(chain.Children)
		}
		fmt.Fprintf(&sb, "   … %d more\n", remaining)
	}

	return sb.String()
}

// writeAncestorRow writes a single dim ancestor row.
// Format: <KEY:14>  <TYPE-BADGE>  <STATUS:14>  <SUMMARY:60>
func writeAncestorRow(sb *strings.Builder, node HierarchyNode, colorEnabled bool) {
	typeBadge := ColorIssueType(node.IssueType, colorEnabled)
	statusStr := ColorStatusName(node.Status, colorEnabled)
	statusVis := len([]rune(TruncateString(node.Status, 14)))
	statusPadded := statusStr + strings.Repeat(" ", max(0, 14-statusVis))
	summary := TruncateString(node.Summary, 60)

	line := fmt.Sprintf("%-14s  %s  %s  %s",
		TruncateString(node.Key, 14),
		typeBadge,
		statusPadded,
		summary)
	sb.WriteString(Dim(line, colorEnabled))
	sb.WriteByte('\n')
}

// writeSubjectRow writes the subject row with a ▶ prefix.
// Format: ▶ <KEY:14>  <TYPE-BADGE>  <STATUS:14>  <SUMMARY:60>
func writeSubjectRow(sb *strings.Builder, node HierarchyNode, colorEnabled bool) {
	typeBadge := ColorIssueType(node.IssueType, colorEnabled)
	statusStr := ColorStatusName(node.Status, colorEnabled)
	statusVis := len([]rune(TruncateString(node.Status, 14)))
	statusPadded := statusStr + strings.Repeat(" ", max(0, 14-statusVis))
	summary := TruncateString(node.Summary, 60)

	keyStr := Bold(TruncateString(node.Key, 14), colorEnabled)
	fmt.Fprintf(sb, "▶ %-14s  %s  %s  %s\n",
		keyStr,
		typeBadge,
		statusPadded,
		summary)
}

// writeChildRow writes a single child row with the given tree connector.
// Format:   ├─ <KEY:14>  <TYPE-BADGE>  <STATUS:14>  <ASSIGNEE:22>  <SUMMARY:50>
func writeChildRow(sb *strings.Builder, node HierarchyNode, connector string, colorEnabled bool) {
	typeBadge := ColorIssueType(node.IssueType, colorEnabled)
	statusStr := ColorStatusName(node.Status, colorEnabled)
	statusVis := len([]rune(TruncateString(node.Status, 14)))
	statusPadded := statusStr + strings.Repeat(" ", max(0, 14-statusVis))

	assignee := node.Assignee
	if assignee == "" {
		assignee = "(unassigned)"
	}
	summary := TruncateString(node.Summary, 50)

	fmt.Fprintf(sb, "  %s %-14s  %s  %s  %-22s  %s\n",
		connector,
		TruncateString(node.Key, 14),
		typeBadge,
		statusPadded,
		TruncateString(assignee, 22),
		summary)
}
