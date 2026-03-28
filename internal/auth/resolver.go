package auth

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Resolver resolves access key IDs to IAM ARNs using STS and caches results in the database.
type Resolver struct {
	sts  *sts.Client
	pool *pgxpool.Pool
	mu   sync.Mutex
	seen map[string]bool
}

func NewResolver(ctx context.Context, region string, pool *pgxpool.Pool) (*Resolver, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config for resolver: %w", err)
	}

	return &Resolver{
		sts:  sts.NewFromConfig(cfg),
		pool: pool,
		seen: make(map[string]bool),
	}, nil
}

// Resolve looks up the account for an access key and updates the callers table.
// If the same account has a previously registered role_arn, it inherits it.
func (r *Resolver) Resolve(ctx context.Context, accessKeyID string) {
	r.mu.Lock()
	if r.seen[accessKeyID] {
		r.mu.Unlock()
		return
	}
	r.seen[accessKeyID] = true
	r.mu.Unlock()

	go r.resolve(context.Background(), accessKeyID)
}

func (r *Resolver) resolve(ctx context.Context, accessKeyID string) {
	// Check if already resolved
	var existingARN *string
	r.pool.QueryRow(ctx, "SELECT role_arn FROM callers WHERE access_key_id = $1", accessKeyID).Scan(&existingARN)
	if existingARN != nil && *existingARN != "" {
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

	// Check if another caller from the same account already has a role_arn
	var inheritedARN *string
	r.pool.QueryRow(ctx, `
		SELECT role_arn FROM callers
		WHERE account_id = $1 AND role_arn IS NOT NULL AND role_arn != ''
		LIMIT 1
	`, accountID).Scan(&inheritedARN)

	if inheritedARN != nil && *inheritedARN != "" {
		// Inherit the role_arn from a sibling key in the same account
		_, err = r.pool.Exec(ctx, `
			UPDATE callers SET account_id = $1, role_arn = $2
			WHERE access_key_id = $3
		`, accountID, *inheritedARN, accessKeyID)
		if err != nil {
			slog.Warn("update caller with inherited ARN failed", "error", err)
		} else {
			slog.Info("resolved caller (inherited)", "access_key_id", accessKeyID, "account_id", accountID, "arn", *inheritedARN)
		}
	} else {
		_, err = r.pool.Exec(ctx, `
			UPDATE callers SET account_id = $1
			WHERE access_key_id = $2 AND (account_id IS NULL OR account_id = '')
		`, accountID, accessKeyID)
		if err != nil {
			slog.Warn("update caller account_id failed", "error", err)
		} else {
			slog.Info("resolved caller", "access_key_id", accessKeyID, "account_id", accountID)
		}
	}
}

// UpdateRoleARN stores the full role ARN for a caller.
// Also propagates to all other callers from the same account.
func (r *Resolver) UpdateRoleARN(ctx context.Context, accessKeyID, roleARN string) {
	go func() {
		ctx := context.Background()

		// Update this caller
		_, err := r.pool.Exec(ctx, `
			UPDATE callers SET role_arn = $1
			WHERE access_key_id = $2
		`, roleARN, accessKeyID)
		if err != nil {
			slog.Warn("update role_arn failed", "error", err)
			return
		}

		// Also get this caller's account_id and propagate to siblings
		var accountID *string
		r.pool.QueryRow(ctx, "SELECT account_id FROM callers WHERE access_key_id = $1", accessKeyID).Scan(&accountID)
		if accountID != nil && *accountID != "" {
			r.pool.Exec(ctx, `
				UPDATE callers SET role_arn = $1
				WHERE account_id = $2 AND (role_arn IS NULL OR role_arn = '')
			`, roleARN, *accountID)
		}
	}()
}
