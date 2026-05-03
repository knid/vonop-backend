package storage

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type fakeLifecycleClient struct {
	called bool
	bucket string
	rules  int
}

func (f *fakeLifecycleClient) PutBucketLifecycleConfiguration(_ context.Context, in *s3.PutBucketLifecycleConfigurationInput, _ ...func(*s3.Options)) (*s3.PutBucketLifecycleConfigurationOutput, error) {
	f.called = true
	if in.Bucket != nil {
		f.bucket = *in.Bucket
	}
	if in.LifecycleConfiguration != nil {
		f.rules = len(in.LifecycleConfiguration.Rules)
	}
	return &s3.PutBucketLifecycleConfigurationOutput{}, nil
}

func TestEnsureKeystoreBundlesLifecycle_PostsExpectedRule(t *testing.T) {
	f := &fakeLifecycleClient{}
	if err := EnsureKeystoreBundlesLifecycle(context.Background(), f, "wemob-assets"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !f.called {
		t.Fatal("PutBucketLifecycleConfiguration not called")
	}
	if f.bucket != "wemob-assets" {
		t.Errorf("bucket: got %q want wemob-assets", f.bucket)
	}
	if f.rules != 1 {
		t.Errorf("rules: got %d want 1", f.rules)
	}
}
