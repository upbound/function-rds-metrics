package main

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/upbound/function-rds-metrics/input/v1beta1"

	"github.com/crossplane/function-sdk-go/errors"
)

// defaultRegion is used when the input sets no region.
const defaultRegion = "us-east-1"

// defaultRoleSessionName is the STS session name when the input sets none.
const defaultRoleSessionName = "function-rds-metrics"

// identitySource returns the configured credential source, Secret by default.
func identitySource(in *v1beta1.Input) v1beta1.IdentitySource {
	if in.Identity != nil && in.Identity.Source != "" {
		return in.Identity.Source
	}
	return v1beta1.IdentitySourceSecret
}

// needsCredentialsBlock reports whether the input needs the "aws-creds"
// credentials block: for static credentials, or for a web identity token that
// is delivered through it.
func needsCredentialsBlock(in *v1beta1.Input) bool {
	switch identitySource(in) {
	case v1beta1.IdentitySourceSecret:
		return true
	case v1beta1.IdentitySourceWebIdentity:
		// Without webIdentity, buildAWSConfig reports the missing roleARN.
		wi := in.Identity.WebIdentity
		return wi != nil && (wi.TokenConfig == nil || wi.TokenConfig.Source == "" || wi.TokenConfig.Source == "Secret")
	case v1beta1.IdentitySourceIRSA, v1beta1.IdentitySourcePodIdentity:
		return false
	default:
		return false
	}
}

// buildAWSConfig resolves an AWS config for the input's identity. creds holds
// the decoded "aws-creds" credentials block; it is empty when the identity
// source needs none.
func buildAWSConfig(ctx context.Context, creds map[string][]byte, in *v1beta1.Input) (aws.Config, error) { //nolint:gocyclo // straight-line credential-source selection; splitting hurts readability
	region := in.Region
	if region == "" {
		region = defaultRegion
	}
	opts := []func(*config.LoadOptions) error{config.WithRegion(region)}

	var webIdentity *v1beta1.WebIdentity
	var chain []v1beta1.AssumeRole
	if in.Identity != nil {
		webIdentity = in.Identity.WebIdentity
		chain = in.Identity.AssumeRoleChain
	}

	switch source := identitySource(in); source {
	case v1beta1.IdentitySourceSecret:
		static, err := parseStaticCredentials(creds)
		if err != nil {
			return aws.Config{}, err
		}
		opts = append(opts, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			static["access-key-id"], static["secret-access-key"], static["session-token"])))
	case v1beta1.IdentitySourceIRSA, v1beta1.IdentitySourcePodIdentity:
		// No secret: the SDK default credential chain resolves the web identity
		// token file (IRSA) or the container credentials endpoint (Pod Identity).
	case v1beta1.IdentitySourceWebIdentity:
		if webIdentity == nil || webIdentity.RoleARN == "" {
			return aws.Config{}, errors.New("identity.webIdentity.roleARN is required for the WebIdentity source")
		}
		base, err := config.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			return aws.Config{}, errors.Wrap(err, "cannot load base AWS config for WebIdentity")
		}
		retriever, err := webIdentityTokenRetriever(creds["token"], webIdentity)
		if err != nil {
			return aws.Config{}, err
		}
		provider := stscreds.NewWebIdentityRoleProvider(sts.NewFromConfig(base), webIdentity.RoleARN, retriever,
			webIdentityRoleOptions(webIdentity))
		opts = append(opts, config.WithCredentialsProvider(aws.NewCredentialsCache(provider)))
	default:
		return aws.Config{}, errors.Errorf("unsupported identity source: %s", source)
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, errors.Wrap(err, "failed to create AWS config")
	}

	// assumeRoleChain: assume each role in order, each using the previous step's credentials.
	for _, r := range chain {
		prev := cfg
		role := r
		cfg.Credentials = aws.NewCredentialsCache(stscreds.NewAssumeRoleProvider(sts.NewFromConfig(prev), role.RoleARN,
			assumeRoleOptions(role)))
	}
	return cfg, nil
}

// webIdentityRoleOptions sets the session name: the configured one, or a fixed
// default. The SDK's default is time-based, and the assumed-role ARN in AWS
// errors includes it, so condition messages would change on every run.
func webIdentityRoleOptions(wi *v1beta1.WebIdentity) func(*stscreds.WebIdentityRoleOptions) {
	return func(o *stscreds.WebIdentityRoleOptions) {
		o.RoleSessionName = defaultRoleSessionName
		if wi.RoleSessionName != "" {
			o.RoleSessionName = wi.RoleSessionName
		}
	}
}

// assumeRoleOptions configures one assumeRoleChain hop, with the same session
// name default as webIdentityRoleOptions.
func assumeRoleOptions(role v1beta1.AssumeRole) func(*stscreds.AssumeRoleOptions) {
	return func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = defaultRoleSessionName
		if role.RoleSessionName != nil && *role.RoleSessionName != "" {
			o.RoleSessionName = *role.RoleSessionName
		}
		if role.ExternalID != nil {
			o.ExternalID = role.ExternalID
		}
		o.Tags = toSTSTags(role.Tags)
		o.TransitiveTagKeys = role.TransitiveTagKeys
	}
}

// parseStaticCredentials reads static credentials from the "aws-creds" block:
// direct access-key-id/secret-access-key keys first, then a shared-credentials
// INI under key "credentials".
func parseStaticCredentials(creds map[string][]byte) (map[string]string, error) {
	if c, err := parseDirectCredentials(creds); err == nil {
		return c, nil
	}
	if c, err := parseCredentialsFile(creds); err == nil {
		return c, nil
	}
	return nil, errors.New("neither direct credentials nor credentials file found")
}

// staticIdentityToken is an in-memory stscreds.IdentityTokenRetriever backed by
// token bytes delivered through the function credentials block.
type staticIdentityToken []byte

// GetIdentityToken returns the token bytes.
func (t staticIdentityToken) GetIdentityToken() ([]byte, error) { return t, nil }

func webIdentityTokenRetriever(tokenSecret []byte, wi *v1beta1.WebIdentity) (stscreds.IdentityTokenRetriever, error) {
	source := "Secret"
	if wi.TokenConfig != nil && wi.TokenConfig.Source != "" {
		source = wi.TokenConfig.Source
	}
	switch source {
	case "Secret":
		if len(tokenSecret) == 0 {
			return nil, errors.New("WebIdentity token is empty (expected key \"token\" in the aws-creds credentials block)")
		}
		return staticIdentityToken(tokenSecret), nil
	case "Filesystem":
		if wi.TokenConfig == nil || wi.TokenConfig.FSPath == nil || *wi.TokenConfig.FSPath == "" {
			return nil, errors.New("identity.webIdentity.tokenConfig.fsPath is required for the Filesystem token source")
		}
		return stscreds.IdentityTokenFile(*wi.TokenConfig.FSPath), nil
	}
	return nil, errors.Errorf("unsupported tokenConfig.source: %s", source)
}

func toSTSTags(tags map[string]string) []ststypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]ststypes.Tag, 0, len(tags))
	for k, v := range tags {
		out = append(out, ststypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return out
}
