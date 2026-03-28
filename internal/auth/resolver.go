package auth

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"bedrockproxy/internal/store"
)

// Resolver resolves access key IDs to IAM ARNs using STS and caches results in the store.
type Resolver struct {
	sts   *sts.Client
	store *store.Store
	mu    sync.Mutex
	seen  map[string]bool
}

func NewResolver(ctx context.Context, region string, s *store.Store) (*Resolver, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config for resolver: %w", err)
	}

	return &Resolver{
		sts:   sts.NewFromConfig(cfg),
		store: s,
		seen:  make(map[string]bool),
	}, nil
}

// Resolve looks up the account for an access key and updates the store.
// If the same account has a previously registered role_arn, it inherits it.
func (r *Resolver) Resolve(ctx context.Context, accessKeyID string) {
	// Skip if already fully resolved (has ARN)
	if arn := r.store.GetCallerRoleARN(accessKeyID); arn != "" {
		return
	}

	// Skip if STS lookup already done (account resolved, just no ARN yet)
	r.mu.Lock()
	if r.seen[accessKeyID] {
		r.mu.Unlock()
		// Still try to inherit ARN from siblings (may have been registered since last check)
		if acct := r.store.GetCallerAccountID(accessKeyID); acct != "" {
			if inherited := r.store.FindARNByAccount(acct); inherited != "" {
				r.store.UpdateCallerARN(accessKeyID, inherited)
			}
		}
		return
	}
	r.seen[accessKeyID] = true
	r.mu.Unlock()

	go r.resolve(context.Background(), accessKeyID)
}

func (r *Resolver) resolve(ctx context.Context, accessKeyID string) {
	// Check if already resolved
	if arn := r.store.GetCallerRoleARN(accessKeyID); arn != "" {
		return
	}

	out, err := r.sts.GetAccessKeyInfo(ctx, &sts.GetAccessKeyInfoInput{
		AccessKeyId: aws.String(accessKeyID),
	})
	if err != nil {
		slog.Warn("sts:GetAccessKeyInfo failed", "access_key_id", accessKeyID, "error", err)
		return
	}

	accountID := aws.ToString(out.Account)

	r.store.EnsureCaller(accessKeyID)
	r.store.UpdateCallerAccount(accessKeyID, accountID)

	// Check if another caller from the same account already has a role_arn
	inheritedARN := r.store.FindARNByAccount(accountID)
	if inheritedARN != "" {
		r.store.UpdateCallerARN(accessKeyID, inheritedARN)
		slog.Info("resolved caller (inherited)", "access_key_id", accessKeyID, "account_id", accountID, "arn", inheritedARN)
	} else {
		slog.Info("resolved caller", "access_key_id", accessKeyID, "account_id", accountID)
	}
}

// UpdateRoleARN stores the full role ARN for a caller.
// Also propagates to all other callers from the same account.
func (r *Resolver) UpdateRoleARN(_ context.Context, accessKeyID, roleARN string) {
	r.store.UpdateCallerARN(accessKeyID, roleARN)
}
