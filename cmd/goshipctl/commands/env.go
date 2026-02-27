package commands

import (
	"errors"
	"fmt"
	"maps"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/guilhermebr/goship/internal/vault"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

const maskedValue = "********"

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage project-level environment variables",
	Long:  `Set, list, and delete project-level environment variables. Use --secret to encrypt sensitive values.`,
}

var (
	envSecretFlag bool
	envShowValues bool
)

var envSetCmd = &cobra.Command{
	Use:   "set <project> KEY=VALUE [KEY=VALUE ...]",
	Short: "Set environment variables for a project",
	Long:  `Set one or more environment variables. Use --secret to encrypt values at rest.`,
	Args:  cobra.MinimumNArgs(2),
	RunE:  runEnvSet,
}

var envListCmd = &cobra.Command{
	Use:     "list <project>",
	Short:   "List environment variables for a project",
	Aliases: []string{"ls"},
	Args:    cobra.ExactArgs(1),
	RunE:    runEnvList,
}

var envDeleteCmd = &cobra.Command{
	Use:     "delete <project> KEY [KEY ...]",
	Short:   "Delete environment variables from a project",
	Aliases: []string{"rm"},
	Args:    cobra.MinimumNArgs(2),
	RunE:    runEnvDelete,
}

func init() {
	envSetCmd.Flags().BoolVar(&envSecretFlag, "secret", false, "Encrypt values at rest")
	envListCmd.Flags().BoolVar(&envShowValues, "show-values", false, "Show decrypted values (default: masked)")

	envCmd.AddCommand(envSetCmd)
	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envDeleteCmd)
}

func runEnvSet(cmd *cobra.Command, args []string) error {
	projectName := args[0]
	kvPairs := args[1:]

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	if project.Env == nil {
		project.Env = make(map[string]string)
	}

	// Parse KEY=VALUE pairs.
	parsed := parseEnv(kvPairs)
	if len(parsed) != len(kvPairs) {
		return errors.New("invalid format: all arguments must be KEY=VALUE")
	}

	// Encrypt values if --secret flag is set.
	if envSecretFlag {
		v, vErr := initVault()
		if vErr != nil {
			return vErr
		}

		for k, val := range parsed {
			encrypted, encErr := v.Encrypt(val)
			if encErr != nil {
				return fmt.Errorf("failed to encrypt %s: %w", k, encErr)
			}

			parsed[k] = encrypted
		}
	}

	maps.Copy(project.Env, parsed)

	if err := store.UpdateProject(project); err != nil {
		return fmt.Errorf("failed to save project: %w", err)
	}

	out := cmd.OutOrStdout()

	keys := make([]string, 0, len(parsed))
	for k := range parsed {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		if envSecretFlag {
			fmt.Fprintf(out, "  %s=%s  (encrypted)\n", k, maskedValue)
		} else {
			fmt.Fprintf(out, "  %s=%s\n", k, parsed[k])
		}
	}

	fmt.Fprintf(out, "Environment updated for project '%s'\n", projectName)

	return nil
}

func runEnvList(cmd *cobra.Command, args []string) error {
	projectName := args[0]

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	if len(project.Env) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No environment variables set for project '%s'.\n", projectName)
		return nil
	}

	// Sort keys for stable output.
	keys := make([]string, 0, len(project.Env))
	for k := range project.Env {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var decrypted map[string]string
	if envShowValues {
		decrypted, err = decryptProjectEnv(project.Env)
		if err != nil {
			return fmt.Errorf("failed to decrypt env: %w", err)
		}
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tVALUE\tSECRET")

	for _, k := range keys {
		rawVal := project.Env[k]
		secret := vault.IsEncrypted(rawVal)

		displayVal := rawVal
		if secret {
			displayVal = maskedValue
			if envShowValues && decrypted != nil {
				displayVal = decrypted[k]
			}
		}

		secretLabel := ""
		if secret {
			secretLabel = "yes"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\n", k, displayVal, secretLabel)
	}

	return w.Flush()
}

func runEnvDelete(cmd *cobra.Command, args []string) error {
	projectName := args[0]
	keys := args[1:]

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	if project.Env == nil {
		return fmt.Errorf("no environment variables set for project '%s'", projectName)
	}

	out := cmd.OutOrStdout()

	var deleted int
	for _, k := range keys {
		if _, ok := project.Env[k]; !ok {
			fmt.Fprintf(out, "  %s: not found (skipped)\n", k)
			continue
		}

		delete(project.Env, k)
		fmt.Fprintf(out, "  %s: deleted\n", k)
		deleted++
	}

	if deleted == 0 {
		return nil
	}

	if err := store.UpdateProject(project); err != nil {
		return fmt.Errorf("failed to save project: %w", err)
	}

	fmt.Fprintf(out, "Environment updated for project '%s'\n", projectName)

	return nil
}

// initVault creates a Vault instance using the master key from the data directory.
func initVault() (*vault.Vault, error) {
	dir := expandDataDir(cfg.DataDir)

	key, err := vault.LoadOrCreateMasterKey(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to load master key: %w", err)
	}

	return vault.New(key)
}

// mergeProjectEnv decrypts project-level env vars and merges them into app.Env.
// Project env is the base; app-level values override.
func mergeProjectEnv(projectEnv map[string]string, app *entities.AppSpec) error {
	if len(projectEnv) == 0 {
		return nil
	}

	decrypted, err := decryptProjectEnv(projectEnv)
	if err != nil {
		return fmt.Errorf("failed to decrypt project env: %w", err)
	}

	merged := make(map[string]string, len(decrypted)+len(app.Env))
	maps.Copy(merged, decrypted)
	maps.Copy(merged, app.Env)
	app.Env = merged

	return nil
}

// decryptProjectEnv returns a copy of env with encrypted values decrypted.
// Only initializes the vault if there are encrypted values.
func decryptProjectEnv(env map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(env))

	var v *vault.Vault

	for k, val := range env {
		if !vault.IsEncrypted(val) {
			result[k] = val
			continue
		}

		if v == nil {
			var err error

			v, err = initVault()
			if err != nil {
				return nil, err
			}
		}

		decrypted, err := v.Decrypt(val)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt %s: %w", k, err)
		}

		result[k] = decrypted
	}

	return result, nil
}
