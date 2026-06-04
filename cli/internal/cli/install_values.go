package cli

import (
	"fmt"
	"os"

	"helm.sh/helm/v3/pkg/strvals"
	"sigs.k8s.io/yaml"
)

// buildUserOverrides assembles the user-supplied Helm value overrides from the
// install command's -f/--set/--set-string flags into a single values tree.
//
// Precedence, later wins: the -f files are read in the order given and
// deep-merged so a later file overrides an earlier one; then --set lines are
// parsed (typed scalars) and merged on top; then --set-string lines (forced to
// strings) are merged last. Both --set forms parse into the same accumulating
// map, so they layer over the file values rather than replacing them.
//
// The returned map is the user-override layer only; it carries none of the
// CLI's pins. The caller folds it into Values.Extra, where Values.ToHelmValues
// deep-merges it over the pinned typed fields (model A: the operator image stays
// digest-pinned, so these flags customize operational knobs, not the image).
// Returns nil when no override flags are supplied.
func buildUserOverrides(valueFiles, setValues, setStringValues []string) (map[string]any, error) {
	overrides := map[string]any{}

	for _, file := range valueFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read values file %s: %w", file, err)
		}
		parsed := map[string]any{}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			return nil, fmt.Errorf("parse values file %s: %w", file, err)
		}
		mergeValueMaps(overrides, parsed)
	}

	for _, set := range setValues {
		if err := strvals.ParseInto(set, overrides); err != nil {
			return nil, fmt.Errorf("parse --set %q: %w", set, err)
		}
	}

	for _, set := range setStringValues {
		if err := strvals.ParseIntoString(set, overrides); err != nil {
			return nil, fmt.Errorf("parse --set-string %q: %w", set, err)
		}
	}

	if len(overrides) == 0 {
		return nil, nil
	}
	return overrides, nil
}

// mergeValueMaps deep-merges src into dst, with src winning on conflicts. Nested
// maps merge recursively; any non-map value replaces the destination. It matches
// the operator package's own value merge so the -f file layering here behaves
// the same way Values.Extra is folded over the pins downstream.
func mergeValueMaps(dst, src map[string]any) {
	for key, srcVal := range src {
		if srcMap, ok := srcVal.(map[string]any); ok {
			if dstMap, ok := dst[key].(map[string]any); ok {
				mergeValueMaps(dstMap, srcMap)
				continue
			}
		}
		dst[key] = srcVal
	}
}
