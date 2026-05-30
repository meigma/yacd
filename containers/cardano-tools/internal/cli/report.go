package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/meigma/yacd/containers/cardano-tools/internal/artifactset"
	"github.com/meigma/yacd/containers/cardano-tools/internal/config"
	"github.com/meigma/yacd/containers/cardano-tools/internal/kube"
	"github.com/meigma/yacd/internal/controller/annotations"
)

// newReportCommand builds the "report" subcommand, which reads an artifact
// directory produced by generate (or fetch) and publishes its manifest into
// the network artifact ConfigMap the YACD controllers consume.
func newReportCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Publish an artifact directory's manifest to the network artifact ConfigMap",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.LoadReport(commandContext.viper)
			if err != nil {
				return err
			}
			return runReport(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}

	flags := cmd.Flags()
	flags.String("artifact-configmap-name", "", "Name of the network artifact ConfigMap to patch")
	flags.String("artifact-configmap-namespace", "", "Namespace of the network artifact ConfigMap (defaults to the projected namespace file)")
	flags.String("artifact-token-path", config.DefaultServiceTokenPath, "Path to the projected ServiceAccount token used to patch the ConfigMap")
	flags.String("artifact-ca-path", config.DefaultServiceCAPath, "Path to the projected Kubernetes API CA bundle")
	flags.String("artifact-namespace-path", config.DefaultNamespacePath, "Path to the projected namespace file used to resolve the ConfigMap namespace")
	flags.String("kubernetes-api-url", "", "Kubernetes API base URL (defaults to pod-injected service host/port)")
	flags.String("artifact-dir", "", "Directory containing the artifact files to publish")
	flags.String("plan-manifest-file", "", "Absolute path to the localnet plan manifest (defaults to <artifact-dir>/yacd-localnet-plan.json)")
	flags.String("cardano-network-name", "", "Name of the owning CardanoNetwork resource")
	flags.String("cardano-network-namespace", "", "Namespace of the owning CardanoNetwork resource (defaults to the artifact namespace)")
	flags.String("cardano-network-mode", "", "Network mode (e.g. local, public)")
	flags.String("cardano-network-era", "", "Cardano era for the published connection metadata")
	flags.String("cardano-node-to-node-host", "", "Primary node-to-node Service hostname")
	flags.Int("cardano-node-to-node-port", 0, "Primary node-to-node Service port")
	flags.String("cardano-node-to-node-url", "", "Pre-built primary node-to-node URL (defaults to tcp://<host>:<port>)")
	flags.Bool("dry-run", false, "Print the merge-patch JSON to stdout and skip the Kubernetes API call")

	return cmd
}

// runReport reads the artifacts and manifest from disk, assembles the artifact
// set, and applies the resulting patch via the kube adapter. On success it
// writes a one-line confirmation containing the published data hash to out.
func runReport(ctx context.Context, cfg config.ReportConfig, out io.Writer) error {
	manifest, err := artifactset.ReadManifest(cfg.ArtifactDir, cfg.PlanManifestFile)
	if err != nil {
		return err
	}

	artifactData, err := artifactset.ReadArtifacts(cfg.ArtifactDir)
	if err != nil {
		return err
	}

	set, err := artifactset.Build(artifactset.Input{
		Network: artifactset.NetworkIdentity{
			Name:           cfg.CardanoNetworkName,
			Namespace:      cfg.CardanoNetworkNamespace,
			Mode:           cfg.CardanoNetworkMode,
			Era:            cfg.CardanoNetworkEra,
			NodeToNodeHost: cfg.CardanoNodeToNodeHost,
			NodeToNodePort: cfg.CardanoNodeToNodePort,
			NodeToNodeURL:  cfg.CardanoNodeToNodeURL,
		},
		Manifest:  manifest,
		Artifacts: artifactData,
	})
	if err != nil {
		return err
	}

	patch := toConfigMapPatch(set)

	if cfg.DryRun {
		body, err := kube.MarshalMergePatchIndented(patch)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(body))
		return err
	}

	kubeClient, err := kube.NewClient(kube.Config{
		APIURL:    cfg.KubernetesAPIURL,
		TokenPath: cfg.ArtifactTokenPath,
		CAPath:    cfg.ArtifactCAPath,
	})
	if err != nil {
		return err
	}

	if err := kubeClient.PatchConfigMap(ctx, cfg.ArtifactConfigMapNamespace, cfg.ArtifactConfigMapName, patch); err != nil {
		return err
	}

	_, err = fmt.Fprintf(out, "published Cardano network artifacts to ConfigMap %s/%s with data hash %s\n",
		cfg.ArtifactConfigMapNamespace, cfg.ArtifactConfigMapName, set.Annotations.DataHash)
	return err
}

// toConfigMapPatch projects an [artifactset.Set] onto the [kube.ConfigMapPatch]
// shape: present data becomes SetData, owned keys absent from data become
// PruneData, and the typed annotations become a flat map keyed by the shared
// controller annotation constants.
func toConfigMapPatch(set artifactset.Set) kube.ConfigMapPatch {
	prune := make([]string, 0)
	for _, key := range set.KnownKeys {
		if _, present := set.Data[key]; present {
			continue
		}
		prune = append(prune, key)
	}
	return kube.ConfigMapPatch{
		SetData:   set.Data,
		PruneData: prune,
		Annotations: map[string]string{
			annotations.ArtifactSchemaVersion: set.Annotations.SchemaVersion,
			annotations.LocalnetFingerprint:   set.Annotations.LocalnetFingerprint,
			annotations.ArtifactDataHash:      set.Annotations.DataHash,
		},
	}
}
