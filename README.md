# function-rds-metrics

A [Crossplane composition function][functions] for fetching Amazon RDS metrics from AWS CloudWatch and making them available in composition pipelines.

## Overview

This function retrieves CloudWatch metrics for RDS database instances and writes them to pipeline context or XR status, enabling intelligent scaling decisions and monitoring workflows in Crossplane compositions.

## Features

- **Dynamic Database Resolution**: Supports both static database names and dynamic references to XR fields or pipeline context
- **Flexible Metric Selection**: Fetch default or custom CloudWatch metrics for RDS instances
- **Pipeline Integration**: Writes metrics to pipeline context for consumption by subsequent functions (e.g., function-claude for AI-driven scaling)
- **Multiple Output Targets**: Store metrics in XR status or pipeline context

## Input Schema

```yaml
apiVersion: rdsmetrics.fn.crossplane.io/v1beta1
kind: Input
spec:
  # Database identifier (use either databaseName OR databaseNameRef)
  databaseName: "my-rds-instance"           # Static database name
  databaseNameRef: "xr.metadata.name"      # Dynamic reference to XR field
  
  # AWS configuration
  region: "us-west-2"                      # AWS region (optional, defaults to us-east-1)
  
  # Metrics configuration
  metrics:                                 # List of CloudWatch metrics to fetch (optional)
    - "CPUUtilization"
    - "DatabaseConnections"
    - "FreeableMemory"
    - "FreeStorageSpace"
  period: 300                             # Data granularity in seconds (optional, default: 300)
  
  # Output configuration
  target: "context.metricsResult"         # Where to store metrics (context.* or status.*)

  # Credentials (optional; without it, the Secret source below is used)
  # identity:
  #   source: Secret                      # Secret | IRSA | WebIdentity | PodIdentity
```

## Database Name References

The function supports dynamic database name resolution using references:

### XR References
- `xr.metadata.name` - Use the XR's metadata name (common pattern for database identifiers)
- `xr.spec.database.instanceId` - Reference nested spec fields
- `xr.status.database.actualName` - Reference nested status fields

### Context References
- `context.databaseName` - Reference pipeline context from previous functions

## Default Metrics

When no metrics are specified, the function fetches these default CloudWatch metrics:

- `CPUUtilization` - CPU usage percentage
- `DatabaseConnections` - Number of active connections
- `FreeableMemory` - Available memory in bytes
- `FreeStorageSpace` - Available storage in bytes
- `ReadIOPS` - Read I/O operations per second
- `WriteIOPS` - Write I/O operations per second
- `ReadLatency` - Read operation latency
- `WriteLatency` - Write operation latency

## Usage Examples

### Basic Usage with Static Database Name

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
spec:
  mode: Pipeline
  pipeline:
  - step: fetch-rds-metrics
    functionRef:
      name: function-rds-metrics
    input:
      apiVersion: rdsmetrics.fn.crossplane.io/v1beta1
      kind: Input
      databaseName: "my-production-db"
      region: "us-west-2"
      target: "status.metrics"
    credentials:
      - name: aws-creds
        source: Secret
        secretRef:
          namespace: crossplane-system
          name: aws-creds
```

### Dynamic Database Name from XR Metadata

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
spec:
  mode: Pipeline
  pipeline:
  - step: fetch-rds-metrics
    functionRef:
      name: function-rds-metrics
    input:
      apiVersion: rdsmetrics.fn.crossplane.io/v1beta1
      kind: Input
      databaseNameRef: "xr.metadata.name"
      region: "us-west-2"
      target: "context.metricsResult"
```

### AI-Driven Scaling Pipeline

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
spec:
  mode: Pipeline
  pipeline:
  - step: fetch-metrics
    functionRef:
      name: function-rds-metrics
    input:
      apiVersion: rdsmetrics.fn.crossplane.io/v1beta1
      kind: Input
      databaseNameRef: "xr.metadata.name"
      region: "us-west-2"
      metrics:
        - "CPUUtilization"
        - "DatabaseConnections"
        - "FreeableMemory"
      target: "context.rdsMetrics"
  
  - step: ai-scaling-decision
    functionRef:
      name: function-claude
    input:
      apiVersion: claude.fn.crossplane.io/v1beta1
      kind: Input
      prompt: "Analyze RDS metrics and recommend scaling actions"
      contextRef: "rdsMetrics"
      target: "status.scalingRecommendation"
```

## Output Format

The function outputs metrics in this structure:

```yaml
databaseName: "my-production-db"
region: "us-west-2"
timestamp: "2025-07-27T10:20:56Z"
metrics:
  CPUUtilization:
    value: 45.2
    unit: "Percent"
    timestamp: "2025-07-27T10:20:00Z"
  DatabaseConnections:
    value: 12
    unit: "Count"
    timestamp: "2025-07-27T10:20:00Z"
  FreeableMemory:
    value: 1073741824
    unit: "Bytes"
    timestamp: "2025-07-27T10:20:00Z"
```

## Testing Examples

The `example/` directory contains several testing scenarios:

### Test Basic Metrics Fetch
```bash
crossplane render example/basic-metrics-query/xr.yaml example/basic-metrics-query/composition.yaml example/functions.yaml --function-credentials=example/secrets/aws-creds.yaml
```

### Test Dynamic Database Name Resolution
```bash
crossplane render example/database-ref-from-xr/xr.yaml example/database-ref-from-xr/composition.yaml example/functions.yaml --function-credentials=example/secrets/aws-creds.yaml --include-context
```

### Test Custom Metrics
```bash
crossplane render example/custom-metrics-query/xr.yaml example/custom-metrics-query/composition.yaml example/functions.yaml --function-credentials=example/secrets/aws-creds.yaml
```

## AWS Credentials

The function requires AWS credentials with CloudWatch read permissions. Provide credentials as a Kubernetes secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: aws-creds
  namespace: crossplane-system
type: Opaque
data:
  credentials: <base64-encoded-aws-credentials-file>
```

The credentials file should be in AWS CLI format:
```ini
[default]
aws_access_key_id = YOUR_ACCESS_KEY
aws_secret_access_key = YOUR_SECRET_KEY
```

The Secret can also hold the keys `access-key-id` and `secret-access-key` (and
optionally `session-token`) directly.

### Identity sources

Set `identity.source` in the input to use credentials without a long-lived key.
It works like the `identity` input of function-aws-query:

| Source | Credentials | `aws-creds` block |
|---|---|---|
| `Secret` (default) | Static credentials from the Secret above | required |
| `IRSA` | SDK default chain: the function pod's ServiceAccount role (web identity token file) | not needed |
| `PodIdentity` | SDK default chain: EKS Pod Identity | not needed |
| `WebIdentity` | Assumes `webIdentity.roleARN` with a web identity token from a file (`tokenConfig.source: Filesystem`, `fsPath`) or from key `token` of the `aws-creds` block (`tokenConfig.source: Secret`) | only for a Secret token |

`identity.assumeRoleChain` assumes further roles, in order, on top of any source.
Without `roleSessionName`, every role session is named `function-rds-metrics`.

```yaml
identity:
  source: WebIdentity
  webIdentity:
    roleARN: arn:aws:iam::123456789012:role/rds-metrics-reader
    tokenConfig:
      source: Filesystem
      fsPath: /var/run/secrets/upbound.io/provider/token
```

The region is always `region` from the input, or `us-east-1` without it. The
`AWS_REGION` environment variable is not used, for any source.

On Upbound control planes, `WebIdentity` with the Upbound OIDC token works without
any secret: set the annotation
`proidc.cloud-spaces.upbound.io/audience: sts.amazonaws.com` on the function's
`DeploymentRuntimeConfig` and read the token from
`/var/run/secrets/upbound.io/provider/token`. The role's trust policy needs
the subject `mcp:<org>/<control plane>:function:<function name>`.

The identity needs `cloudwatch:GetMetricStatistics`.

## Development

### Building the Function

```shell
# Run code generation
go generate ./...

# Run tests
go test ./...

# Build the function's runtime image
docker build . --tag=function-rds-metrics

# Build a function package
crossplane xpkg build -f package --embed-runtime-image=function-rds-metrics
```

### Local Development

For local development, use the Development runtime annotation:

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: Function
metadata:
  name: function-rds-metrics
  annotations:
    render.crossplane.io/runtime: Development
```

Then run the function locally:
```bash
go run . --insecure --debug
```

## Integration with Other Functions

This function is designed to work in composition pipelines with other functions:

- **function-claude**: Provides AI-driven analysis of RDS metrics for intelligent scaling decisions

## Error Handling

The function implements robust error handling:
- **AWS Config Errors**: Returns empty metrics but continues pipeline execution
- **Missing Database**: Fails fast with clear error messages
- **CloudWatch API Errors**: Continues with the metrics that could be fetched. If
  every metric request fails (for example missing permissions or invalid
  credentials), the `FunctionSuccess` condition is `False` with reason
  `CloudWatchError`; if only some fail, the function returns a warning. A metric
  without data points is not an error.
- **Invalid References**: Provides detailed error messages for debugging

[functions]: https://docs.crossplane.io/latest/concepts/composition-functions
