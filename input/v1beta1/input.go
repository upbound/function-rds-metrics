// Package v1beta1 contains the input type for this Function
// +kubebuilder:object:generate=true
// +groupName=rdsmetrics.fn.crossplane.io
// +versionName=v1alpha1
package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Input can be used to provide input to this Function.
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:categories=crossplane
type Input struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// DatabaseName is the name of the RDS database instance to fetch metrics for
	// +optional
	DatabaseName string `json:"databaseName,omitempty"`

	// DatabaseNameRef is a reference to retrieve the database name (e.g., from status or context)
	// Overrides DatabaseName field if used
	// +optional
	DatabaseNameRef *string `json:"databaseNameRef,omitempty"`

	// Region is the AWS region where the RDS instance is located
	Region string `json:"region,omitempty"`

	// Metrics is a list of CloudWatch metrics to fetch
	Metrics []string `json:"metrics,omitempty"`

	// Period is the granularity of the returned data points in seconds
	Period int32 `json:"period,omitempty"`

	// Target where to store the metrics result
	Target string `json:"target"`
}
