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

	// Identity selects how the function gets AWS credentials. Without it, the
	// function reads static credentials from the "aws-creds" credentials block,
	// as before (Secret source).
	// +optional
	Identity *Identity `json:"identity,omitempty"`
}

// Identity mirrors provider-upjet-aws spec.credentials + spec.assumeRoleChain,
// and the identity input of function-aws-query.
type Identity struct {
	// Source of the base credentials.
	// +kubebuilder:validation:Enum=Secret;IRSA;WebIdentity;PodIdentity
	// +kubebuilder:default=Secret
	// +optional
	Source IdentitySource `json:"source,omitempty"`

	// WebIdentity configures the AssumeRoleWithWebIdentity flow. Required when
	// Source is WebIdentity.
	// +optional
	WebIdentity *WebIdentity `json:"webIdentity,omitempty"`

	// AssumeRoleChain assumes a chain of roles (in order) on top of the base
	// credentials resolved from Source.
	// +optional
	AssumeRoleChain []AssumeRole `json:"assumeRoleChain,omitempty"`
}

// IdentitySource controls how base credentials are resolved.
// Supported values: Secret;IRSA;WebIdentity;PodIdentity.
type IdentitySource string

const (
	// IdentitySourceSecret reads static credentials from the function
	// credentials block named "aws-creds": keys access-key-id and
	// secret-access-key (optional session-token), or a shared-credentials INI
	// under key "credentials".
	IdentitySourceSecret IdentitySource = "Secret"
	// IdentitySourceIRSA uses IAM Roles for Service Accounts: the function pod's
	// ServiceAccount carries the role; credentials come from the SDK default
	// chain (web identity token file). No secret required.
	IdentitySourceIRSA IdentitySource = "IRSA"
	// IdentitySourceWebIdentity uses an explicit role ARN plus a web identity
	// token (sourced from a Secret or the filesystem).
	IdentitySourceWebIdentity IdentitySource = "WebIdentity"
	// IdentitySourcePodIdentity uses EKS Pod Identity: credentials come from the
	// container credentials endpoint via the SDK default chain. No secret.
	IdentitySourcePodIdentity IdentitySource = "PodIdentity"
)

// WebIdentity configures the AssumeRoleWithWebIdentity flow.
type WebIdentity struct {
	// RoleARN is the IAM role to assume with the web identity token.
	RoleARN string `json:"roleARN"` //nolint:tagliatelle // mirrors provider-upjet-aws field name

	// RoleSessionName is an optional session name for the assumed role.
	// +optional
	RoleSessionName string `json:"roleSessionName,omitempty"`

	// TokenConfig selects where the web identity token comes from.
	// +optional
	TokenConfig *TokenConfig `json:"tokenConfig,omitempty"`
}

// TokenConfig selects the source of a web identity token.
type TokenConfig struct {
	// Source of the token. With Secret, the token bytes arrive via the "aws-creds"
	// credentials block (key "token"). With Filesystem, FSPath is read.
	// +kubebuilder:validation:Enum=Secret;Filesystem
	Source string `json:"source"`

	// FSPath is the path to the token file when Source is Filesystem.
	// +optional
	FSPath *string `json:"fsPath,omitempty"`
}

// AssumeRole describes a single STS AssumeRole hop.
type AssumeRole struct {
	// RoleARN is the role to assume.
	RoleARN string `json:"roleARN"` //nolint:tagliatelle // mirrors provider-upjet-aws field name

	// ExternalID to pass to AssumeRole, if the role's trust policy requires it.
	// +optional
	ExternalID *string `json:"externalID,omitempty"`

	// RoleSessionName for the assumed role session.
	// +optional
	RoleSessionName *string `json:"roleSessionName,omitempty"`

	// Tags are session tags to pass to AssumeRole.
	// +optional
	Tags map[string]string `json:"tags,omitempty"`

	// TransitiveTagKeys lists session tag keys that propagate to downstream
	// AssumeRole calls in the chain.
	// +optional
	TransitiveTagKeys []string `json:"transitiveTagKeys,omitempty"`
}
