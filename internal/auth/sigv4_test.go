package auth

import (
	"net/http"
	"testing"
)

func TestParseSigV4(t *testing.T) {
	tests := []struct {
		name          string
		authHeader    string
		securityToken string
		wantErr       string
		wantKey       string
		wantRegion    string
		wantService   string
		wantToken     string
	}{
		{
			name:       "valid SigV4 header",
			authHeader: "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20260327/eu-central-1/bedrock/aws4_request, SignedHeaders=host;x-amz-date, Signature=abc123",
			wantKey:    "AKIAIOSFODNN7EXAMPLE",
			wantRegion: "eu-central-1",
			wantService: "bedrock",
		},
		{
			name:          "valid SigV4 with security token",
			authHeader:    "AWS4-HMAC-SHA256 Credential=ASIAXYZ123456789012/20260327/us-west-2/bedrock/aws4_request, SignedHeaders=host, Signature=def456",
			securityToken: "FwoGZXIvYXdzEBYaDK1234567890example==",
			wantKey:       "ASIAXYZ123456789012",
			wantRegion:    "us-west-2",
			wantService:   "bedrock",
			wantToken:     "FwoGZXIvYXdzEBYaDK1234567890example==",
		},
		{
			name:    "missing Authorization header",
			wantErr: "missing Authorization header",
		},
		{
			name:       "non-SigV4 auth scheme",
			authHeader: "Bearer some-token",
			wantErr:    "unsupported auth scheme",
		},
		{
			name:       "missing Credential field",
			authHeader: "AWS4-HMAC-SHA256 SignedHeaders=host, Signature=abc",
			wantErr:    "missing Credential",
		},
		{
			name:       "malformed Credential without comma",
			authHeader: "AWS4-HMAC-SHA256 Credential=AKID/20260327/eu-central-1/bedrock/aws4_request",
			wantErr:    "malformed Credential",
		},
		{
			name:       "Credential with too few parts",
			authHeader: "AWS4-HMAC-SHA256 Credential=AKID/20260327/eu-central-1, SignedHeaders=host, Signature=abc",
			wantErr:    "invalid Credential format: expected 5 parts, got 3",
		},
		{
			name:       "Credential with too many parts",
			authHeader: "AWS4-HMAC-SHA256 Credential=AKID/20260327/eu-central-1/bedrock/aws4_request/extra, SignedHeaders=host, Signature=abc",
			wantErr:    "invalid Credential format: expected 5 parts, got 6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "http://localhost/model/test/converse", nil)
			if err != nil {
				t.Fatalf("creating request: %v", err)
			}

			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			if tt.securityToken != "" {
				req.Header.Set("X-Amz-Security-Token", tt.securityToken)
			}

			got, err := ParseSigV4(req)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !containsSubstring(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.AccessKeyID != tt.wantKey {
				t.Errorf("AccessKeyID = %q, want %q", got.AccessKeyID, tt.wantKey)
			}
			if got.Region != tt.wantRegion {
				t.Errorf("Region = %q, want %q", got.Region, tt.wantRegion)
			}
			if got.Service != tt.wantService {
				t.Errorf("Service = %q, want %q", got.Service, tt.wantService)
			}
			if got.SecurityToken != tt.wantToken {
				t.Errorf("SecurityToken = %q, want %q", got.SecurityToken, tt.wantToken)
			}
		})
	}
}

func TestCallerIdentity_IsTemporary(t *testing.T) {
	tests := []struct {
		name        string
		accessKeyID string
		want        bool
	}{
		{name: "temporary ASIA key", accessKeyID: "ASIAXYZ123456789012", want: true},
		{name: "permanent AKIA key", accessKeyID: "AKIAIOSFODNN7EXAMPLE", want: false},
		{name: "empty key", accessKeyID: "", want: false},
		{name: "short ASIA prefix only", accessKeyID: "ASIA", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &CallerIdentity{AccessKeyID: tt.accessKeyID}
			if got := c.IsTemporary(); got != tt.want {
				t.Errorf("IsTemporary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && contains(s, substr))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
