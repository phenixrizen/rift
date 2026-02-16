package naming

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/phenixrizen/rift/internal/config"
	"github.com/phenixrizen/rift/internal/discovery"
	"github.com/phenixrizen/rift/internal/state"
)

var nonSlugRegex = regexp.MustCompile(`[^a-z0-9]+`)

func Slug(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	s = nonSlugRegex.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "unknown"
	}
	return s
}

func InferEnv(parts ...string) string {
	combined := strings.ToLower(strings.Join(parts, " "))
	switch {
	case strings.Contains(combined, "prod"):
		return "prod"
	case strings.Contains(combined, "staging"), strings.Contains(combined, "stage"):
		return "staging"
	case strings.Contains(combined, "development"), strings.Contains(combined, "dev"):
		return "dev"
	case strings.Contains(combined, "integration"), strings.Contains(combined, "int"):
		return "int"
	default:
		return "other"
	}
}

type uniqueNamer struct {
	counts map[string]int
}

func newUniqueNamer() *uniqueNamer {
	return &uniqueNamer{counts: map[string]int{}}
}

func (u *uniqueNamer) next(base string) string {
	base = Slug(base)
	u.counts[base]++
	if u.counts[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, u.counts[base])
}

func BuildState(cfg config.Config, inv discovery.Inventory) state.State {
	profileNamer := newUniqueNamer()
	contextNamer := newUniqueNamer()

	roleKeyToProfile := map[string]string{}
	roles := make([]state.RoleRecord, 0, len(inv.Roles))

	sort.Slice(inv.Roles, func(i, j int) bool {
		left := strings.Join([]string{inv.Roles[i].AccountName, inv.Roles[i].AccountID, inv.Roles[i].RoleName}, "|")
		right := strings.Join([]string{inv.Roles[j].AccountName, inv.Roles[j].AccountID, inv.Roles[j].RoleName}, "|")
		return left < right
	})

	for _, role := range inv.Roles {
		env := InferEnv(role.AccountName, role.RoleName)
		accountSlug := Slug(role.AccountName)
		if accountSlug == "unknown" {
			accountSlug = Slug(role.AccountID)
		}
		roleSlug := Slug(role.RoleName)
		base := fmt.Sprintf("rift-%s-%s-%s", env, accountSlug, roleSlug)
		profile := profileNamer.next(base)
		key := role.AccountID + "|" + role.RoleName
		roleKeyToProfile[key] = profile
		roles = append(roles, state.RoleRecord{
			Env:         env,
			AccountID:   role.AccountID,
			AccountName: role.AccountName,
			RoleName:    role.RoleName,
			RoleSlug:    roleSlug,
			AWSProfile:  profile,
		})
	}

	sort.Slice(inv.Clusters, func(i, j int) bool {
		left := strings.Join([]string{inv.Clusters[i].AccountName, inv.Clusters[i].RoleName, inv.Clusters[i].Region, inv.Clusters[i].ClusterName}, "|")
		right := strings.Join([]string{inv.Clusters[j].AccountName, inv.Clusters[j].RoleName, inv.Clusters[j].Region, inv.Clusters[j].ClusterName}, "|")
		return left < right
	})

	clusters := make([]state.ClusterRecord, 0, len(inv.Clusters))
	for _, cluster := range inv.Clusters {
		env := InferEnv(cluster.AccountName, cluster.RoleName, cluster.ClusterName)
		accountSlug := Slug(cluster.AccountName)
		if accountSlug == "unknown" {
			accountSlug = Slug(cluster.AccountID)
		}
		clusterSlug := Slug(cluster.ClusterName)
		contextBase := fmt.Sprintf("rift-%s-%s-%s", env, accountSlug, clusterSlug)
		context := contextNamer.next(contextBase)
		key := cluster.AccountID + "|" + cluster.RoleName
		profile := roleKeyToProfile[key]
		if profile == "" {
			roleSlug := Slug(cluster.RoleName)
			profile = profileNamer.next(fmt.Sprintf("rift-%s-%s-%s", env, accountSlug, roleSlug))
			roleKeyToProfile[key] = profile
			roles = append(roles, state.RoleRecord{
				Env:         env,
				AccountID:   cluster.AccountID,
				AccountName: cluster.AccountName,
				RoleName:    cluster.RoleName,
				RoleSlug:    roleSlug,
				AWSProfile:  profile,
			})
		}
		namespace := cfg.NamespaceForEnv(env)
		namespaces := []string{}
		if namespace != "" {
			namespaces = append(namespaces, namespace)
		}
		clusters = append(clusters, state.ClusterRecord{
			Env:                      env,
			AccountID:                cluster.AccountID,
			AccountName:              cluster.AccountName,
			RoleName:                 cluster.RoleName,
			AWSProfile:               profile,
			Region:                   cluster.Region,
			ClusterName:              cluster.ClusterName,
			ClusterARN:               cluster.ClusterARN,
			ClusterEndpoint:          cluster.ClusterEndpoint,
			ClusterCertificateBase64: cluster.ClusterCertificateBase64,
			KubeContext:              context,
			Namespace:                namespace,
			Namespaces:               namespaces,
		})
	}

	st := state.State{
		GeneratedAt: inv.GeneratedAt,
		Regions:     append([]string(nil), cfg.Regions...),
		Roles:       dedupeRoles(roles),
		Clusters:    clusters,
	}
	st.Normalize()
	return st
}

func dedupeRoles(roles []state.RoleRecord) []state.RoleRecord {
	seen := map[string]struct{}{}
	out := make([]state.RoleRecord, 0, len(roles))
	for _, role := range roles {
		k := role.AccountID + "|" + role.RoleName + "|" + role.AWSProfile
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, role)
	}
	return out
}
