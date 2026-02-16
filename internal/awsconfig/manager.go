package awsconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/phenixrizen/rift/internal/config"
	"github.com/phenixrizen/rift/internal/state"
	"gopkg.in/ini.v1"
)

type SyncResult struct {
	Added   int
	Updated int
	Removed int
}

const (
	riftProfilePrefix = "profile rift-"
	ssoSessionSection = "sso-session rift"
	legacyAuthProfile = "profile rift-auth"
)

func EnsureSession(path string, cfg config.Config, dryRun bool) (bool, error) {
	file, err := loadINI(path)
	if err != nil {
		return false, err
	}
	changed := ensureSSOSession(file, cfg)
	if !changed || dryRun {
		return changed, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := file.SaveTo(path); err != nil {
		return false, err
	}
	return true, nil
}

func EnsureLegacyAuthProfile(path string, cfg config.Config, dryRun bool) (bool, error) {
	file, err := loadINI(path)
	if err != nil {
		return false, err
	}
	sec, err := file.GetSection(legacyAuthProfile)
	changed := false
	if err != nil {
		sec, err = file.NewSection(legacyAuthProfile)
		if err != nil {
			return false, err
		}
		changed = true
	}
	changed = setKey(sec, "sso_start_url", cfg.SSOStartURL) || changed
	changed = setKey(sec, "sso_region", cfg.SSORegion) || changed
	changed = setKey(sec, "output", "json") || changed
	if !changed || dryRun {
		return changed, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := file.SaveTo(path); err != nil {
		return false, err
	}
	return true, nil
}

func Sync(path string, cfg config.Config, st state.State, dryRun bool) (SyncResult, error) {
	file, err := loadINI(path)
	if err != nil {
		return SyncResult{}, err
	}
	result := SyncResult{}

	if changed := ensureSSOSession(file, cfg); changed {
		result.Updated++
	}

	desired := map[string]state.RoleRecord{}
	for _, role := range st.Roles {
		desired[role.AWSProfile] = role
	}

	existingRift := make([]string, 0)
	for _, section := range file.Sections() {
		name := section.Name()
		if strings.HasPrefix(name, riftProfilePrefix) {
			existingRift = append(existingRift, strings.TrimPrefix(name, "profile "))
		}
	}

	for _, profile := range existingRift {
		if _, ok := desired[profile]; !ok {
			file.DeleteSection("profile " + profile)
			result.Removed++
		}
	}

	sorted := make([]string, 0, len(desired))
	for profile := range desired {
		sorted = append(sorted, profile)
	}
	sort.Strings(sorted)

	defaultRegion := ""
	if len(cfg.Regions) > 0 {
		defaultRegion = cfg.Regions[0]
	}

	for _, profile := range sorted {
		role := desired[profile]
		secName := "profile " + profile
		created := false
		sec, err := file.GetSection(secName)
		if err != nil {
			sec, err = file.NewSection(secName)
			if err != nil {
				return result, fmt.Errorf("create section %q: %w", secName, err)
			}
			created = true
			result.Added++
		}
		changed := false
		changed = setKey(sec, "sso_session", "rift") || changed
		changed = setKey(sec, "sso_account_id", role.AccountID) || changed
		changed = setKey(sec, "sso_role_name", role.RoleName) || changed
		if defaultRegion != "" {
			changed = setKey(sec, "region", defaultRegion) || changed
		}
		changed = setKey(sec, "output", "json") || changed
		if changed && !created {
			result.Updated++
		}
	}

	if dryRun {
		return result, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return result, err
	}
	if err := file.SaveTo(path); err != nil {
		return result, err
	}
	return result, nil
}

func ensureSSOSession(file *ini.File, cfg config.Config) bool {
	sec, err := file.GetSection(ssoSessionSection)
	if err != nil {
		sec, _ = file.NewSection(ssoSessionSection)
	}
	changed := false
	changed = setKey(sec, "sso_start_url", cfg.SSOStartURL) || changed
	changed = setKey(sec, "sso_region", cfg.SSORegion) || changed
	changed = setKey(sec, "sso_registration_scopes", "sso:account:access") || changed
	return changed
}

func loadINI(path string) (*ini.File, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return ini.Empty(), nil
		}
		return nil, err
	}
	return ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true}, path)
}

func setKey(section *ini.Section, key, value string) bool {
	existing := section.Key(key).String()
	if existing == value {
		return false
	}
	section.Key(key).SetValue(value)
	return true
}
