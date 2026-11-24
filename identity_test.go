package main

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/upbound/function-rds-metrics/input/v1beta1"
)

func TestNeedsCredentialsBlock(t *testing.T) {
	fs := "/var/run/secrets/token"
	cases := map[string]struct {
		in   *v1beta1.Input
		want bool
	}{
		"NoIdentityMeansSecret":    {in: &v1beta1.Input{}, want: true},
		"Secret":                   {in: &v1beta1.Input{Identity: &v1beta1.Identity{Source: v1beta1.IdentitySourceSecret}}, want: true},
		"IRSA":                     {in: &v1beta1.Input{Identity: &v1beta1.Identity{Source: v1beta1.IdentitySourceIRSA}}, want: false},
		"PodIdentity":              {in: &v1beta1.Input{Identity: &v1beta1.Identity{Source: v1beta1.IdentitySourcePodIdentity}}, want: false},
		"WebIdentityWithoutConfig": {in: &v1beta1.Input{Identity: &v1beta1.Identity{Source: v1beta1.IdentitySourceWebIdentity}}, want: false},
		"WebIdentitySecretToken": {in: &v1beta1.Input{Identity: &v1beta1.Identity{
			Source:      v1beta1.IdentitySourceWebIdentity,
			WebIdentity: &v1beta1.WebIdentity{RoleARN: "arn:aws:iam::111:role/x"},
		}}, want: true},
		"WebIdentityTokenFile": {in: &v1beta1.Input{Identity: &v1beta1.Identity{
			Source: v1beta1.IdentitySourceWebIdentity,
			WebIdentity: &v1beta1.WebIdentity{
				RoleARN:     "arn:aws:iam::111:role/x",
				TokenConfig: &v1beta1.TokenConfig{Source: "Filesystem", FSPath: &fs},
			},
		}}, want: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := needsCredentialsBlock(tc.in); got != tc.want {
				t.Errorf("needsCredentialsBlock() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildAWSConfig(t *testing.T) {
	// Don't let the developer's ~/.aws or AWS_* variables change the results.
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	direct := map[string][]byte{"access-key-id": []byte("AK"), "secret-access-key": []byte("SK"), "session-token": []byte("ST")}
	iniCreds := map[string][]byte{"credentials": []byte("[default]\naws_access_key_id = IAK\naws_secret_access_key = ISK\n")}

	t.Run("SecretDirectKeysAndDefaultRegion", func(t *testing.T) {
		cfg, err := buildAWSConfig(context.Background(), direct, &v1beta1.Input{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Region != defaultRegion {
			t.Errorf("region = %q, want %q", cfg.Region, defaultRegion)
		}
		got, err := cfg.Credentials.Retrieve(context.Background())
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if got.AccessKeyID != "AK" || got.SecretAccessKey != "SK" || got.SessionToken != "ST" {
			t.Errorf("creds = %+v, want AK/SK/ST", got)
		}
	})

	t.Run("SecretCredentialsFileAndRegion", func(t *testing.T) {
		cfg, err := buildAWSConfig(context.Background(), iniCreds, &v1beta1.Input{Region: "eu-central-1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Region != "eu-central-1" {
			t.Errorf("region = %q, want eu-central-1", cfg.Region)
		}
		got, err := cfg.Credentials.Retrieve(context.Background())
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if got.AccessKeyID != "IAK" || got.SecretAccessKey != "ISK" {
			t.Errorf("creds = %+v, want IAK/ISK", got)
		}
	})

	t.Run("SecretEmptyErrors", func(t *testing.T) {
		if _, err := buildAWSConfig(context.Background(), map[string][]byte{}, &v1beta1.Input{}); err == nil {
			t.Error("expected error for missing credentials")
		}
	})

	t.Run("WebIdentityRequiresRoleARN", func(t *testing.T) {
		in := &v1beta1.Input{Identity: &v1beta1.Identity{Source: v1beta1.IdentitySourceWebIdentity}}
		if _, err := buildAWSConfig(context.Background(), map[string][]byte{}, in); err == nil {
			t.Error("expected error for missing roleARN")
		}
	})

	t.Run("WebIdentityFilesystemRequiresPath", func(t *testing.T) {
		in := &v1beta1.Input{Identity: &v1beta1.Identity{
			Source: v1beta1.IdentitySourceWebIdentity,
			WebIdentity: &v1beta1.WebIdentity{
				RoleARN:     "arn:aws:iam::111:role/x",
				TokenConfig: &v1beta1.TokenConfig{Source: "Filesystem"},
			},
		}}
		if _, err := buildAWSConfig(context.Background(), map[string][]byte{}, in); err == nil {
			t.Error("expected error for missing fsPath")
		}
	})

	t.Run("WebIdentityTokenFileWires", func(t *testing.T) {
		fs := "/var/run/secrets/upbound.io/provider/token"
		in := &v1beta1.Input{Identity: &v1beta1.Identity{
			Source: v1beta1.IdentitySourceWebIdentity,
			WebIdentity: &v1beta1.WebIdentity{
				RoleARN:     "arn:aws:iam::111:role/x",
				TokenConfig: &v1beta1.TokenConfig{Source: "Filesystem", FSPath: &fs},
			},
		}}
		cfg, err := buildAWSConfig(context.Background(), map[string][]byte{}, in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !aws.IsCredentialsProvider(cfg.Credentials, (*stscreds.WebIdentityRoleProvider)(nil)) {
			t.Errorf("credentials provider = %T, want a web identity role provider", cfg.Credentials)
		}
	})

	t.Run("UnsupportedSource", func(t *testing.T) {
		in := &v1beta1.Input{Identity: &v1beta1.Identity{Source: "Bogus"}}
		if _, err := buildAWSConfig(context.Background(), map[string][]byte{}, in); err == nil {
			t.Error("expected error for unsupported source")
		}
	})

	t.Run("IRSANoSecret", func(t *testing.T) {
		in := &v1beta1.Input{Identity: &v1beta1.Identity{Source: v1beta1.IdentitySourceIRSA}}
		cfg, err := buildAWSConfig(context.Background(), map[string][]byte{}, in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Region != defaultRegion {
			t.Errorf("region = %q, want %q", cfg.Region, defaultRegion)
		}
	})

	t.Run("AssumeRoleChainWires", func(t *testing.T) {
		in := &v1beta1.Input{Identity: &v1beta1.Identity{
			Source:          v1beta1.IdentitySourceSecret,
			AssumeRoleChain: []v1beta1.AssumeRole{{RoleARN: "arn:aws:iam::222:role/x"}},
		}}
		cfg, err := buildAWSConfig(context.Background(), direct, in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !aws.IsCredentialsProvider(cfg.Credentials, (*stscreds.AssumeRoleProvider)(nil)) {
			t.Errorf("credentials provider = %T, want an assume-role provider", cfg.Credentials)
		}
	})
}

func TestWebIdentityTokenRetriever(t *testing.T) {
	wi := &v1beta1.WebIdentity{RoleARN: "arn:aws:iam::111:role/x"}

	t.Run("SecretToken", func(t *testing.T) {
		r, err := webIdentityTokenRetriever([]byte("tok"), wi)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := r.GetIdentityToken()
		if err != nil || string(got) != "tok" {
			t.Errorf("token = %q, err = %v, want tok", got, err)
		}
	})

	t.Run("SecretTokenMissing", func(t *testing.T) {
		if _, err := webIdentityTokenRetriever(nil, wi); err == nil {
			t.Error("expected error for an empty token")
		}
	})

	t.Run("UnsupportedTokenSource", func(t *testing.T) {
		bad := &v1beta1.WebIdentity{RoleARN: "arn:aws:iam::111:role/x", TokenConfig: &v1beta1.TokenConfig{Source: "Bogus"}}
		if _, err := webIdentityTokenRetriever(nil, bad); err == nil {
			t.Error("expected error for an unsupported token source")
		}
	})
}

func TestRoleSessionName(t *testing.T) {
	custom := "my-session"
	webIdentity := func(wi *v1beta1.WebIdentity) string {
		var o stscreds.WebIdentityRoleOptions
		webIdentityRoleOptions(wi)(&o)
		return o.RoleSessionName
	}
	assumeRole := func(r v1beta1.AssumeRole) string {
		var o stscreds.AssumeRoleOptions
		assumeRoleOptions(r)(&o)
		return o.RoleSessionName
	}
	cases := map[string]struct{ got, want string }{
		"WebIdentityDefault": {got: webIdentity(&v1beta1.WebIdentity{}), want: defaultRoleSessionName},
		"WebIdentityCustom":  {got: webIdentity(&v1beta1.WebIdentity{RoleSessionName: custom}), want: custom},
		"AssumeRoleDefault":  {got: assumeRole(v1beta1.AssumeRole{}), want: defaultRoleSessionName},
		"AssumeRoleCustom":   {got: assumeRole(v1beta1.AssumeRole{RoleSessionName: &custom}), want: custom},
	}
	for name, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: RoleSessionName = %q, want %q", name, tc.got, tc.want)
		}
	}
}
