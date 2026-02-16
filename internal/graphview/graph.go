package graphview

import (
	"sort"
	"strings"

	"github.com/phenixrizen/rift/internal/state"
)

type Options struct {
	Env        string
	Account    string
	Role       string
	Region     string
	Cluster    string
	Namespaces bool
	Depth      int
}

type Node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Layer int    `json:"layer"`
}

type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

func Build(st state.State, opts Options) Graph {
	if opts.Depth < 2 {
		opts.Depth = 2
	}
	if opts.Depth > 4 {
		opts.Depth = 4
	}
	if opts.Env == "" {
		opts.Env = "all"
	}

	nodes := map[string]Node{}
	edges := map[string]Edge{}

	addNode := func(id, label, kind string, layer int) {
		if _, ok := nodes[id]; ok {
			return
		}
		nodes[id] = Node{ID: id, Label: label, Kind: kind, Layer: layer}
	}
	addEdge := func(from, to string) {
		k := from + "->" + to
		if _, ok := edges[k]; ok {
			return
		}
		edges[k] = Edge{From: from, To: to}
	}

	roleRows := filterRoles(st.Roles, opts)
	clusterRows := filterClusters(st.Clusters, opts)

	accountsByEnv := map[string]map[string]struct{}{}
	for _, role := range roleRows {
		if accountsByEnv[role.Env] == nil {
			accountsByEnv[role.Env] = map[string]struct{}{}
		}
		accountsByEnv[role.Env][role.AccountID] = struct{}{}
	}
	for _, cluster := range clusterRows {
		if accountsByEnv[cluster.Env] == nil {
			accountsByEnv[cluster.Env] = map[string]struct{}{}
		}
		accountsByEnv[cluster.Env][cluster.AccountID] = struct{}{}
	}

	envs := make([]string, 0, len(accountsByEnv))
	for env := range accountsByEnv {
		envs = append(envs, env)
	}
	sort.Strings(envs)

	for _, env := range envs {
		envID := "env:" + env
		addNode(envID, env+"-accounts ("+itoa(len(accountsByEnv[env]))+")", "env", 0)
	}

	for _, role := range roleRows {
		envID := "env:" + role.Env
		accountID := "acct:" + role.Env + ":" + role.AccountID
		accountLabel := role.AccountName
		if strings.TrimSpace(accountLabel) == "" {
			accountLabel = role.AccountID
		} else {
			accountLabel = accountLabel + " (" + role.AccountID + ")"
		}
		addNode(accountID, accountLabel, "account", 1)
		addEdge(envID, accountID)

		if opts.Depth >= 2 {
			roleID := "role:" + role.Env + ":" + role.AccountID + ":" + role.RoleName
			addNode(roleID, role.RoleName, "role", 2)
			addEdge(accountID, roleID)
		}
	}

	if opts.Depth >= 3 {
		for _, cluster := range clusterRows {
			roleID := "role:" + cluster.Env + ":" + cluster.AccountID + ":" + cluster.RoleName
			clusterID := "cluster:" + cluster.Env + ":" + cluster.AccountID + ":" + cluster.RoleName + ":" + cluster.Region + ":" + cluster.ClusterName
			addNode(clusterID, cluster.ClusterName+" ["+cluster.Region+"]", "cluster", 3)
			addEdge(roleID, clusterID)

			if opts.Depth >= 4 && opts.Namespaces {
				namespaces := normalizeNamespaces(cluster)
				for _, ns := range namespaces {
					nsID := clusterID + ":ns:" + ns
					addNode(nsID, ns, "namespace", 4)
					addEdge(clusterID, nsID)
				}
			}
		}
	}

	out := Graph{
		Nodes: make([]Node, 0, len(nodes)),
		Edges: make([]Edge, 0, len(edges)),
	}
	for _, node := range nodes {
		out.Nodes = append(out.Nodes, node)
	}
	for _, edge := range edges {
		out.Edges = append(out.Edges, edge)
	}
	sort.Slice(out.Nodes, func(i, j int) bool {
		if out.Nodes[i].Layer == out.Nodes[j].Layer {
			return out.Nodes[i].Label < out.Nodes[j].Label
		}
		return out.Nodes[i].Layer < out.Nodes[j].Layer
	})
	sort.Slice(out.Edges, func(i, j int) bool {
		left := out.Edges[i].From + "|" + out.Edges[i].To
		right := out.Edges[j].From + "|" + out.Edges[j].To
		return left < right
	})
	return out
}

func filterRoles(roles []state.RoleRecord, opts Options) []state.RoleRecord {
	out := make([]state.RoleRecord, 0, len(roles))
	for _, role := range roles {
		if opts.Env != "all" && role.Env != opts.Env {
			continue
		}
		if !matchAny(role.AccountName+" "+role.AccountID, opts.Account) {
			continue
		}
		if !matchAny(role.RoleName, opts.Role) {
			continue
		}
		out = append(out, role)
	}
	return out
}

func filterClusters(clusters []state.ClusterRecord, opts Options) []state.ClusterRecord {
	out := make([]state.ClusterRecord, 0, len(clusters))
	for _, cluster := range clusters {
		if opts.Env != "all" && cluster.Env != opts.Env {
			continue
		}
		if !matchAny(cluster.AccountName+" "+cluster.AccountID, opts.Account) {
			continue
		}
		if !matchAny(cluster.RoleName, opts.Role) {
			continue
		}
		if !matchAny(cluster.Region, opts.Region) {
			continue
		}
		if !matchAny(cluster.ClusterName, opts.Cluster) {
			continue
		}
		out = append(out, cluster)
	}
	return out
}

func matchAny(value, filter string) bool {
	if strings.TrimSpace(filter) == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value), strings.ToLower(strings.TrimSpace(filter)))
}

func normalizeNamespaces(cluster state.ClusterRecord) []string {
	set := map[string]struct{}{}
	for _, ns := range cluster.Namespaces {
		ns = strings.TrimSpace(ns)
		if ns != "" {
			set[ns] = struct{}{}
		}
	}
	if cluster.Namespace != "" {
		set[strings.TrimSpace(cluster.Namespace)] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for ns := range set {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	return string(buf[i:])
}
