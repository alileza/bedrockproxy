package auth

import (
	"fmt"
	"net/http"
	"strings"
)

// CallerIdentity represents the identity extracted from a SigV4 Authorization header.
type CallerIdentity struct {
	AccessKeyID   string
	Region        string
	Service       string
	SecurityToken string // X-Amz-Security-Token (present for STS temporary credentials)
}

// IsTemporary returns true if the credentials are STS temporary (IRSA, AssumeRole, etc.)
func (c *CallerIdentity) IsTemporary() bool {
	return strings.HasPrefix(c.AccessKeyID, "ASIA")
}

// ParseSigV4 extracts caller identity from the AWS SigV4 Authorization header.
//
// Header format:
//
//	AWS4-HMAC-SHA256 Credential=AKID/20260327/eu-central-1/bedrock/aws4_request, SignedHeaders=..., Signature=...
func ParseSigV4(r *http.Request) (*CallerIdentity, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return nil, fmt.Errorf("missing Authorization header")
	}

	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		return nil, fmt.Errorf("unsupported auth scheme: expected AWS4-HMAC-SHA256")
	}

	// Extract Credential component
	parts := strings.SplitN(auth, "Credential=", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("missing Credential in Authorization header")
	}

	credEnd := strings.Index(parts[1], ",")
	if credEnd == -1 {
		return nil, fmt.Errorf("malformed Credential in Authorization header")
	}
	credential := parts[1][:credEnd]

	// Credential format: AKID/date/region/service/aws4_request
	credParts := strings.Split(credential, "/")
	if len(credParts) != 5 {
		return nil, fmt.Errorf("invalid Credential format: expected 5 parts, got %d", len(credParts))
	}

	return &CallerIdentity{
		AccessKeyID:   credParts[0],
		Region:        credParts[2],
		Service:       credParts[3],
		SecurityToken: r.Header.Get("X-Amz-Security-Token"),
	}, nil
}
