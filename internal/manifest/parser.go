package manifest

import (
	"bufio"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load reads and parses a goship.yml manifest from the given path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Load secrets from file if specified.
	if m.SecretsFile != "" {
		baseDir := filepath.Dir(path)
		secretsPath := m.SecretsFile
		if !filepath.IsAbs(secretsPath) {
			secretsPath = filepath.Join(baseDir, secretsPath)
		}

		fileSecrets, err := loadSecretsFile(secretsPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load secrets_file %q: %w", m.SecretsFile, err)
		}

		// File secrets are base; inline secrets override.
		if m.Secrets == nil {
			m.Secrets = fileSecrets
		} else {
			merged := make(map[string]string, len(fileSecrets)+len(m.Secrets))
			maps.Copy(merged, fileSecrets)
			maps.Copy(merged, m.Secrets)
			m.Secrets = merged
		}
	}

	if err := m.Validate(); err != nil {
		return nil, err
	}

	return &m, nil
}

// Validate checks required fields in the manifest.
func (m *Manifest) Validate() error {
	if m.Version == "" {
		return errors.New("manifest: version is required")
	}
	if m.Project == "" {
		return errors.New("manifest: project name is required")
	}
	return nil
}

// Resolve applies an environment overlay and returns a new resolved manifest.
// The original manifest is not modified.
//
//nolint:funlen // Environment overlay logic is sequential and clear
func (m *Manifest) Resolve(envName string) (*Manifest, error) {
	if envName == "" {
		return m, nil
	}

	env, ok := m.Environments[envName]
	if !ok {
		return nil, fmt.Errorf("manifest: environment %q not found", envName)
	}

	// Deep copy the base manifest.
	resolved := *m

	// Resources: field-level override.
	if env.Resources != nil {
		if resolved.Resources == nil {
			resolved.Resources = &Resources{}
		}
		base := *resolved.Resources
		if env.Resources.CPU > 0 {
			base.CPU = env.Resources.CPU
		}
		if env.Resources.Memory != "" {
			base.Memory = env.Resources.Memory
		}
		if env.Resources.Disk != "" {
			base.Disk = env.Resources.Disk
		}
		resolved.Resources = &base
	}

	// Domains: full replace.
	if len(env.Domains) > 0 {
		resolved.Domains = make([]string, len(env.Domains))
		copy(resolved.Domains, env.Domains)
		// Reset default domain to first of the new list.
		if len(resolved.Domains) > 0 {
			resolved.DefaultDomain = resolved.Domains[0]
		}
	}

	// Compose: field-level override.
	if env.Compose != nil {
		if resolved.Compose == nil {
			resolved.Compose = &Compose{}
		}
		base := *resolved.Compose
		if env.Compose.File != "" {
			base.File = env.Compose.File
		}
		if env.Compose.Build != nil {
			base.Build = env.Compose.Build
		}
		resolved.Compose = &base
	}

	// Env: map merge (environment on top of base).
	if len(env.Env) > 0 {
		merged := make(map[string]string, len(resolved.Env)+len(env.Env))
		maps.Copy(merged, resolved.Env)
		maps.Copy(merged, env.Env)
		resolved.Env = merged
	}

	// Secrets: map merge (environment on top of base).
	if len(env.Secrets) > 0 {
		merged := make(map[string]string, len(resolved.Secrets)+len(env.Secrets))
		maps.Copy(merged, resolved.Secrets)
		maps.Copy(merged, env.Secrets)
		resolved.Secrets = merged
	}

	// Clear environments from resolved manifest (no longer needed).
	resolved.Environments = nil

	return &resolved, nil
}

// loadSecretsFile reads a KEY=VALUE file and returns the parsed map.
// Lines starting with # and empty lines are skipped.
func loadSecretsFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // best-effort close on read-only file

	secrets := make(map[string]string)
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		secrets[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return secrets, nil
}
