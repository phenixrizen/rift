package graphview

import (
	"sort"
	"strings"
)

func RenderASCII(graph Graph, maxWidth int) string {
	if maxWidth <= 0 {
		maxWidth = 120
	}
	nodeMap := map[string]Node{}
	children := map[string][]string{}
	incoming := map[string]int{}

	for _, node := range graph.Nodes {
		nodeMap[node.ID] = node
	}
	for _, edge := range graph.Edges {
		children[edge.From] = append(children[edge.From], edge.To)
		incoming[edge.To]++
	}
	for from := range children {
		sort.Slice(children[from], func(i, j int) bool {
			return nodeMap[children[from][i]].Label < nodeMap[children[from][j]].Label
		})
	}

	roots := make([]string, 0)
	for _, node := range graph.Nodes {
		if incoming[node.ID] == 0 {
			roots = append(roots, node.ID)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		return nodeMap[roots[i]].Label < nodeMap[roots[j]].Label
	})

	if len(roots) == 0 {
		return "(no graph nodes)\n"
	}

	lines := make([]string, 0)
	for idx, root := range roots {
		if idx > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, truncate(nodeMap[root].Label, maxWidth))
		appendChildren(root, "", &lines, children, nodeMap, maxWidth)
	}
	return strings.Join(lines, "\n") + "\n"
}

func appendChildren(id, prefix string, lines *[]string, children map[string][]string, nodeMap map[string]Node, maxWidth int) {
	kids := children[id]
	for i, kid := range kids {
		last := i == len(kids)-1
		connector := "|- "
		nextPrefix := prefix + "|  "
		if last {
			connector = "\\- "
			nextPrefix = prefix + "   "
		}
		line := prefix + connector + nodeMap[kid].Label
		*lines = append(*lines, truncate(line, maxWidth))
		appendChildren(kid, nextPrefix, lines, children, nodeMap, maxWidth)
	}
}

func truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		return string(runes[:maxWidth])
	}
	return string(runes[:maxWidth-3]) + "..."
}
