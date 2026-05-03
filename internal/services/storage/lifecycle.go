package storage

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// lifecycleClient is the minimal subset of the S3 client we need.
// Defined as an interface so tests can use a fake.
type lifecycleClient interface {
	PutBucketLifecycleConfiguration(ctx context.Context, in *s3.PutBucketLifecycleConfigurationInput, opts ...func(*s3.Options)) (*s3.PutBucketLifecycleConfigurationOutput, error)
}

// EnsureKeystoreBundlesLifecycle installs (or overwrites) a 1-day expiration
// rule on the keystore-bundles/ prefix of the given bucket. RedeemKeystoreDownload
// uploads ephemeral zips there and presigns a 5-minute URL; without this rule the
// objects accumulate indefinitely.
//
// Idempotent — PutBucketLifecycleConfiguration overwrites the bucket's rules,
// so re-running with the same input is a no-op.
//
// Failure-soft: callers should log + continue if this returns an error. The
// happy path of keystore download still works without the lifecycle rule;
// only the cleanup is deferred.
func EnsureKeystoreBundlesLifecycle(ctx context.Context, client lifecycleClient, bucket string) error {
	rule := s3Types.LifecycleRule{
		ID:     aws.String("wemob-keystore-bundles-1d-expire"),
		Status: s3Types.ExpirationStatusEnabled,
		Filter: &s3Types.LifecycleRuleFilter{
			Prefix: aws.String("keystore-bundles/"),
		},
		Expiration: &s3Types.LifecycleExpiration{
			Days: aws.Int32(1),
		},
	}
	_, err := client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
		LifecycleConfiguration: &s3Types.BucketLifecycleConfiguration{
			Rules: []s3Types.LifecycleRule{rule},
		},
	})
	if err != nil {
		return fmt.Errorf("put bucket lifecycle: %w", err)
	}
	return nil
}
