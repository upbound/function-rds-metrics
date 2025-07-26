# RDS Metrics Function Examples

You can run the RDS metrics function locally and test it using `crossplane beta render` with these example manifests.

## Setup

1. **Create AWS credentials secret:**
```shell
kubectl create secret generic aws-creds -n crossplane-system --from-file=credentials=$HOME/.aws/credentials --dry-run=client -o yaml | kubectl apply -f -
```

2. **Or use the example secret (for testing only):**
```shell
kubectl apply -f secrets/aws-creds.yaml
```

## Testing Scenarios

### Basic Test
```shell
# Run the function locally
$ go run . --insecure --debug

# In another terminal, test basic functionality
$ crossplane render xr.yaml composition.yaml functions.yaml --function-credentials=secrets/aws-creds.yaml -rc
```

### Custom Metrics Query
```shell
$ crossplane render custom-metrics-query/xr.yaml custom-metrics-query/composition.yaml custom-metrics-query/functions.yaml --function-credentials=secrets/aws-creds.yaml -rc
```

### Context Testing
```shell  
$ crossplane render context-testing/xr.yaml context-testing/composition.yaml context-testing/functions.yaml --function-credentials=secrets/aws-creds.yaml -rc
```

## Expected Output

The function should:
1. Write RDS metrics to XR status at the specified target path
2. Write metrics to pipeline context for downstream functions
3. Set appropriate Crossplane conditions

Example successful output:
```yaml
---
apiVersion: example.crossplane.io/v1
kind: XR
metadata:
  name: example-rds-metrics-test
status:
  rdsMetrics:
    databaseName: "example-rds-instance"
    region: "us-east-1"
    timestamp: "2025-01-26T..."
    metrics:
      CPUUtilization:
        value: 25.5
        unit: "Percent"
        timestamp: "2025-01-26T..."
---
apiVersion: render.crossplane.io/v1beta1
kind: Result
message: Successfully fetched metrics for RDS instance example-rds-instance
severity: SEVERITY_NORMAL
step: fetch-metrics
```
