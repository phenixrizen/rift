package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	configDirName  = ".config/rift"
	configFileName = "config.yaml"
	stateFileName  = "state.json"
)

var defaultRegions = []string{"us-east-1", "us-west-2"}

type Config struct {
	SSOStartURL        string            `yaml:"sso_start_url"`
	SSORegion          string            `yaml:"sso_region"`
	Regions            []string          `yaml:"regions"`
	NamespaceDefaults  map[string]string `yaml:"namespace_defaults"`
	DiscoverNamespaces bool              `yaml:"discover_namespaces"`
}

func Default() Config {
	return Config{
		Regions:            append([]string(nil), defaultRegions...),
		NamespaceDefaults:  map[string]string{},
		DiscoverNamespaces: true,
	}
}

func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configDirName, configFileName), nil
}

func DefaultStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configDirName, stateFileName), nil
}

func ResolvePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is empty")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return filepath.Abs(path)
}

func Load(path string) (Config, error) {
	cfg := Default()
	resolved, err := ResolvePath(path)
	if err != nil {
		return cfg, err
	}
	bytes, err := os.ReadFile(resolved)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	resolved, err := ResolvePath(path)
	if err != nil {
		return err
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(resolved, data, 0o644); err != nil {
		return err
	}
	return nil
}

func (c *Config) Normalize() {
	if len(c.Regions) == 0 {
		c.Regions = append([]string(nil), defaultRegions...)
	}
	seen := map[string]struct{}{}
	regions := make([]string, 0, len(c.Regions))
	for _, region := range c.Regions {
		region = strings.TrimSpace(strings.ToLower(region))
		if region == "" {
			continue
		}
		if _, ok := seen[region]; ok {
			continue
		}
		seen[region] = struct{}{}
		regions = append(regions, region)
	}
	sort.Strings(regions)
	if len(regions) == 0 {
		regions = append([]string(nil), defaultRegions...)
	}
	c.Regions = regions

	if c.NamespaceDefaults == nil {
		c.NamespaceDefaults = map[string]string{}
	}
	normalized := make(map[string]string, len(c.NamespaceDefaults))
	for k, v := range c.NamespaceDefaults {
		key := strings.TrimSpace(strings.ToLower(k))
		if key == "" {
			continue
		}
		normalized[key] = strings.TrimSpace(v)
	}
	c.NamespaceDefaults = normalized
	c.SSOStartURL = strings.TrimSpace(c.SSOStartURL)
	c.SSORegion = strings.TrimSpace(strings.ToLower(c.SSORegion))
}

func (c Config) Validate() error {
	if c.SSOStartURL == "" {
		return errors.New("config missing sso_start_url")
	}
	if c.SSORegion == "" {
		return errors.New("config missing sso_region")
	}
	if len(c.Regions) == 0 {
		return errors.New("config missing regions")
	}
	return nil
}

func (c Config) NamespaceForEnv(env string) string {
	key := strings.ToLower(strings.TrimSpace(env))
	if key == "" {
		return ""
	}
	if value := strings.TrimSpace(c.NamespaceDefaults[key]); value != "" {
		return value
	}
	if key == "staging" {
		return strings.TrimSpace(c.NamespaceDefaults["stg"])
	}
	if key == "stg" {
		return strings.TrimSpace(c.NamespaceDefaults["staging"])
	}
	return ""
}
