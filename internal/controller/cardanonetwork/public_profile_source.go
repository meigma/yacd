package cardanonetwork

import (
	"context"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/publicnet"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	cardanoNetworkCustomConfigMapSourceNameField = "spec.public.configSource.configMapRef.name"
	cardanoNetworkCustomSecretSourceNameField    = "spec.public.configSource.secretRef.name"
)

func (r *CardanoNetworkReconciler) publicCustomProfileBundle(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (*publicnet.CustomBundle, error) {
	if network.Spec.Mode != yacdv1alpha1.CardanoNetworkModePublic ||
		network.Spec.Public == nil ||
		network.Spec.Public.Profile != yacdv1alpha1.PublicNetworkProfileCustom {
		return nil, nil
	}

	source := network.Spec.Public.ConfigSource
	if source == nil {
		return nil, unsupportedSpec("public custom profile configSource is required")
	}
	switch {
	case source.ConfigMapRef != nil && source.SecretRef == nil:
		return r.publicCustomProfileConfigMapBundle(ctx, network, source.ConfigMapRef.Name)
	case source.SecretRef != nil && source.ConfigMapRef == nil:
		return r.publicCustomProfileSecretBundle(ctx, network, source.SecretRef.Name)
	default:
		return nil, unsupportedSpec("public custom profile configSource must set exactly one of configMapRef or secretRef")
	}
}

func (r *CardanoNetworkReconciler) publicCustomProfileConfigMapBundle(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	name string,
) (*publicnet.CustomBundle, error) {
	if name == "" {
		return nil, unsupportedSpec("public custom profile configMapRef.name is required")
	}

	configMap := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: network.Namespace, Name: name}
	if err := r.Get(ctx, key, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, unsupportedSpec("public custom profile ConfigMap %s is missing", key)
		}
		return nil, err
	}

	return customProfileBundleFromStringData(configMap.Data), nil
}

func (r *CardanoNetworkReconciler) publicCustomProfileSecretBundle(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	name string,
) (*publicnet.CustomBundle, error) {
	if name == "" {
		return nil, unsupportedSpec("public custom profile secretRef.name is required")
	}

	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: network.Namespace, Name: name}
	if err := r.Get(ctx, key, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, unsupportedSpec("public custom profile Secret %s is missing", key)
		}
		return nil, err
	}

	files := make(map[string]string, len(publicnet.SupportedCustomProfileKeys()))
	for _, key := range publicnet.SupportedCustomProfileKeys() {
		if raw, ok := secret.Data[key]; ok {
			files[key] = string(raw)
		}
	}
	return &publicnet.CustomBundle{Files: files}, nil
}

func customProfileBundleFromStringData(data map[string]string) *publicnet.CustomBundle {
	files := make(map[string]string, len(publicnet.SupportedCustomProfileKeys()))
	for _, key := range publicnet.SupportedCustomProfileKeys() {
		if value, ok := data[key]; ok {
			files[key] = value
		}
	}
	return &publicnet.CustomBundle{Files: files}
}

func (r *CardanoNetworkReconciler) indexCustomProfileSources(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &yacdv1alpha1.CardanoNetwork{}, cardanoNetworkCustomConfigMapSourceNameField, cardanoNetworkCustomConfigMapSourceIndexer); err != nil {
		return err
	}
	return mgr.GetFieldIndexer().IndexField(context.Background(), &yacdv1alpha1.CardanoNetwork{}, cardanoNetworkCustomSecretSourceNameField, cardanoNetworkCustomSecretSourceIndexer)
}

func cardanoNetworkCustomConfigMapSourceIndexer(object client.Object) []string {
	network, ok := object.(*yacdv1alpha1.CardanoNetwork)
	if !ok || network.Spec.Mode != yacdv1alpha1.CardanoNetworkModePublic ||
		network.Spec.Public == nil ||
		network.Spec.Public.Profile != yacdv1alpha1.PublicNetworkProfileCustom ||
		network.Spec.Public.ConfigSource == nil ||
		network.Spec.Public.ConfigSource.ConfigMapRef == nil ||
		network.Spec.Public.ConfigSource.ConfigMapRef.Name == "" {
		return nil
	}
	return []string{network.Spec.Public.ConfigSource.ConfigMapRef.Name}
}

func cardanoNetworkCustomSecretSourceIndexer(object client.Object) []string {
	network, ok := object.(*yacdv1alpha1.CardanoNetwork)
	if !ok || network.Spec.Mode != yacdv1alpha1.CardanoNetworkModePublic ||
		network.Spec.Public == nil ||
		network.Spec.Public.Profile != yacdv1alpha1.PublicNetworkProfileCustom ||
		network.Spec.Public.ConfigSource == nil ||
		network.Spec.Public.ConfigSource.SecretRef == nil ||
		network.Spec.Public.ConfigSource.SecretRef.Name == "" {
		return nil
	}
	return []string{network.Spec.Public.ConfigSource.SecretRef.Name}
}

func (r *CardanoNetworkReconciler) customProfileConfigMapEventHandler() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.cardanoNetworksForCustomProfileConfigMap)
}

func (r *CardanoNetworkReconciler) customProfileSecretEventHandler() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.cardanoNetworksForCustomProfileSecret)
}

func (r *CardanoNetworkReconciler) cardanoNetworksForCustomProfileConfigMap(ctx context.Context, object client.Object) []reconcile.Request {
	configMap, ok := object.(*corev1.ConfigMap)
	if !ok {
		return nil
	}
	return r.cardanoNetworksForCustomProfileSource(ctx, client.ObjectKeyFromObject(configMap), cardanoNetworkCustomConfigMapSourceNameField)
}

func (r *CardanoNetworkReconciler) cardanoNetworksForCustomProfileSecret(ctx context.Context, object client.Object) []reconcile.Request {
	secret, ok := object.(*corev1.Secret)
	if !ok {
		return nil
	}
	return r.cardanoNetworksForCustomProfileSource(ctx, client.ObjectKeyFromObject(secret), cardanoNetworkCustomSecretSourceNameField)
}

func (r *CardanoNetworkReconciler) cardanoNetworksForCustomProfileSource(
	ctx context.Context,
	source client.ObjectKey,
	field string,
) []reconcile.Request {
	networks := &yacdv1alpha1.CardanoNetworkList{}
	if err := r.List(ctx, networks,
		client.InNamespace(source.Namespace),
		client.MatchingFields{field: source.Name},
	); err != nil {
		logf.FromContext(ctx).Error(err, "Unable to list CardanoNetwork resources for custom profile source", "source", source, "field", field)
		return nil
	}

	requests := make([]reconcile.Request, 0, len(networks.Items))
	for _, network := range networks.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&network)})
	}
	return requests
}
