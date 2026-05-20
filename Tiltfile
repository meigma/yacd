EXPECTED_CONTEXT = 'kind-template-k8s-dev'
NAMESPACE = 'template-k8s-system'
IMAGE = 'ghcr.io/meigma/template-k8s'
CHART = 'charts/template-k8s'

allow_k8s_contexts(EXPECTED_CONTEXT)
if k8s_context() != EXPECTED_CONTEXT:
    fail('Tilt may only run against %s. Run `moon run root:dev-up` from a clean shell.' % EXPECTED_CONTEXT)

ci_settings(timeout='10m', readiness_timeout='5m')

k8s_yaml(blob("""
apiVersion: v1
kind: Namespace
metadata:
  name: template-k8s-system
  labels:
    pod-security.kubernetes.io/enforce: restricted
"""))

custom_build(
    IMAGE,
    './dev/ko-build.sh',
    deps=['api', 'cmd', 'internal', 'go.mod', 'go.sum', '.ko.yaml'],
)

k8s_yaml(helm(
    CHART,
    name='template-k8s',
    namespace=NAMESPACE,
    set=[
        'image.repository=%s' % IMAGE,
        'image.tag=tilt',
        'image.pullPolicy=IfNotPresent',
        'leaderElection.enabled=false',
    ],
))

k8s_resource(
    workload='template-k8s-controller-manager',
    new_name='controller',
)
