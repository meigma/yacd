EXPECTED_CONTEXT = 'kind-yacd-dev'
NAMESPACE = 'yacd-system'
IMAGE = 'ghcr.io/meigma/yacd'
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
    './dev/ko-build.sh',
    deps=['cmd', 'go.mod', 'go.sum', '.ko.yaml'],
)

k8s_yaml(helm(
    CHART,
    name='yacd',
    namespace=NAMESPACE,
    set=[
        'image.repository=%s' % IMAGE,
        'image.tag=tilt',
        'image.pullPolicy=IfNotPresent',
        'leaderElection.enabled=false',
        'manager.logLevel=debug',
    ],
))

k8s_resource(
    workload='yacd-controller-manager',
    new_name='controller',
)
