package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Flusher periodically flushes requests from the store to S3.
type S3Flusher struct {
	store    *Store
	client   *s3.Client
	bucket   string
	prefix   string
	interval time.Duration
}

// NewS3Flusher creates a new S3 flusher. If bucket is empty, flushing is disabled.
func NewS3Flusher(ctx context.Context, store *Store, bucket, prefix string, interval time.Duration, region string) (*S3Flusher, error) {
	if bucket == "" {
		slog.Warn("S3 flusher disabled: no bucket configured")
		return &S3Flusher{store: store, interval: interval}, nil
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config for s3 flusher: %w", err)
	}

	return &S3Flusher{
		store:    store,
		client:   s3.NewFromConfig(cfg),
		bucket:   bucket,
		prefix:   prefix,
		interval: interval,
	}, nil
}

// Start begins the periodic flush loop in a background goroutine.
// It stops when the context is cancelled.
func (f *S3Flusher) Start(ctx context.Context) {
	if f.client == nil {
		slog.Info("S3 flusher not started (no bucket configured)")
		return
	}

	go func() {
		ticker := time.NewTicker(f.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				// Final flush on shutdown
				f.flush(context.Background())
				return
			case <-ticker.C:
				f.flush(ctx)
			}
		}
	}()

	slog.Info("S3 flusher started", "bucket", f.bucket, "prefix", f.prefix, "interval", f.interval)
}

// FlushNow performs an immediate flush. Used during graceful shutdown.
func (f *S3Flusher) FlushNow(ctx context.Context) {
	if f.client == nil {
		return
	}
	f.flush(ctx)
}

func (f *S3Flusher) flush(ctx context.Context) {
	requests := f.store.FlushRequests()
	if len(requests) == 0 {
		return
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)

	for _, r := range requests {
		data, err := json.Marshal(r)
		if err != nil {
			slog.Error("s3 flusher: failed to marshal request", "error", err)
			continue
		}
		gz.Write(data)
		gz.Write([]byte("\n"))
	}

	if err := gz.Close(); err != nil {
		slog.Error("s3 flusher: failed to close gzip writer", "error", err)
		return
	}

	now := time.Now().UTC()
	key := fmt.Sprintf("%s/%04d/%02d/%02d/%02d/%d.json.gz",
		f.prefix,
		now.Year(), now.Month(), now.Day(), now.Hour(),
		now.UnixMilli(),
	)

	_, err := f.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:          aws.String(f.bucket),
		Key:             aws.String(key),
		Body:            bytes.NewReader(buf.Bytes()),
		ContentType:     aws.String("application/gzip"),
		ContentEncoding: aws.String("gzip"),
	})
	if err != nil {
		slog.Error("s3 flusher: failed to upload to S3", "key", key, "error", err)
		return
	}

	slog.Info("s3 flusher: uploaded requests", "key", key, "count", len(requests))
}
