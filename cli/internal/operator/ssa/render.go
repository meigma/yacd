package ssa

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const (
	// chartDir is the embed root holding the in-package chart copy.
	chartDir = "chart"

	// releaseName is the Helm release name the operator is rendered under. It
	// drives the chart's resource name prefixes (yacd-*), so it is fixed rather
	// than configurable.
	releaseName = "yacd"

	// hookAnnotation marks Helm hook objects, which the install filters out
	// (mirroring `helm template --no-hooks`).
	hookAnnotation = "helm.sh/hook"
)

// render loads the embedded chart, renders its templates with the resolved
// namespace and supplied Helm values, and returns the ordered object set the
// apply pipeline consumes. It is clientless: the chart uses no `lookup`, so no
// rest.Config is needed to render. CRDs come from chart.CRDObjects() (Helm
// renders crds/ separately), not engine.Render, reproducing `--include-crds`.
// Helm hook objects and empty documents are skipped.
func render(chartFS fs.FS, namespace string, values map[string]any) ([]*unstructured.Unstructured, error) {
	files, err := bufferedFiles(chartFS)
	if err != nil {
		return nil, err
	}

	ch, err := loader.LoadFiles(files)
	if err != nil {
		return nil, fmt.Errorf("load embedded chart: %w", err)
	}

	renderVals, err := chartutil.ToRenderValues(ch, values,
		chartutil.ReleaseOptions{Name: releaseName, Namespace: namespace}, nil)
	if err != nil {
		return nil, fmt.Errorf("coalesce chart values: %w", err)
	}

	rendered, err := engine.Render(ch, renderVals)
	if err != nil {
		return nil, fmt.Errorf("render chart: %w", err)
	}

	objs, err := parseRendered(rendered)
	if err != nil {
		return nil, err
	}

	crds, err := parseCRDs(ch.CRDObjects())
	if err != nil {
		return nil, err
	}

	return append(objs, crds...), nil
}

// bufferedFiles walks the embedded chart filesystem into the chart-relative
// BufferedFiles loader.LoadFiles expects (names are relative to the chart root,
// so the chartDir prefix is stripped).
func bufferedFiles(chartFS fs.FS) ([]*loader.BufferedFile, error) {
	var files []*loader.BufferedFile
	err := fs.WalkDir(chartFS, chartDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(chartFS, p)
		if err != nil {
			return fmt.Errorf("read embedded chart file %s: %w", p, err)
		}
		rel := strings.TrimPrefix(p, chartDir+"/")
		// loader expects forward-slash, chart-relative paths.
		files = append(files, &loader.BufferedFile{Name: path.Clean(rel), Data: data})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk embedded chart: %w", err)
	}
	if len(files) == 0 {
		return nil, errors.New("embedded chart is empty")
	}
	return files, nil
}

// parseRendered turns the per-template multi-document YAML engine.Render emits
// into unstructured objects, skipping empty documents (the chart's
// validate-values template renders to nothing) and Helm hook objects.
func parseRendered(rendered map[string]string) ([]*unstructured.Unstructured, error) {
	var objs []*unstructured.Unstructured
	for name, content := range rendered {
		parsed, err := decodeObjects(strings.NewReader(content))
		if err != nil {
			return nil, fmt.Errorf("decode rendered template %s: %w", name, err)
		}
		for _, obj := range parsed {
			if isHook(obj) {
				continue
			}
			objs = append(objs, obj)
		}
	}
	return objs, nil
}

// parseCRDs turns the chart's CRD files into unstructured objects.
func parseCRDs(crds []chart.CRD) ([]*unstructured.Unstructured, error) {
	var objs []*unstructured.Unstructured
	for _, crd := range crds {
		parsed, err := decodeObjects(strings.NewReader(string(crd.File.Data)))
		if err != nil {
			return nil, fmt.Errorf("decode CRD %s: %w", crd.Name, err)
		}
		objs = append(objs, parsed...)
	}
	return objs, nil
}

// decodeObjects splits a multi-document YAML stream into unstructured objects,
// skipping empty documents.
func decodeObjects(r io.Reader) ([]*unstructured.Unstructured, error) {
	decoder := yaml.NewYAMLOrJSONDecoder(r, 4096)
	var objs []*unstructured.Unstructured
	for {
		raw := map[string]any{}
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if len(raw) == 0 {
			continue
		}
		objs = append(objs, &unstructured.Unstructured{Object: raw})
	}
	return objs, nil
}

// isHook reports whether an object carries a Helm hook annotation, so the
// install can drop it (mirroring `helm template --no-hooks`).
func isHook(obj *unstructured.Unstructured) bool {
	_, ok := obj.GetAnnotations()[hookAnnotation]
	return ok
}
