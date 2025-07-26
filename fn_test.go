package main

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/response"
)

func TestWriteMetricsToContext(t *testing.T) {
	f := &Function{log: logging.NewNopLogger()}

	testMetrics := &RDSMetrics{
		DatabaseName: "test-db",
		Region:       "us-east-1",
		Timestamp:    time.Now(),
		Metrics: map[string]MetricValue{
			"CPUUtilization": {
				Value:     85.5,
				Unit:      "Percent",
				Timestamp: time.Now(),
			},
			"DatabaseConnections": {
				Value:     45,
				Unit:      "Count",
				Timestamp: time.Now(),
			},
		},
	}

	type args struct {
		rsp     *fnv1.RunFunctionResponse
		metrics *RDSMetrics
	}
	type want struct {
		err                error
		contextShouldExist bool
		rdsMetricsKey      bool
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"WriteToEmptyContext": {
			reason: "Should create context and write metrics when context is nil",
			args: args{
				rsp:     &fnv1.RunFunctionResponse{},
				metrics: testMetrics,
			},
			want: want{
				err:                nil,
				contextShouldExist: true,
				rdsMetricsKey:      true,
			},
		},
		"WriteToExistingContext": {
			reason: "Should preserve existing context and add metrics",
			args: args{
				rsp: &fnv1.RunFunctionResponse{
					Context: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"existing-key": {
								Kind: &structpb.Value_StringValue{StringValue: "existing-value"},
							},
						},
					},
				},
				metrics: testMetrics,
			},
			want: want{
				err:                nil,
				contextShouldExist: true,
				rdsMetricsKey:      true,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := f.writeMetricsToContext(tc.args.rsp, tc.args.metrics)

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("%s\nwriteMetricsToContext(...): -want err, +got err:\n%s", tc.reason, diff)
			}

			if tc.want.contextShouldExist && tc.args.rsp.Context == nil {
				t.Errorf("%s: Expected context to exist", tc.reason)
			}

			if tc.want.rdsMetricsKey {
				if _, exists := tc.args.rsp.Context.Fields["rds-metrics"]; !exists {
					t.Errorf("%s: Expected 'rds-metrics' key in context", tc.reason)
				}
			}
		})
	}
}

func TestRunFunction(t *testing.T) {
	var (
		xr = `{"apiVersion":"aws.platform.upbound.io/v1alpha1","kind":"XSQLInstance","metadata":{"name":"test-db"},"spec":{"parameters":{"region":"us-east-1","engine":"postgres"}}}`
		awsCreds = &fnv1.CredentialData{
			Data: map[string][]byte{
				"access-key-id":     []byte("test-access-key"),
				"secret-access-key": []byte("test-secret-key"),
			},
		}
	)

	type args struct {
		ctx context.Context
		req *fnv1.RunFunctionRequest
	}
	type want struct {
		rsp *fnv1.RunFunctionResponse
		err error
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"MissingCredentials": {
			reason: "Should return fatal error when AWS credentials are missing",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "test"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "rdsmetrics.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"databaseName": "test-db",
						"region": "us-east-1",
						"target": "status.metrics"
					}`),
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "test", Ttl: durationpb.New(response.DefaultTTL)},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_FATAL,
							Message:  "failed to get aws-creds credentials",
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
				},
			},
		},
		"MissingDatabaseName": {
			reason: "Should return condition false when database name is missing",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "test"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "rdsmetrics.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"region": "us-east-1",
						"target": "status.metrics"
					}`),
					Credentials: map[string]*fnv1.Credentials{
						"aws-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: awsCreds},
						},
					},
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "test", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_FALSE,
							Reason: "InvalidInput",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &Function{log: logging.NewNopLogger()}
			rsp, err := f.RunFunction(tc.args.ctx, tc.args.req)

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("%s\nf.RunFunction(...): -want err, +got err:\n%s", tc.reason, diff)
			}

			// Compare specific fields to avoid issues with timestamps and complex objects
			if len(tc.want.rsp.Results) > 0 && len(rsp.Results) > 0 {
				if tc.want.rsp.Results[0].Severity != rsp.Results[0].Severity {
					t.Errorf("%s: Expected severity %v, got %v", tc.reason, tc.want.rsp.Results[0].Severity, rsp.Results[0].Severity)
				}
			}

			if len(tc.want.rsp.Conditions) > 0 && len(rsp.Conditions) > 0 {
				if tc.want.rsp.Conditions[0].Type != rsp.Conditions[0].Type {
					t.Errorf("%s: Expected condition type %v, got %v", tc.reason, tc.want.rsp.Conditions[0].Type, rsp.Conditions[0].Type)
				}
				if tc.want.rsp.Conditions[0].Status != rsp.Conditions[0].Status {
					t.Errorf("%s: Expected condition status %v, got %v", tc.reason, tc.want.rsp.Conditions[0].Status, rsp.Conditions[0].Status)
				}
			}
		})
	}
}
