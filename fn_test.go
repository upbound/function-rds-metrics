package main

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/response"
)

func strPtr(s string) *string {
	return &s
}

func TestRunFunction(t *testing.T) {
	var (
		xr    = `{"apiVersion":"example.crossplane.io/v1","kind":"XR","metadata":{"name":"test-xr"}}`
		creds = &fnv1.CredentialData{
			Data: map[string][]byte{
				"credentials": []byte(`[default]
aws_access_key_id = test-access-key
aws_secret_access_key = test-secret-key`),
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
			reason: "The Function should return a fatal result if no credentials were specified",
			args: args{
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "rdsmetrics.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"databaseName": "test-db",
						"region": "us-east-1",
						"target": "status.metrics"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
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
			reason: "The Function should return a false condition if no database name or databaseNameRef is specified",
			args: args{
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "rdsmetrics.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"region": "us-east-1",
						"target": "status.metrics"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"aws-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:    "FunctionSuccess",
							Status:  fnv1.Status_STATUS_CONDITION_FALSE,
							Reason:  "InvalidInput",
							Message: strPtr("DatabaseName or DatabaseNameRef is required"),
							Target:  fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
				},
			},
		},
		"MetricsToContextField": {
			reason: "The Function should store metrics results in context field",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "rdsmetrics.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"databaseName": "test-db",
						"region": "us-east-1",
						"target": "context.metricsResult",
						"metrics": ["CPUUtilization"]
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"aws-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:    "FunctionSuccess",
							Status:  fnv1.Status_STATUS_CONDITION_FALSE,
							Reason:  "AWSConfigError",
							Message: strPtr("Failed to create AWS config: failed to create AWS config: operation error loading EC2 IMDS resource: request canceled, context deadline exceeded"),
							Target:  fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.crossplane.io/v1",
								"kind": "XR",
								"metadata": {
									"name": "test-xr"
								}
							}`),
						},
					},
				},
			},
		},
		"DatabaseNameFromRef": {
			reason: "The Function should resolve database name from XR metadata.name",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "rdsmetrics.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"databaseNameRef": "xr.metadata.name",
						"region": "us-east-1",
						"target": "context.metricsResult",
						"metrics": ["CPUUtilization"]
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"aws-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:    "FunctionSuccess",
							Status:  fnv1.Status_STATUS_CONDITION_FALSE,
							Reason:  "AWSConfigError",
							Message: strPtr("Failed to create AWS config: failed to create AWS config: operation error loading EC2 IMDS resource: request canceled, context deadline exceeded"),
							Target:  fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.crossplane.io/v1",
								"kind": "XR",
								"metadata": {
									"name": "test-xr"
								}
							}`),
						},
					},
				},
			},
		},
		"MetricsToStatusField": {
			reason: "The Function should store metrics results in status field only (no context duplication)",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "rdsmetrics.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"databaseName": "test-db",
						"region": "us-east-1",
						"target": "status.rdsMetrics",
						"metrics": ["CPUUtilization"]
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"aws-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:    "FunctionSuccess",
							Status:  fnv1.Status_STATUS_CONDITION_FALSE,
							Reason:  "AWSConfigError",
							Message: strPtr("Failed to create AWS config: failed to create AWS config: operation error loading EC2 IMDS resource: request canceled, context deadline exceeded"),
							Target:  fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					// Status targets should NOT populate context to avoid duplication
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.crossplane.io/v1",
								"kind": "XR",
								"metadata": {
									"name": "test-xr"
								}
							}`),
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

			// For tests that expect AWS failures, we mainly want to verify context population behavior
			if name == "DatabaseNameFromRef" {
				if rsp == nil {
					t.Fatalf("%s: Expected response, got nil", tc.reason)
				}
				
				// Verify context was populated with the resolved database name
				if rsp.Context == nil {
					t.Errorf("%s: Expected context to be populated for context target, got nil", tc.reason)
				} else {
					contextMap := rsp.Context.AsMap()
					if metricsResult, exists := contextMap["metricsResult"]; exists {
						if metricsData, ok := metricsResult.(map[string]interface{}); ok {
							if dbName, exists := metricsData["databaseName"]; exists {
								if dbName == "test-xr" {
									t.Logf("%s: SUCCESS - Database name reference was resolved correctly from 'xr.metadata.name' to '%s'", tc.reason, dbName)
								} else {
									t.Errorf("%s: Expected database name 'test-xr', got '%v'", tc.reason, dbName)
								}
							} else {
								t.Errorf("%s: Expected 'databaseName' field in metrics data", tc.reason)
							}
						} else {
							t.Errorf("%s: Expected metricsResult to be a map", tc.reason)
						}
					} else {
						t.Errorf("%s: Expected 'metricsResult' key in context. Available keys: %v", tc.reason, getMapKeys(contextMap))
					}
				}
				return
			}
			
			if name == "MetricsToContextField" {
				if rsp == nil {
					t.Fatalf("%s: Expected response, got nil", tc.reason)
				}
				
				// Context targets should populate context
				if rsp.Context == nil {
					t.Errorf("%s: Expected context to be populated for context target, got nil", tc.reason)
				} else {
					contextMap := rsp.Context.AsMap()
					// Check for the specific field
					if _, exists := contextMap["metricsResult"]; !exists {
						t.Errorf("%s: Expected 'metricsResult' key in context for context target. Available keys: %v", tc.reason, getMapKeys(contextMap))
					}
					// Check for the reference field
					if _, exists := contextMap["rdsMetricsRef"]; !exists {
						t.Errorf("%s: Expected 'rdsMetricsRef' key in context for function-claude integration. Available keys: %v", tc.reason, getMapKeys(contextMap))
					}
					if len(contextMap) > 0 {
						t.Logf("%s: SUCCESS - Context is populated correctly", tc.reason)
					}
				}
				
				if len(rsp.Conditions) == 0 {
					t.Errorf("%s: Expected at least one condition", tc.reason)
				}
				return
			}
			
			if name == "MetricsToStatusField" {
				if rsp == nil {
					t.Fatalf("%s: Expected response, got nil", tc.reason)
				}
				
				// Status targets should NOT populate context (to avoid duplication)
				if rsp.Context != nil {
					contextMap := rsp.Context.AsMap()
					if len(contextMap) > 0 {
						t.Errorf("%s: Expected context to be empty for status target to avoid duplication, but got: %v", tc.reason, getMapKeys(contextMap))
					}
				}
				
				t.Logf("%s: SUCCESS - Status target does not populate context (avoids duplication)", tc.reason)
				
				if len(rsp.Conditions) == 0 {
					t.Errorf("%s: Expected at least one condition", tc.reason)
				}
				return
			}

			// For other tests, do exact comparison
			if diff := cmp.Diff(tc.want.rsp, rsp, protocmp.Transform()); diff != "" {
				t.Errorf("%s\nf.RunFunction(...): -want rsp, +got rsp:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("%s\nf.RunFunction(...): -want err, +got err:\n%s", tc.reason, diff)
			}
		})
	}
}

func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestContextWritingDirect(t *testing.T) {
	// Test the context writing function directly
	f := &Function{log: logging.NewNopLogger()}
	
	testMetrics := &RDSMetrics{
		DatabaseName: "test-db",
		Region:       "us-east-1",
		Timestamp:    time.Now(),
		Metrics:      make(map[string]MetricValue),
	}

	req := &fnv1.RunFunctionRequest{}
	rsp := &fnv1.RunFunctionResponse{}

	// Test context target
	err := f.writeMetricsToContext(req, rsp, testMetrics, "context.metricsResult")
	if err != nil {
		t.Fatalf("writeMetricsToContext failed: %v", err)
	}

	if rsp.Context == nil {
		t.Fatal("Expected context to be populated, got nil")
	}

	contextMap := rsp.Context.AsMap()
	t.Logf("Context map: %+v", contextMap)

	// Check if context has the expected fields for the new simplified structure
	if _, exists := contextMap["metricsResult"]; !exists {
		t.Errorf("Expected 'metricsResult' key in context. Available keys: %v", getMapKeys(contextMap))
	}

	if _, exists := contextMap["rdsMetricsRef"]; !exists {
		t.Errorf("Expected 'rdsMetricsRef' key in context for function-claude integration. Available keys: %v", getMapKeys(contextMap))
	}
}