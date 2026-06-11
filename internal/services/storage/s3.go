package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Storage struct {
	Region string
	Bucket string
	Client *s3.Client

	PresignClient *s3.PresignClient
}

func NewS3Storage(region, bucket, accessKey, secretKey string) (*S3Storage, error) {
	cfg := aws.Config{
		Region:      region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	}
	client := s3.NewFromConfig(cfg)
	presignClient := s3.NewPresignClient(client)

	return &S3Storage{
		Region:        region,
		Bucket:        bucket,
		Client:        client,
		PresignClient: presignClient,
	}, nil
}

func (s *S3Storage) DownloadFile(ctx context.Context, s3URL string, destinationPath string) error {
	resp, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.Bucket,
		Key:    &s3URL,
	})

	if err != nil {
		slog.Error("failed to download file from S3", "from", "s3", "operation", "DownloadFile", "bucket", s.Bucket, "key", s3URL, "error", err)
		return err
	}
	defer resp.Body.Close()

	outFile, err := os.Create(destinationPath)
	if err != nil {
		slog.Error("failed to create destination file", "from", "s3", "operation", "DownloadFile", "bucket", s.Bucket, "key", s3URL, "error", err)
		return err
	}
	defer outFile.Close()

	// Write the S3 object data to the file
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		slog.Error("failed to write S3 object to file", "from", "s3", "operation", "DownloadFile", "bucket", s.Bucket, "key", s3URL, "error", err)
		return err
	}
	return nil
}

func (s *S3Storage) UploadFile(ctx context.Context, s3URL string, sourcePath string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		slog.Error("failed to open source file for upload", "from", "s3", "operation", "UploadFile", "bucket", s.Bucket, "key", s3URL, "error", err)
		return err
	}
	defer file.Close()

	_, err = s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.Bucket,
		Key:    &s3URL,
		Body:   file,
	})
	if err != nil {
		slog.Error("failed to upload file to S3", "from", "s3", "operation", "UploadFile", "bucket", s.Bucket, "key", s3URL, "error", err)
		return err
	}
	return nil
}

func (s *S3Storage) ReadObjectBytes(ctx context.Context, key string) ([]byte, error) {
	resp, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.Bucket,
		Key:    &key,
	})
	if err != nil {
		slog.Error("failed to get object from S3", "from", "s3", "operation", "ReadObjectBytes", "bucket", s.Bucket, "key", key, "error", err)
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("failed to read object body", "from", "s3", "operation", "ReadObjectBytes", "bucket", s.Bucket, "key", key, "error", err)
		return nil, err
	}
	return data, nil
}

func (s *S3Storage) UploadFileByte(ctx context.Context, s3URL string, data []byte) error {
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.Bucket,
		Key:    &s3URL,
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		slog.Error("failed to upload bytes to S3", "from", "s3", "operation", "UploadFileByte", "bucket", s.Bucket, "key", s3URL, "error", err)
		return err
	}
	return nil
}

func (s S3Storage) GetObject(
	ctx context.Context, objectKey string, lifetimeSecs int64) (*v4.PresignedHTTPRequest, error) {
	if lifetimeSecs <= 0 {
		lifetimeSecs = 3600 // default 1 hour
	}
	if lifetimeSecs > 604800 { // 7 days max
		lifetimeSecs = 604800
	}
	request, err := s.PresignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(objectKey),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(lifetimeSecs * int64(time.Second))
	})
	if err != nil {
		slog.Error("failed to create presigned GET request", "from", "s3", "bucket", s.Bucket, "key", objectKey, "error", err)
	}
	return request, err
}

func (s S3Storage) PutObject(
	ctx context.Context, objectKey string, lifetimeSecs int64) (*v4.PresignedHTTPRequest, error) {
	if lifetimeSecs <= 0 {
		lifetimeSecs = 3600 // default 1 hour
	}
	if lifetimeSecs > 604800 { // 7 days max
		lifetimeSecs = 604800
	}
	request, err := s.PresignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(objectKey),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(lifetimeSecs * int64(time.Second))
	})
	if err != nil {
		slog.Error("failed to create presigned PUT request", "from", "s3", "bucket", s.Bucket, "key", objectKey, "error", err)
	}
	return request, err
}

func (s S3Storage) DeleteObject(ctx context.Context, objectKey string) (*v4.PresignedHTTPRequest, error) {
	request, err := s.PresignClient.PresignDeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		slog.Error("failed to create presigned DELETE request", "from", "s3", "bucket", s.Bucket, "key", objectKey, "error", err)
	}
	return request, err
}

func (s *S3Storage) DeleteObjectDirect(ctx context.Context, objectKey string) error {
	_, err := s.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		slog.Error("failed to delete object from S3", "from", "s3", "operation", "DeleteObjectDirect", "bucket", s.Bucket, "key", objectKey, "error", err)
	}
	return err
}

func (s *S3Storage) UploadFileByteWithContentType(ctx context.Context, s3URL string, data []byte, contentType string) error {
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.Bucket,
		Key:         &s3URL,
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		slog.Error("failed to upload bytes with content type to S3", "from", "s3", "operation", "UploadFileByteWithContentType", "bucket", s.Bucket, "key", s3URL, "error", err)
	}
	return err
}

// UploadFileBytePublic uploads data and returns the permanent direct URL.
// Public access is controlled by the bucket policy, not per-object ACLs
// (S3 buckets with "BucketOwnerEnforced" object ownership disallow ACLs).
func (s *S3Storage) UploadFileBytePublic(ctx context.Context, key string, data []byte, contentType string) (string, error) {
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.Bucket,
		Key:         &key,
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		slog.Error("failed to upload public file to S3", "from", "s3", "operation", "UploadFileBytePublic", "bucket", s.Bucket, "key", key, "error", err)
		return "", err
	}
	publicURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.Bucket, s.Region, key)
	return publicURL, nil
}

// PutBytes stores raw bytes at key with the given content type.
// Satisfies credStoreS3 — thin wrapper over UploadFileByteWithContentType.
func (s *S3Storage) PutBytes(ctx context.Context, key string, body []byte, contentType string) error {
	return s.UploadFileByteWithContentType(ctx, key, body, contentType)
}

// GetBytes retrieves raw bytes at key.
// Satisfies credStoreS3 — thin wrapper over ReadObjectBytes.
func (s *S3Storage) GetBytes(ctx context.Context, key string) ([]byte, error) {
	return s.ReadObjectBytes(ctx, key)
}

// DeleteBytes removes the object at key.
// Satisfies credStoreS3 — thin wrapper over DeleteObjectDirect.
func (s *S3Storage) DeleteBytes(ctx context.Context, key string) error {
	return s.DeleteObjectDirect(ctx, key)
}

func (s S3Storage) PresignPostObject(ctx context.Context, objectKey string, lifetimeSecs int64) (*s3.PresignedPostRequest, error) {
	if lifetimeSecs <= 0 {
		lifetimeSecs = 3600 // default 1 hour
	}
	if lifetimeSecs > 604800 { // 7 days max
		lifetimeSecs = 604800
	}
	request, err := s.PresignClient.PresignPostObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(objectKey),
	}, func(options *s3.PresignPostOptions) {
		options.Expires = time.Duration(lifetimeSecs) * time.Second
	})
	if err != nil {
		slog.Error("failed to create presigned POST request", "from", "s3", "bucket", s.Bucket, "key", objectKey, "error", err)
	}
	return request, err
}
