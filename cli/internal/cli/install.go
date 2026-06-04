package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/spf13/cobra"
)

// installPollInterval is how often the install command polls operator readiness
// after the apply returns not-ready.
const installPollInterval = 3 * time.Second

// newInstallCommand wires the top-level `yacd install` command. Unlike the
// devnet verbs, install targets an arbitrary cluster: it honors explicit
// --kubeconfig/--context (or the YACD_* env vars, already folded into the
// runtime config) and otherwise uses the ambient current-context. It does not
// consult the managed-devnet record. The root -n/--namespace flag selects the
// install namespace; empty defaults to operator.DefaultNamespace.
func newInstallCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install or upgrade the YACD operator on a cluster",
		Long: "Install or upgrade the YACD operator onto the targeted cluster.\n\n" +
			"The target is the explicit --kubeconfig/--context (or the YACD_KUBECONFIG/\n" +
			"YACD_KUBE_CONTEXT environment variables), falling back to the ambient\n" +
			"kubeconfig current-context. The -n/--namespace flag selects the install\n" +
			"namespace and defaults to \"" + operator.DefaultNamespace + "\".\n\n" +
			"install reconciles the cluster to the operator version this CLI embeds: it\n" +
			"installs when absent, upgrades an older same-major install, re-applies an\n" +
			"equal version to heal drift, and refuses a newer or major-mismatched\n" +
			"in-cluster version with actionable guidance. Use --dry-run to preview the\n" +
			"action without changing the cluster.\n\n" +
			"The default operator image is digest-pinned to the version this CLI\n" +
			"embeds; the supported way to change the operator version is to upgrade the\n" +
			"CLI. The -f/--set/--set-string flags customize OPERATIONAL chart values\n" +
			"(replicas, resources, scheduling, logging, metrics, and so on). -f reads\n" +
			"YAML values files (repeatable, later files win); --set and --set-string\n" +
			"take inline key=value overrides in Helm syntax (--set-string forces string\n" +
			"values). Precedence, later wins: -f files (in order) < --set < --set-string\n" +
			"(this is a fixed precedence and intentionally does not interleave --set and\n" +
			"--set-string by argument order the way Helm does). These override values are\n" +
			"validated against the chart's schema, so a bad value fails fast (under\n" +
			"--dry-run too). Because user values deep-merge over the defaults, a --set\n" +
			"image.tag is ineffective (the chart renders repository@digest and the pinned\n" +
			"digest wins), but a --set image.digest or image.repository WILL repoint the\n" +
			"operator image and is not a supported configuration.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeConfig, err := loadRuntimeConfig(commandContext.viper)
			if err != nil {
				return err
			}

			waitReady := commandContext.viper.GetBool("wait")
			timeout := commandContext.viper.GetDuration("timeout")
			dryRun := commandContext.viper.GetBool("dry-run")

			// Read the value-override flags straight off the command rather than
			// through viper: viper's StringArray binding mangles the Helm --set comma
			// syntax (each "a=1,b=2" line must reach strvals intact).
			valueFiles, err := cmd.Flags().GetStringArray("values")
			if err != nil {
				return err
			}
			setValues, err := cmd.Flags().GetStringArray("set")
			if err != nil {
				return err
			}
			setStringValues, err := cmd.Flags().GetStringArray("set-string")
			if err != nil {
				return err
			}

			userOverrides, err := buildUserOverrides(valueFiles, setValues, setStringValues)
			if err != nil {
				return err
			}

			installer, err := commandContext.operatorInstaller(runtimeConfig.Kubeconfig, runtimeConfig.KubeContext)
			if err != nil {
				return err
			}

			// Model A: the pinned typed Image/FaucetImage stay, and the user
			// overrides ride in Extra, which ToHelmValues deep-merges on top. The
			// embedded digest shadows a --set image.tag (the chart renders
			// repository@digest), so the default install stays digest-pinned for
			// operational knobs; image.digest/image.repository overrides do still win
			// through the merge and are an unsupported configuration, not blocked here.
			vals := operator.Default()
			vals.Extra = userOverrides

			spec := operator.InstallSpec{
				Namespace: runtimeConfig.Namespace,
				Values:    vals,
			}
			resolvedNamespace := runtimeConfig.Namespace
			if resolvedNamespace == "" {
				resolvedNamespace = operator.DefaultNamespace
			}

			report := &stepReporter{w: commandContext.err}

			// Dry-run computes a read-only plan and never waits, so it is gated by
			// neither --wait nor a positive --timeout. The timeout still bounds the
			// plan's reads when set.
			if dryRun {
				return runInstallPlan(cmd.Context(), commandContext, installer, spec, resolvedNamespace, timeout)
			}

			if waitReady && timeout <= 0 {
				return fmt.Errorf("--timeout must be greater than 0 when --wait is set")
			}

			return runInstall(cmd.Context(), commandContext, report, installer, spec, resolvedNamespace, waitReady, timeout)
		},
	}

	cmd.Flags().Bool("wait", true, "Wait for the operator to become ready")
	cmd.Flags().Duration("timeout", 5*time.Minute, "Maximum time to wait for the operator to become ready")
	cmd.Flags().Bool("dry-run", false, "Report the planned action without changing the cluster")
	cmd.Flags().StringArrayP("values", "f", nil, "Path to a YAML file with operational chart value overrides (repeatable; later files win)")
	cmd.Flags().StringArray("set", nil, "Set an operational chart value (Helm --set syntax, repeatable)")
	cmd.Flags().StringArray("set-string", nil, "Set an operational chart value forced to a string (Helm --set-string syntax, repeatable)")

	return cmd
}

// runInstallPlan handles `yacd install --dry-run`: it computes the action the
// next install would take and prints it, mutating nothing. The plan's reads are
// bounded by timeout when set, so a stuck API server cannot stall the preview
// past the advertised maximum. A would-refuse plan returns the typed error with
// actionable guidance attached so main surfaces it once on stderr and the
// command exits nonzero, matching the real-install refuse path.
func runInstallPlan(ctx context.Context, commandContext *commandContext, installer operator.Installer, spec operator.InstallSpec, namespace string, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	decision, err := installer.Plan(ctx, spec)
	if err != nil {
		if guidance, ok := refuseGuidance(err); ok {
			return fmt.Errorf("refuse to install in namespace %s: %w; %s", namespace, err, guidance)
		}
		return err
	}

	installed := decision.InstalledVersion
	if installed == "" {
		installed = "none"
	}
	_, err = fmt.Fprintf(commandContext.out, "Plan: %s operator (installed %s -> %s) in namespace %s\n",
		actionVerb(decision.Action), installed, decision.TargetVersion, namespace)
	return err
}

// runInstall handles a real `yacd install`: it applies the operator, maps a
// refusal to actionable guidance, and — when --wait is set — waits for the
// manager Deployment to become Available before reporting success. The whole
// operation (the apply and, when set, the readiness poll) is bounded by timeout
// when positive, mirroring lifecycle.Manager, so a stuck API server cannot
// stall the apply past the advertised maximum even with --wait=false.
func runInstall(ctx context.Context, commandContext *commandContext, report *stepReporter, installer operator.Installer, spec operator.InstallSpec, namespace string, waitReady bool, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	report.Step("Installing operator in namespace %s", namespace)
	state, err := installer.EnsureOperator(ctx, spec)
	if err != nil {
		if guidance, ok := refuseGuidance(err); ok {
			return fmt.Errorf("%w; %s", err, guidance)
		}
		return err
	}

	if waitReady && !state.Ready {
		report.Substep("Waiting for the operator to become ready")
		state, err = operator.WaitForReady(ctx, installer, namespace, installPollInterval)
		if err != nil {
			return fmt.Errorf("operator did not become ready: %w", err)
		}
	}

	if waitReady {
		_, err = fmt.Fprintf(commandContext.out, "Operator %s ready in namespace %s\n", state.Version, namespace)
		return err
	}

	_, err = fmt.Fprintf(commandContext.out, "Operator %s installed in namespace %s (not waiting for readiness)\n", state.Version, namespace)
	return err
}

// actionVerb renders an install Action as a present-tense verb for plan output.
func actionVerb(action operator.Action) string {
	switch action {
	case operator.ActionInstall:
		return "install"
	case operator.ActionUpgrade:
		return "upgrade"
	case operator.ActionNoop:
		return "re-apply"
	case operator.ActionRefuse:
		return "refuse"
	default:
		return "apply"
	}
}

// refuseGuidance maps a Decide refusal error to a short actionable message. It
// returns false for any other error so the caller surfaces it unchanged.
func refuseGuidance(err error) (string, bool) {
	switch {
	case errors.Is(err, operator.ErrNewerOperator):
		return "upgrade the CLI to a version at least as new as the installed operator", true
	case errors.Is(err, operator.ErrMajorMismatch):
		return "a manual migration is required to cross a major version boundary", true
	case errors.Is(err, operator.ErrUnknownInstalledVersion):
		return "the installed operator has no readable version label; resolve it before reinstalling", true
	default:
		return "", false
	}
}
