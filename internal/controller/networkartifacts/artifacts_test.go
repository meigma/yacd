package networkartifacts

import (
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	cardanonetworkartifacts "github.com/meigma/yacd/internal/cardano/networkartifacts"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	ctrlartifacts "github.com/meigma/yacd/internal/ctrlkit/artifacts"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestProducerConfigMap(t *testing.T) {
	configMap := validConfigMap()

	result := ProducerConfigMap(configMap, "fingerprint")

	assert.True(t, result.Ready)
	assert.Empty(t, result.Message)
	assert.Equal(t, configMap.Name, result.Status.NetworkConfigMapName)
	assert.Equal(t, cardanonetworkartifacts.SchemaVersion, result.Status.SchemaVersion)
	assert.Equal(t, configMap.Annotations[ctrlannotations.ArtifactDataHash], result.Status.DataHash)
}

func TestProducerConfigMapValidationOrder(t *testing.T) {
	tests := []struct {
		name        string
		configMap   *corev1.ConfigMap
		fingerprint string
		message     string
	}{
		{
			name:    "missing ConfigMap",
			message: "artifact ConfigMap is missing",
		},
		{
			name: "deleting ConfigMap",
			configMap: func() *corev1.ConfigMap {
				cm := validConfigMap()
				now := metav1.Now()
				cm.DeletionTimestamp = &now
				return cm
			}(),
			fingerprint: "fingerprint",
			message:     "artifact ConfigMap is deleting",
		},
		{
			name: "schema not published",
			configMap: func() *corev1.ConfigMap {
				cm := validConfigMap()
				delete(cm.Annotations, ctrlannotations.ArtifactSchemaVersion)
				return cm
			}(),
			fingerprint: "fingerprint",
			message:     "artifact ConfigMap schema version is not published",
		},
		{
			name:        "fingerprint mismatch",
			configMap:   validConfigMap(),
			fingerprint: "other",
			message:     "artifact ConfigMap localnet fingerprint does not match the accepted localnet",
		},
		{
			name: "hash not published",
			configMap: func() *corev1.ConfigMap {
				cm := validConfigMap()
				delete(cm.Annotations, ctrlannotations.ArtifactDataHash)
				return cm
			}(),
			fingerprint: "fingerprint",
			message:     "artifact ConfigMap data hash is not published",
		},
		{
			name: "invalid data",
			configMap: func() *corev1.ConfigMap {
				cm := validConfigMap()
				delete(cm.Data, cardanonetworkartifacts.ConfigurationKey)
				cm.Annotations[ctrlannotations.ArtifactDataHash] = ctrlartifacts.ComputeDataHash(cm.Data)
				return cm
			}(),
			fingerprint: "fingerprint",
			message:     "artifact ConfigMap is missing configuration.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProducerConfigMap(tt.configMap, tt.fingerprint)

			assert.False(t, result.Ready)
			assert.Equal(t, tt.message, result.Message)
		})
	}
}

func TestProducerConfigMapNeedsRecovery(t *testing.T) {
	assert.False(t, ProducerConfigMapNeedsRecovery(nil, "fingerprint"))
	assert.False(t, ProducerConfigMapNeedsRecovery(&corev1.ConfigMap{}, "fingerprint"))

	configMap := validConfigMap()
	delete(configMap.Data, cardanonetworkartifacts.ConfigurationKey)
	assert.True(t, ProducerConfigMapNeedsRecovery(configMap, "fingerprint"))
}

func TestConsumerStatus(t *testing.T) {
	status := validConfigMapStatus()

	result := ConsumerStatus(&status)

	assert.True(t, result.Ready)
	assert.Equal(t, "network-artifacts", result.ConfigMapName)
}

func TestConsumerStatusRejectsIncompleteStatus(t *testing.T) {
	assert.Equal(t,
		"Referenced CardanoNetwork artifact status is incomplete",
		ConsumerStatus(nil).Message,
	)

	status := validConfigMapStatus()
	status.DataHash = ""
	assert.Equal(t,
		"Referenced CardanoNetwork artifact status is incomplete",
		ConsumerStatus(&status).Message,
	)
}

func TestConsumerConfigMap(t *testing.T) {
	result := ConsumerConfigMap(validConfigMap(), validConfigMapStatus())

	assert.True(t, result.Ready)
	assert.False(t, result.Pending)
	assert.Empty(t, result.Message)
}

func TestConsumerConfigMapRejectsInvalidReferences(t *testing.T) {
	tests := []struct {
		name    string
		cm      *corev1.ConfigMap
		pending bool
		message string
	}{
		{
			name:    "missing ConfigMap",
			pending: true,
			message: "Referenced CardanoNetwork artifact ConfigMap does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConsumerConfigMap(tt.cm, validConfigMapStatus())

			assert.Equal(t, tt.pending, result.Pending)
			assert.Equal(t, tt.message, result.Message)
		})
	}
}

func TestConsumerConfigMapRejectsDeletingConfigMap(t *testing.T) {
	configMap := validConfigMap()
	now := metav1.Now()
	configMap.DeletionTimestamp = &now

	result := ConsumerConfigMap(configMap, validConfigMapStatus())

	assert.False(t, result.Ready)
	assert.True(t, result.Pending)
	assert.Equal(t, "Referenced CardanoNetwork artifact ConfigMap is deleting", result.Message)
}

func TestConsumerConfigMapRejectsMetadataMismatch(t *testing.T) {
	configMap := validConfigMap()
	configMap.Annotations[ctrlannotations.ArtifactDataHash] = "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	result := ConsumerConfigMap(configMap, validConfigMapStatus())

	assert.False(t, result.Ready)
	assert.False(t, result.Pending)
	assert.Equal(t, "Referenced CardanoNetwork artifact ConfigMap metadata does not match status", result.Message)
}

func TestConsumerConfigMapRejectsInvalidData(t *testing.T) {
	configMap := validConfigMap()
	delete(configMap.Data, cardanonetworkartifacts.ConfigurationKey)
	configMap.Annotations[ctrlannotations.ArtifactDataHash] = ctrlartifacts.ComputeDataHash(configMap.Data)
	status := validConfigMapStatus()
	status.DataHash = configMap.Annotations[ctrlannotations.ArtifactDataHash]

	result := ConsumerConfigMap(configMap, status)

	assert.False(t, result.Ready)
	assert.False(t, result.Pending)
	assert.Equal(t, "Referenced CardanoNetwork artifact ConfigMap is invalid: artifact ConfigMap is missing configuration.yaml", result.Message)
}

func validConfigMap() *corev1.ConfigMap {
	data := map[string]string{
		cardanonetworkartifacts.ConfigurationKey:   "configuration",
		cardanonetworkartifacts.ByronGenesisKey:    "byron",
		cardanonetworkartifacts.ShelleyGenesisKey:  "shelley",
		cardanonetworkartifacts.AlonzoGenesisKey:   "alonzo",
		cardanonetworkartifacts.ConwayGenesisKey:   "conway",
		cardanonetworkartifacts.PrimaryTopologyKey: "topology",
		cardanonetworkartifacts.PlanManifestKey:    "plan",
		cardanonetworkartifacts.ConnectionKey:      "connection",
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "network-artifacts",
			Annotations: map[string]string{
				ctrlannotations.ArtifactSchemaVersion: cardanonetworkartifacts.SchemaVersion,
				ctrlannotations.LocalnetFingerprint:   "fingerprint",
				ctrlannotations.ArtifactDataHash:      ctrlartifacts.ComputeDataHash(data),
			},
		},
		Data: data,
	}
}

func validConfigMapStatus() yacdv1alpha1.CardanoNetworkArtifactsStatus {
	configMap := validConfigMap()
	return yacdv1alpha1.CardanoNetworkArtifactsStatus{
		NetworkConfigMapName: configMap.Name,
		SchemaVersion:        cardanonetworkartifacts.SchemaVersion,
		DataHash:             configMap.Annotations[ctrlannotations.ArtifactDataHash],
	}
}
