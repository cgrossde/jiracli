package jira

import (
	"fmt"
	"sort"
	"strings"
)

// ChildFilter limits which children are counted/shown for a hierarchy subject.
// The zero value (no category, ExcludeDone false) matches everything.
//
// Filtering is applied server-side: JQLClause() produces a predicate that is
// appended to every children/sibling/descendant query so the Jira-reported
// totals and pagination already reflect the filter. This keeps plain-text and
// --json output identical and correct even when a fetch is capped (a
// client-side filter over a capped page would silently under-count). The one
// exception is inline sub-tasks, which arrive embedded with the subject (never
// paginated) and are therefore filtered client-side via KeepCategory.
type ChildFilter struct {
	// Category, when set, keeps only nodes whose statusCategory matches it
	// exactly ("To Do", "In Progress", "Done").
	Category string
	// ExcludeDone keeps only non-Done nodes. Ignored when Category is set.
	ExcludeDone bool
	// Label is the flag phrase shown in filter-related notices,
	// e.g. "--open" or "--state todo".
	Label string
}

// KeepCategory reports whether an item with the given statusCategory
// ("To Do", "In Progress", "Done") passes the filter. Used for the inline
// sub-task path, which is filtered client-side.
func (f ChildFilter) KeepCategory(statusCategory string) bool {
	if f.Category != "" {
		return strings.EqualFold(statusCategory, f.Category)
	}
	if f.ExcludeDone {
		return !strings.EqualFold(statusCategory, "Done")
	}
	return true
}

// JQLClause returns a JQL fragment (prefixed with " AND ") that restricts a
// query to the filter's status category, or "" when the filter is inactive.
// The quoted-name syntax matches the rest of the codebase (see DefaultOpenFilter).
func (f ChildFilter) JQLClause() string {
	if f.Category != "" {
		return ` AND statusCategory = "` + f.Category + `"`
	}
	if f.ExcludeDone {
		return ` AND statusCategory != "Done"`
	}
	return ""
}

// RenderHierarchy returns the plain or colored tree representation.
// colorEnabled controls ANSI; pass ColorsEnabled()-style boolean from the caller.
//
// The chain is expected to be already filtered (status filtering happens
// server-side in BuildHierarchy). The renderer never drops nodes — it draws
// exactly what it is given, so plain-text and --json reflect the same data.
func RenderHierarchy(chain HierarchyChain, colorEnabled bool) string {
	var sb strings.Builder

	if len(chain.Ancestors) == 0 && len(chain.Children) == 0 && len(chain.Siblings) == 0 {
		writeSubjectRow(&sb, chain.Subject, colorEnabled)
		sb.WriteString("(standalone issue — no parent or children)\n")
		return sb.String()
	}

	// Ancestor rows (dim).
	for _, anc := range chain.Ancestors {
		writeAncestorRow(&sb, anc, colorEnabled)
	}

	// When siblings are present, render them as a unified block where the subject
	// is marked with ▶ and its children expand inline below it.
	if len(chain.Siblings) > 0 {
		visible := chain.Siblings
		truncated := chain.SiblingsTruncated
		for i := range visible {
			sib := visible[i]
			isLast := i == len(visible)-1 && !truncated
			connector := "├─"
			if isLast {
				connector = "└─"
			}
			nextPrefix := "│  "
			if isLast {
				nextPrefix = "   "
			}
			if sib.IsSubject {
				// Write subject row inline as a sibling entry with ▶.
				writeSiblingSubjectRow(&sb, sib, connector, "  ", colorEnabled)
				// Expand subject's own children indented under it.
				if chain.ChildrenError != "" {
					fmt.Fprintf(&sb, "  %s  (could not fetch children — %s)\n", nextPrefix, chain.ChildrenError)
				} else if len(chain.Children) > 0 {
					visKids := make([]HierarchyNode, len(chain.Children))
					copy(visKids, chain.Children)
					sort.SliceStable(visKids, func(a, b int) bool {
						aDone := strings.EqualFold(visKids[a].StatusCategory, "Done")
						bDone := strings.EqualFold(visKids[b].StatusCategory, "Done")
						return !aDone && bDone
					})
					childTruncated := chain.ChildrenTruncated
					for j := range visKids {
						kidLast := j == len(visKids)-1 && !childTruncated
						kidConnector := "├─"
						if kidLast {
							kidConnector = "└─"
						}
						kidNextPrefix := nextPrefix + "│  "
						if kidLast {
							kidNextPrefix = nextPrefix + "   "
						}
						writeChildRow(&sb, visKids[j], kidConnector, "  "+nextPrefix, colorEnabled)
						if visKids[j].Children != nil {
							renderChildSubtree(&sb, visKids[j].Children, colorEnabled, "  "+kidNextPrefix)
						}
					}
					if childTruncated {
						remaining := chain.ChildrenTotal - len(chain.Children)
						fmt.Fprintf(&sb, "  %s… %d more children — rerun with --all\n", nextPrefix, remaining)
					}
				}
			} else {
				writeChildRow(&sb, sib, connector, "  ", colorEnabled)
			}
		}
		if truncated {
			remaining := chain.SiblingsTotal - len(chain.Siblings)
			fmt.Fprintf(&sb, "   … %d more siblings — rerun with --all to fetch everything\n", remaining)
		}
		if chain.DescendantsTruncated {
			sb.WriteString("   (some subtrees may be incomplete — rerun with --all to fetch every descendant)\n")
		}
		return sb.String()
	}

	// No siblings: original subject + children rendering.
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
	visible := make([]HierarchyNode, len(chain.Children))
	copy(visible, chain.Children)
	sort.SliceStable(visible, func(i, j int) bool {
		iDone := strings.EqualFold(visible[i].StatusCategory, "Done")
		jDone := strings.EqualFold(visible[j].StatusCategory, "Done")
		return !iDone && jDone
	})

	truncated := chain.ChildrenTruncated

	for i := range visible {
		connector := "├─"
		if i == len(visible)-1 && !truncated {
			connector = "└─"
		}
		var nextPrefix string
		if i == len(visible)-1 && !truncated {
			nextPrefix = "   " // 3 spaces (last child — no bar)
		} else {
			nextPrefix = "│  " // bar + 2 spaces
		}
		writeChildRow(&sb, visible[i], connector, "  ", colorEnabled)
		// Children is non-nil only when descent was attempted (depth >= 2).
		// nil means "not fetched", so skip the subtree call entirely for leaves.
		if visible[i].Children != nil {
			renderChildSubtree(&sb, visible[i].Children, colorEnabled, "  "+nextPrefix)
		}
	}

	if truncated {
		remaining := chain.ChildrenTotal - len(chain.Children)
		fmt.Fprintf(&sb, "   … %d more — rerun with --all to fetch everything\n", remaining)
	}
	if chain.DescendantsTruncated {
		sb.WriteString("   (some subtrees may be incomplete — rerun with --all to fetch every descendant)\n")
	}

	return sb.String()
}

// renderChildSubtree writes nodes and their descendants to sb, indented by prefix.
// prefix is the accumulated indent string from parent levels (e.g. "  │  │  ").
// The nodes are assumed already filtered (server-side); the renderer draws them all.
func renderChildSubtree(sb *strings.Builder, nodes []HierarchyNode, colorEnabled bool, prefix string) {
	if len(nodes) == 0 {
		return
	}

	// Sort: Done last.
	visible := make([]HierarchyNode, len(nodes))
	copy(visible, nodes)
	sort.SliceStable(visible, func(i, j int) bool {
		iDone := strings.EqualFold(visible[i].StatusCategory, "Done")
		jDone := strings.EqualFold(visible[j].StatusCategory, "Done")
		return !iDone && jDone
	})

	for i := range visible {
		connector := "├─"
		var nextPrefix string
		if i == len(visible)-1 {
			connector = "└─"
			nextPrefix = prefix + "   "
		} else {
			nextPrefix = prefix + "│  "
		}
		writeChildRow(sb, visible[i], connector, prefix, colorEnabled)
		// Only recurse when Children is non-nil (descent was attempted at this level).
		if visible[i].Children != nil {
			renderChildSubtree(sb, visible[i].Children, colorEnabled, nextPrefix)
		}
	}
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

// writeSiblingSubjectRow writes the subject node when it appears inline among siblings.
// Uses ▶ instead of the tree connector to distinguish it visually.
func writeSiblingSubjectRow(sb *strings.Builder, node HierarchyNode, connector string, prefix string, colorEnabled bool) {
	typeBadge := ColorIssueType(node.IssueType, colorEnabled)
	statusStr := ColorStatusName(node.Status, colorEnabled)
	statusVis := len([]rune(TruncateString(node.Status, 14)))
	statusPadded := statusStr + strings.Repeat(" ", max(0, 14-statusVis))

	assignee := node.Assignee
	if assignee == "" {
		assignee = "(unassigned)"
	}
	summary := TruncateString(node.Summary, 50)
	keyStr := Bold(TruncateString(node.Key, 14), colorEnabled)
	fmt.Fprintf(sb, "%s▶ %-14s  %s  %s  %-22s  %s\n",
		prefix,
		keyStr,
		typeBadge,
		statusPadded,
		TruncateString(assignee, 22),
		summary)
}

// writeChildRow writes a single child row with the given tree connector and prefix.
// Format: <prefix><connector> <KEY:14>  <TYPE-BADGE>  <STATUS:14>  <ASSIGNEE:22>  <SUMMARY:50>
func writeChildRow(sb *strings.Builder, node HierarchyNode, connector string, prefix string, colorEnabled bool) {
	typeBadge := ColorIssueType(node.IssueType, colorEnabled)
	statusStr := ColorStatusName(node.Status, colorEnabled)
	statusVis := len([]rune(TruncateString(node.Status, 14)))
	statusPadded := statusStr + strings.Repeat(" ", max(0, 14-statusVis))

	assignee := node.Assignee
	if assignee == "" {
		assignee = "(unassigned)"
	}
	summary := TruncateString(node.Summary, 50)

	fmt.Fprintf(sb, "%s%s %-14s  %s  %s  %-22s  %s\n",
		prefix,
		connector,
		TruncateString(node.Key, 14),
		typeBadge,
		statusPadded,
		TruncateString(assignee, 22),
		summary)
}

// RenderHierarchyFlat returns a flat, tab-separated table of all nodes in DFS
// order: depth, key, type, status, assignee, summary, isSubject.
// A header row is always emitted first.
// The chain is assumed already filtered (server-side); every node is emitted.
// Ancestors are included at negative depths; subject at depth 0; children at 1+.
// When siblings are present they appear at depth 1 alongside the subject's children.
func RenderHierarchyFlat(chain HierarchyChain) string {
	var sb strings.Builder
	sb.WriteString("depth\tkey\ttype\tstatus\tassignee\tsummary\n")

	// Ancestors at depth < 0.
	d := -len(chain.Ancestors)
	for _, anc := range chain.Ancestors {
		fmt.Fprintf(&sb, "%d\t%s\t%s\t%s\t\t%s\n", d, anc.Key, anc.IssueType, anc.Status, anc.Summary)
		d++
	}

	// Subject at depth 0.
	subj := chain.Subject
	assignee := subj.Assignee
	if assignee == "" {
		assignee = "(unassigned)"
	}
	fmt.Fprintf(&sb, "%d\t%s\t%s\t%s\t%s\t%s\n", 0, subj.Key, subj.IssueType, subj.Status, assignee, subj.Summary)

	// Children and their descendants, depth-first.
	var walkFlat func(nodes []HierarchyNode, depth int)
	walkFlat = func(nodes []HierarchyNode, depth int) {
		// Sort: Done last (same as tree view).
		visible := make([]HierarchyNode, len(nodes))
		copy(visible, nodes)
		sort.SliceStable(visible, func(i, j int) bool {
			iDone := strings.EqualFold(visible[i].StatusCategory, "Done")
			jDone := strings.EqualFold(visible[j].StatusCategory, "Done")
			return !iDone && jDone
		})
		for _, n := range visible {
			a := n.Assignee
			if a == "" {
				a = "(unassigned)"
			}
			fmt.Fprintf(&sb, "%d\t%s\t%s\t%s\t%s\t%s\n", depth, n.Key, n.IssueType, n.Status, a, n.Summary)
			if n.Children != nil {
				walkFlat(n.Children, depth+1)
			}
		}
	}

	// Siblings appear at depth 1; subject's children appear at depth 1 nested under subject.
	// In flat mode siblings and subject's children are interleaved at the same level.
	if len(chain.Siblings) > 0 {
		for _, sib := range chain.Siblings {
			if sib.IsSubject {
				// Subject's children follow at depth 1.
				walkFlat(chain.Children, 1)
			} else {
				a := sib.Assignee
				if a == "" {
					a = "(unassigned)"
				}
				fmt.Fprintf(&sb, "%d\t%s\t%s\t%s\t%s\t%s\n", 1, sib.Key, sib.IssueType, sib.Status, a, sib.Summary)
				if sib.Children != nil {
					walkFlat(sib.Children, 2)
				}
			}
		}
	} else {
		walkFlat(chain.Children, 1)
	}

	if chain.DescendantsTruncated {
		sb.WriteString("# (some subtrees may be incomplete — rerun with --all)\n")
	}
	return sb.String()
}
