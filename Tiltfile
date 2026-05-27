EXPECTED_CONTEXT = 'kind-yacd-dev'
NAMESPACE = 'yacd-system'
IMAGE = 'ghcr.io/meigma/yacd'
FAUCET_IMAGE = 'ghcr.io/meigma/yacd/faucet'
CARDANO_TESTNET_IMAGE = 'ghcr.io/meigma/yacd/cardano-testnet'
CHART = 'charts/yacd'

allow_k8s_contexts(EXPECTED_CONTEXT)
if k8s_context() != EXPECTED_CONTEXT:
    fail('Tilt may only run against %s. Run `moon run root:dev-up` from a clean shell.' % EXPECTED_CONTEXT)

ci_settings(timeout='10m', readiness_timeout='5m')

k8s_yaml(blob("""
apiVersion: v1
kind: Namespace
metadata:
  name: yacd-system
  labels:
    pod-security.kubernetes.io/enforce: restricted
"""))

custom_build(
    IMAGE,
    './.dev/ko-build.sh',
    deps=['cmd', 'api', 'internal', 'go.mod', 'go.sum', '.ko.yaml', '.dev/ko-build.sh'],
)

local_resource(
    name='faucet-image',
    cmd='EXPECTED_REF=%s:tilt ./.dev/ko-build-faucet.sh && kind load docker-image --name yacd-dev %s:tilt' % (FAUCET_IMAGE, FAUCET_IMAGE),
    deps=['services/faucet', 'go.mod', 'go.sum', '.ko.yaml', '.dev/ko-build-faucet.sh'],
)

# Build the cardano-testnet tools image from local source so the operator
# uses a publisher that includes post-release changes db-sync depends on
# (notably the genesis hash enrichment added in PR #31). The published
# yacd.N tag is rebuilt on release-please cuts; this resource keeps the dev
# loop unblocked between cuts.
local_resource(
    name='cardano-testnet-image',
    cmd='EXPECTED_REF=%s:tilt ./.dev/build-cardano-testnet.sh && kind load docker-image --name yacd-dev %s:tilt' % (CARDANO_TESTNET_IMAGE, CARDANO_TESTNET_IMAGE),
    deps=['containers/cardano-testnet', '.dev/build-cardano-testnet.sh'],
)

k8s_yaml(helm(
    CHART,
    name='yacd',
    namespace=NAMESPACE,
    set=[
        'image.repository=%s' % IMAGE,
        'image.tag=tilt',
        'image.pullPolicy=IfNotPresent',
        'faucet.image.repository=%s' % FAUCET_IMAGE,
        'faucet.image.tag=tilt',
        'cardanoTestnet.image.repository=%s' % CARDANO_TESTNET_IMAGE,
        'cardanoTestnet.image.tag=tilt',
        'leaderElection.enabled=false',
        'manager.logLevel=debug',
    ],
))

k8s_resource(
    workload='yacd-controller-manager',
    new_name='controller',
    resource_deps=['faucet-image', 'cardano-testnet-image'],
)
