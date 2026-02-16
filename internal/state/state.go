package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type RoleRecord struct {
	Env         string `json:"env"`
	AccountID   string `json:"account_id"`
	AccountName string `json:"account_name"`
	RoleName    string `json:"role_name"`
	RoleSlug    string `json:"role_slug"`
	AWSProfile  string `json:"aws_profile"`
}

type ClusterRecord struct {
	Env                      string   `json:"env"`
	AccountID                string   `json:"account_id"`
	AccountName              string   `json:"account_name"`
	RoleName                 string   `json:"role_name"`
	AWSProfile               string   `json:"aws_profile"`
	Region                   string   `json:"region"`
	ClusterName              string   `json:"cluster_name"`
	ClusterARN               string   `json:"cluster_arn"`
	ClusterEndpoint          string   `json:"cluster_endpoint"`
	ClusterCertificateBase64 string   `json:"cluster_certificate_base64"`
	KubeContext              string   `json:"kube_context"`
	Namespace                string   `json:"namespace"`
	Namespaces               []string `json:"namespaces,omitempty"`
}

type State struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Regions     []string        `json:"regions"`
	Roles       []RoleRecord    `json:"roles"`
	Clusters    []ClusterRecord `json:"clusters"`
}

func (s *State) Normalize() {
	sort.Slice(s.Roles, func(i, j int) bool {
		left := strings.Join([]string{s.Roles[i].Env, s.Roles[i].AccountName, s.Roles[i].RoleName}, "|")
		right := strings.Join([]string{s.Roles[j].Env, s.Roles[j].AccountName, s.Roles[j].RoleName}, "|")
		return left < right
	})
	sort.Slice(s.Clusters, func(i, j int) bool {
		left := strings.Join([]string{s.Clusters[i].Env, s.Clusters[i].AccountName, s.Clusters[i].RoleName, s.Clusters[i].Region, s.Clusters[i].ClusterName}, "|")
		right := strings.Join([]string{s.Clusters[j].Env, s.Clusters[j].AccountName, s.Clusters[j].RoleName, s.Clusters[j].Region, s.Clusters[j].ClusterName}, "|")
		return left < right
	})
}

func Load(path string) (State, error) {
	var s State
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parse state: %w", err)
	}
	s.Normalize()
	return s, nil
}

func Save(path string, s State) error {
	s.Normalize()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
