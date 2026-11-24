package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/smithy-go"

	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
)

// fakeCloudWatch answers GetMetricStatistics per metric name: an error if one
// is set for the metric, otherwise the given data points (none by default).
type fakeCloudWatch struct {
	errs   map[string]error
	points map[string][]types.Datapoint
}

func (f fakeCloudWatch) GetMetricStatistics(_ context.Context, in *cloudwatch.GetMetricStatisticsInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricStatisticsOutput, error) {
	name := aws.ToString(in.MetricName)
	if err, ok := f.errs[name]; ok {
		return nil, err
	}
	return &cloudwatch.GetMetricStatisticsOutput{Datapoints: f.points[name]}, nil
}

func runWithFakeCloudWatch(t *testing.T, cw fakeCloudWatch) *fnv1.RunFunctionResponse {
	t.Helper()
	return runWith(t, cw, `{
			"apiVersion": "rdsmetrics.fn.crossplane.io/v1beta1",
			"kind": "Input",
			"databaseName": "test-db",
			"region": "us-east-1",
			"target": "status.rdsMetrics",
			"metrics": ["CPUUtilization", "FreeableMemory"]
		}`, map[string][]byte{"access-key-id": []byte("AK"), "secret-access-key": []byte("SK")})
}

// runWith runs the function with the given input and aws-creds data, against a
// fake CloudWatch.
func runWith(t *testing.T, cw fakeCloudWatch, input string, awsCreds map[string][]byte) *fnv1.RunFunctionResponse {
	t.Helper()
	f := &Function{
		log:           logging.NewNopLogger(),
		newCloudWatch: func(aws.Config) cloudWatchAPI { return cw },
	}
	req := &fnv1.RunFunctionRequest{
		Meta:  &fnv1.RequestMeta{Tag: "t"},
		Input: resource.MustStructJSON(input),
		Observed: &fnv1.State{Composite: &fnv1.Resource{
			Resource: resource.MustStructJSON(`{"apiVersion":"example.crossplane.io/v1","kind":"XR","metadata":{"name":"test-xr"}}`),
		}},
		Credentials: map[string]*fnv1.Credentials{
			"aws-creds": {Source: &fnv1.Credentials_CredentialData{CredentialData: &fnv1.CredentialData{
				Data: awsCreds,
			}}},
		},
	}
	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("RunFunction returned error: %v", err)
	}
	return rsp
}

func functionSuccess(rsp *fnv1.RunFunctionResponse) *fnv1.Condition {
	for _, c := range rsp.GetConditions() {
		if c.GetType() == "FunctionSuccess" {
			return c
		}
	}
	return nil
}

func warnings(rsp *fnv1.RunFunctionResponse) []string {
	var out []string
	for _, r := range rsp.GetResults() {
		if r.GetSeverity() == fnv1.Severity_SEVERITY_WARNING {
			out = append(out, r.GetMessage())
		}
	}
	return out
}

func hasFatal(rsp *fnv1.RunFunctionResponse) bool {
	for _, r := range rsp.GetResults() {
		if r.GetSeverity() == fnv1.Severity_SEVERITY_FATAL {
			return true
		}
	}
	return false
}

func TestCloudWatchErrorReporting(t *testing.T) {
	denied := errors.New("AccessDenied: not authorized to perform cloudwatch:GetMetricStatistics")
	now := time.Now()

	t.Run("AllRequestsFailIsFunctionSuccessFalse", func(t *testing.T) {
		rsp := runWithFakeCloudWatch(t, fakeCloudWatch{errs: map[string]error{"CPUUtilization": denied, "FreeableMemory": denied}})
		c := functionSuccess(rsp)
		if c == nil || c.GetStatus() != fnv1.Status_STATUS_CONDITION_FALSE || c.GetReason() != "CloudWatchError" {
			t.Fatalf("FunctionSuccess = %v, want False/CloudWatchError", c)
		}
		if !strings.Contains(c.GetMessage(), "2 of 2") || !strings.Contains(c.GetMessage(), "AccessDenied") {
			t.Errorf("message = %q, want the failure count and the first error", c.GetMessage())
		}
		// A later step can overwrite the condition, so the warning is reported too.
		if len(warnings(rsp)) != 1 {
			t.Errorf("warnings = %v, want one", warnings(rsp))
		}
		if hasFatal(rsp) {
			t.Error("CloudWatch errors must not stop the pipeline")
		}
		if _, ok := rsp.GetContext().AsMap()["rdsMetrics"]; !ok {
			t.Error("metrics should still be written to the context for pipeline continuity")
		}
	})

	t.Run("SomeRequestsFailIsWarning", func(t *testing.T) {
		rsp := runWithFakeCloudWatch(t, fakeCloudWatch{
			errs: map[string]error{"FreeableMemory": denied},
			points: map[string][]types.Datapoint{"CPUUtilization": {{
				Timestamp: &now, Average: aws.Float64(12.5), Unit: types.StandardUnitPercent,
			}}},
		})
		c := functionSuccess(rsp)
		if c == nil || c.GetStatus() != fnv1.Status_STATUS_CONDITION_TRUE {
			t.Fatalf("FunctionSuccess = %v, want True", c)
		}
		w := warnings(rsp)
		if len(w) != 1 || !strings.Contains(w[0], "1 of 2") {
			t.Errorf("warnings = %v, want one mentioning 1 of 2", w)
		}
	})

	t.Run("NoDataPointsIsNotAnError", func(t *testing.T) {
		rsp := runWithFakeCloudWatch(t, fakeCloudWatch{})
		c := functionSuccess(rsp)
		if c == nil || c.GetStatus() != fnv1.Status_STATUS_CONDITION_TRUE {
			t.Fatalf("FunctionSuccess = %v, want True", c)
		}
		if len(warnings(rsp)) != 0 {
			t.Errorf("warnings = %v, want none", warnings(rsp))
		}
	})
}

func TestNoCredentialsBlockNeededForIRSA(t *testing.T) {
	f := &Function{
		log:           logging.NewNopLogger(),
		newCloudWatch: func(aws.Config) cloudWatchAPI { return fakeCloudWatch{} },
	}
	req := &fnv1.RunFunctionRequest{
		Meta: &fnv1.RequestMeta{Tag: "t"},
		Input: resource.MustStructJSON(`{
			"apiVersion": "rdsmetrics.fn.crossplane.io/v1beta1",
			"kind": "Input",
			"databaseName": "test-db",
			"target": "context.metricsResult",
			"identity": {"source": "IRSA"}
		}`),
		Observed: &fnv1.State{Composite: &fnv1.Resource{
			Resource: resource.MustStructJSON(`{"apiVersion":"example.crossplane.io/v1","kind":"XR","metadata":{"name":"test-xr"}}`),
		}},
	}
	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("RunFunction returned error: %v", err)
	}
	if hasFatal(rsp) {
		t.Fatalf("IRSA without an aws-creds block must not be fatal: %v", rsp.GetResults())
	}
	if c := functionSuccess(rsp); c == nil || c.GetStatus() != fnv1.Status_STATUS_CONDITION_TRUE {
		t.Errorf("FunctionSuccess = %v, want True", c)
	}
}

func TestDatabaseNameRefFromXRSpec(t *testing.T) {
	var gotDB string
	f := &Function{
		log: logging.NewNopLogger(),
		newCloudWatch: func(aws.Config) cloudWatchAPI {
			return recordingCloudWatch{db: &gotDB}
		},
	}
	// As on the first pipeline step: the observed XR has the spec, the desired
	// XR is empty.
	req := &fnv1.RunFunctionRequest{
		Meta: &fnv1.RequestMeta{Tag: "t"},
		Input: resource.MustStructJSON(`{
			"apiVersion": "rdsmetrics.fn.crossplane.io/v1beta1",
			"kind": "Input",
			"databaseNameRef": "xr.spec.databaseName",
			"target": "context.metricsResult",
			"metrics": ["CPUUtilization"],
			"identity": {"source": "IRSA"}
		}`),
		Observed: &fnv1.State{Composite: &fnv1.Resource{
			Resource: resource.MustStructJSON(`{"apiVersion":"example.crossplane.io/v1","kind":"XR","metadata":{"name":"test-xr"},"spec":{"databaseName":"orders-db"}}`),
		}},
	}
	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("RunFunction returned error: %v", err)
	}
	if hasFatal(rsp) {
		t.Fatalf("unexpected fatal result: %v", rsp.GetResults())
	}
	if gotDB != "orders-db" {
		t.Errorf("queried database %q, want orders-db", gotDB)
	}
}

// recordingCloudWatch records the DBInstanceIdentifier it was asked about.
type recordingCloudWatch struct{ db *string }

func (r recordingCloudWatch) GetMetricStatistics(_ context.Context, in *cloudwatch.GetMetricStatisticsInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricStatisticsOutput, error) {
	for _, d := range in.Dimensions {
		if aws.ToString(d.Name) == "DBInstanceIdentifier" {
			*r.db = aws.ToString(d.Value)
		}
	}
	return &cloudwatch.GetMetricStatisticsOutput{}, nil
}

func TestCloudWatchErrorTextIsStable(t *testing.T) {
	// What the SDK returns: an API error wrapped with a per-request ID.
	apiErr := &smithy.GenericAPIError{Code: "AccessDenied", Message: "not authorized to perform cloudwatch:GetMetricStatistics"}
	wrapped := fmt.Errorf("operation error CloudWatch: GetMetricStatistics, https response error StatusCode: 403, RequestID: 1234-abcd, %w", apiErr)

	rsp := runWithFakeCloudWatch(t, fakeCloudWatch{errs: map[string]error{"CPUUtilization": wrapped, "FreeableMemory": wrapped}})
	msg := functionSuccess(rsp).GetMessage()
	if strings.Contains(msg, "RequestID") || strings.Contains(msg, "1234-abcd") {
		t.Errorf("message %q contains the per-request ID", msg)
	}
	if !strings.Contains(msg, "AccessDenied: not authorized") {
		t.Errorf("message %q, want the API error code and message", msg)
	}
}

func TestWebIdentityTokenFromCredentialsBlock(t *testing.T) {
	// WebIdentity with the token as the only key of aws-creds: no static keys
	// needed, and the metrics run with the assumed role (CloudWatch is faked).
	rsp := runWith(t, fakeCloudWatch{}, `{
			"apiVersion": "rdsmetrics.fn.crossplane.io/v1beta1",
			"kind": "Input",
			"databaseName": "test-db",
			"region": "us-east-1",
			"target": "status.rdsMetrics",
			"metrics": ["CPUUtilization"],
			"identity": {"source": "WebIdentity", "webIdentity": {"roleARN": "arn:aws:iam::111:role/x"}}
		}`, map[string][]byte{"token": []byte("tok")})
	if hasFatal(rsp) {
		t.Fatalf("unexpected fatal result: %v", rsp.GetResults())
	}
	if c := functionSuccess(rsp); c == nil || c.GetStatus() != fnv1.Status_STATUS_CONDITION_TRUE {
		t.Errorf("FunctionSuccess = %v, want True", c)
	}
}
