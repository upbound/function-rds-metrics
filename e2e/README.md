# function-rds-metrics end-to-end tests

This directory is a small Upbound project used only to run **real** end-to-end
tests for `function-rds-metrics`: `up test run tests/* --e2e` installs it on a
throwaway control plane, together with the function built from the pull request,
and checks that the function signs in to AWS and fetches CloudWatch metrics. It is
never published to production.

## Layout

```
e2e/
├── upbound.yaml                 # test configuration; depends on the function under test (pinned by CI)
├── apis/xmetrics/               # XRD + Composition that call the function
├── functions/.gitkeep           # must exist for `up`
└── tests/
    └── e2etest-metrics/
        └── test.yaml            # E2ETest: default DRC audience, applies an XMetrics, waits for Ready + FunctionSuccess
```

`up test` builds this project, installs it on the control plane (the function
under test and `function-auto-ready` come along as dependencies), then creates the
XMetrics from `test.yaml`. Dependency-installed functions are named `<org>-<repo>`,
so the Composition references `solutions-function-rds-metrics` and
`crossplane-contrib-function-auto-ready`.

## What the test proves

The XMetrics names a database that doesn't exist. CloudWatch then returns no data
points, but every request still has to succeed, so the test covers sign-in, the
role's permission and the CloudWatch call without an RDS instance.

The function creates no resources, so the XR is `Ready` even when CloudWatch
fails. The test therefore also requires `FunctionSuccess`, which the function
sets to `False` when every metric request fails. Pointing the Composition at a
role that doesn't exist makes the test fail on exactly that condition.

The `databaseNameRef: xr.spec.databaseName` in the Composition also exercises
reference resolution from the XR spec.

## Credentials (no AWS secrets in CI)

The function uses `identity.source: WebIdentity` with the Upbound OIDC token of
its own pod:

- `test.yaml` patches the control plane's `default` `DeploymentRuntimeConfig` with
  `proidc.cloud-spaces.upbound.io/audience: sts.amazonaws.com`. Upbound then
  mounts a token at `/var/run/secrets/upbound.io/provider/token` (function pods
  too). That annotation is the only field the Spaces policy allows here.
- The Composition assumes the role below with that token.

### AWS setup

Role `solutions-e2e-function` (account `609897127049`), shared with the other
function e2e tests. Trust policy:

```json
{
  "Effect": "Allow",
  "Principal": { "Federated": "arn:aws:iam::609897127049:oidc-provider/proidc.upbound.io" },
  "Action": "sts:AssumeRoleWithWebIdentity",
  "Condition": { "StringLike": {
    "proidc.upbound.io:sub": "mcp:solutions/*:function:*",
    "proidc.upbound.io:aud": "sts.amazonaws.com"
  }}
}
```

Its read-only policy must allow `cloudwatch:GetMetricStatistics` for this test
(next to the actions other function tests need).

## Two repos, one function

The function dev build is pushed to `solutions/<repo>` and the wrapper project to
**`solutions/<repo>-e2e-wrapper`**. They must be separate: Crossplane's package
lock keys dependency-graph nodes by source repo, so a project and a function
sharing one repo collide (`node ... already exists`). The `<repo>-e2e-wrapper`
repo is granted to the e2e robot in `solutions-gitops-prod`.

## What runs in CI

On a pull request labelled `run-e2e-tests`, `.github/workflows/e2e.yaml` (synced
from `upbound/sa-up`):

1. builds the function package from the PR head and pushes it to `solutions/<repo>`
   at a dev tag,
2. pins that tag in `upbound.yaml` (`@FUNCTION_VERSION@`),
3. switches to the e2e Spaces control plane group, and
4. runs `up test run tests/* --e2e`, pushing the wrapper project to
   `solutions/<repo>-e2e-wrapper`.

A separate job then posts the `e2e` commit status that branch protection requires.
