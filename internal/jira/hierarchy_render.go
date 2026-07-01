package jira

import (
	"fmt"
	"sort"
	"strings"
)

// ChildFilter limits which children are displayed in a hierarchy render.
// The zero value (no category, ExcludeDone false) shows everything.
type ChildFilter struct {
	// Category, when set, keeps only nodes whose statusCategory matches it
	// exactly ("To Do", "In Progress", "Done").
	Category string
	// ExcludeDone keeps only non-Done nodes. Ignored when Category is set.
	ExcludeDone bool
	// Label is the flag phrase shown in "hidden by <label>" notices,
	// e.g. "--open" or "--state todo".
	Label string
}

// active reports whether the filter removes anything.
func (f ChildFilter) active() bool { return f.Category != "" || f.ExcludeDone }

// KeepCategory reports whether an item with the given statusCategory
// ("To Do", "In Progress", "Done") passes the filter.
func (f ChildFilter) KeepCategory(statusCategory string) bool {
	if f.Category != "" {
		return strings.EqualFold(statusCategory, f.Category)
	}
	if f.ExcludeDone {
		return !strings.EqualFold(statusCategory, "Done")
	}
	return true
}

// keep reports whether a node passes the filter.
func (f ChildFilter) keep(n HierarchyNode) bool { return f.KeepCategory(n.StatusCategory) }

// RenderHierarchy returns the plain or colored tree representation.
// colorEnabled controls ANSI; pass ColorsEnabled()-style boolean from the caller.
// filter optionally limits which children are shown (zero value shows all).
func RenderHierarchy(chain HierarchyChain, colorEnabled bool, filter ChildFilter) string {
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
		visible := filterChildren(chain.Siblings, filter)
		truncated := chain.SiblingsTruncated && !filter.active()
		hiddenSibs := len(chain.Siblings) - len(visible)
		var hiddenDeeper int
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
					sortedKids := make([]HierarchyNode, len(chain.Children))
					copy(sortedKids, chain.Children)
					sort.SliceStable(sortedKids, func(a, b int) bool {
						aDone := strings.EqualFold(sortedKids[a].StatusCategory, "Done")
						bDone := strings.EqualFold(sortedKids[b].StatusCategory, "Done")
						return !aDone && bDone
					})
					visKids := filterChildren(sortedKids, filter)
					childTruncated := chain.ChildrenTruncated && !filter.active()
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
							deeper := renderChildSubtree(&sb, visKids[j].Children, filter, colorEnabled, "  "+kidNextPrefix)
							hiddenDeeper += deeper
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
		} else if filter.active() && (hiddenSibs > 0 || hiddenDeeper > 0) {
			total := hiddenSibs + hiddenDeeper
			fmt.Fprintf(&sb, "   (%d hidden by %s filter, %d across all levels)\n", hiddenSibs, filter.Label, total)
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
	sorted := make([]HierarchyNode, len(chain.Children))
	copy(sorted, chain.Children)
	sort.SliceStable(sorted, func(i, j int) bool {
		iDone := strings.EqualFold(sorted[i].StatusCategory, "Done")
		jDone := strings.EqualFold(sorted[j].StatusCategory, "Done")
		return !iDone && jDone
	})

	// Apply status filter.
	visible := filterChildren(sorted, filter)

	if len(visible) == 0 {
		hidden := len(sorted)
		fmt.Fprintf(&sb, "  (no children match %s — %d hidden)\n", filter.Label, hidden)
		return sb.String()
	}

	// ChildrenTruncated refers to the unfiltered fetch cap; if filtering is
	// active we can't know how many server-side results match, so suppress it.
	truncated := chain.ChildrenTruncated && !filter.active()

	// Render the top-level children with recursive subtree. Track hidden counts.
	hiddenLevel1 := len(sorted) - len(visible)
	var hiddenDeeper int

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
			deeper := renderChildSubtree(&sb, visible[i].Children, filter, colorEnabled, "  "+nextPrefix)
			hiddenDeeper += deeper
		}
	}

	if truncated {
		remaining := chain.ChildrenTotal - len(chain.Children)
		fmt.Fprintf(&sb, "   … %d more — rerun with --all to fetch everything\n", remaining)
	} else if filter.active() && (hiddenLevel1 > 0 || hiddenDeeper > 0) {
		total := hiddenLevel1 + hiddenDeeper
		if hiddenDeeper == 0 {
			fmt.Fprintf(&sb, "   (%d hidden by %s filter)\n", hiddenLevel1, filter.Label)
		} else {
			fmt.Fprintf(&sb, "   (%d hidden by %s filter, %d across all levels)\n", hiddenLevel1, filter.Label, total)
		}
	}
	if chain.DescendantsTruncated {
		sb.WriteString("   (some subtrees may be incomplete — rerun with --all to fetch every descendant)\n")
	}

	return sb.String()
}

// renderChildSubtree writes nodes and their descendants to sb, indented by prefix.
// Returns the count of nodes hidden by the filter across all levels rendered here.
// prefix is the accumulated indent string from parent levels (e.g. "  │  │  ").
func renderChildSubtree(sb *strings.Builder, nodes []HierarchyNode, filter ChildFilter, colorEnabled bool, prefix string) int {
	if len(nodes) == 0 {
		// When filtering is active and a parent has no visible children,
		// write a placeholder.
		if filter.active() {
			fmt.Fprintf(sb, "%s└─ (no matching children)\n", prefix)
		}
		return 0
	}

	// Sort: Done last.
	sorted := make([]HierarchyNode, len(nodes))
	copy(sorted, nodes)
	sort.SliceStable(sorted, func(i, j int) bool {
		iDone := strings.EqualFold(sorted[i].StatusCategory, "Done")
		jDone := strings.EqualFold(sorted[j].StatusCategory, "Done")
		return !iDone && jDone
	})

	visible := filterChildren(sorted, filter)
	hiddenHere := len(sorted) - len(visible)

	if len(visible) == 0 {
		// All children hidden by filter.
		if filter.active() {
			fmt.Fprintf(sb, "%s└─ (no matching children)\n", prefix)
		}
		return hiddenHere
	}

	totalHidden := hiddenHere
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
			deeper := renderChildSubtree(sb, visible[i].Children, filter, colorEnabled, nextPrefix)
			totalHidden += deeper
		}
	}
	return totalHidden
}

// filterChildren returns children matching the filter.
// A zero-value (inactive) filter returns the slice unchanged.
func filterChildren(children []HierarchyNode, filter ChildFilter) []HierarchyNode {
	if !filter.active() {
		return children
	}
	out := children[:0:0]
	for _, ch := range children {
		if filter.keep(ch) {
			out = append(out, ch)
		}
	}
	return out
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
// filter applies the same filter as RenderHierarchy.
// Ancestors are included at negative depths; subject at depth 0; children at 1+.
// When siblings are present they appear at depth 1 alongside the subject's children.
func RenderHierarchyFlat(chain HierarchyChain, filter ChildFilter) string {
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
		sorted := make([]HierarchyNode, len(nodes))
		copy(sorted, nodes)
		sort.SliceStable(sorted, func(i, j int) bool {
			iDone := strings.EqualFold(sorted[i].StatusCategory, "Done")
			jDone := strings.EqualFold(sorted[j].StatusCategory, "Done")
			return !iDone && jDone
		})
		visible := filterChildren(sorted, filter)
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
		visible := filterChildren(chain.Siblings, filter)
		for _, sib := range visible {
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
