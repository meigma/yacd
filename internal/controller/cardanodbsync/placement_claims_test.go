package cardanodbsync

import (
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestSelectPrimarySidecarClaim(t *testing.T) {
	base := time.Date(2026, 5, 28, 21, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		claims        []yacdv1alpha1.CardanoDBSync
		wantIncumbent string
		wantConflicts []string
	}{
		{
			name: "zero claims",
		},
		{
			name: "one claim",
			claims: []yacdv1alpha1.CardanoDBSync{
				primarySidecarClaim("default", "only", base, "uid-only"),
			},
			wantIncumbent: "default/only",
			wantConflicts: []string{},
		},
		{
			name: "oldest creation timestamp wins",
			claims: []yacdv1alpha1.CardanoDBSync{
				primarySidecarClaim("default", "newer", base.Add(time.Minute), "uid-newer"),
				primarySidecarClaim("default", "older", base, "uid-older"),
			},
			wantIncumbent: "default/older",
			wantConflicts: []string{"default/newer"},
		},
		{
			name: "uid breaks creation timestamp tie",
			claims: []yacdv1alpha1.CardanoDBSync{
				primarySidecarClaim("default", "later-uid", base, "uid-b"),
				primarySidecarClaim("default", "earlier-uid", base, "uid-a"),
			},
			wantIncumbent: "default/earlier-uid",
			wantConflicts: []string{"default/later-uid"},
		},
		{
			name: "namespace name breaks uid tie",
			claims: []yacdv1alpha1.CardanoDBSync{
				primarySidecarClaim("z-ns", "winner-by-name", base, "same-uid"),
				primarySidecarClaim("a-ns", "winner-by-namespace", base, "same-uid"),
			},
			wantIncumbent: "a-ns/winner-by-namespace",
			wantConflicts: []string{"z-ns/winner-by-name"},
		},
		{
			name: "name breaks namespace tie",
			claims: []yacdv1alpha1.CardanoDBSync{
				primarySidecarClaim("default", "z-name", base, "same-uid"),
				primarySidecarClaim("default", "a-name", base, "same-uid"),
			},
			wantIncumbent: "default/a-name",
			wantConflicts: []string{"default/z-name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selection := SelectPrimarySidecarClaim(tt.claims)

			if tt.wantIncumbent == "" {
				require.Nil(t, selection.Incumbent)
				assert.Empty(t, selection.ConflictingPeerKeys)
				return
			}
			require.NotNil(t, selection.Incumbent)
			assert.Equal(t, tt.wantIncumbent, primarySidecarClaimKey(selection.Incumbent))
			assert.Equal(t, tt.wantConflicts, selection.ConflictingPeerKeys)
		})
	}
}

func primarySidecarClaim(namespace string, name string, created time.Time, uid string) yacdv1alpha1.CardanoDBSync {
	return yacdv1alpha1.CardanoDBSync{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              name,
			CreationTimestamp: metav1.NewTime(created),
			UID:               types.UID(uid),
		},
	}
}
