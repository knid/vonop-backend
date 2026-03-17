package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Storage struct {
	Region string
	Bucket string
	Client *s3.Client

	PresignClient *s3.PresignClient
}

func NewS3Storage(region, bucket, accessKey, secretKey string) (*S3Storage, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, err
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
		return err
	}
	defer resp.Body.Close()

	outFile, err := os.Create(destinationPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	// Write the S3 object data to the file
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return err
	}
	return nil
}

func (s *S3Storage) UploadFile(ctx context.Context, s3URL string, sourcePath string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.Bucket,
		Key:    &s3URL,
		Body:   file,
	})
	if err != nil {
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
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func (s *S3Storage) UploadFileByte(ctx context.Context, s3URL string, data []byte) error {
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.Bucket,
		Key:    &s3URL,
		Body:   bytes.NewReader(data),
	})
	if err != nil {
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
		log.Printf("Couldn't get a presigned request to get %v:%v. Here's why: %v\n",
			s.Bucket, objectKey, err)
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
		log.Printf("Couldn't get a presigned request to put %v:%v. Here's why: %v\n",
			s.Bucket, objectKey, err)
	}
	return request, err
}

func (s S3Storage) DeleteObject(ctx context.Context, objectKey string) (*v4.PresignedHTTPRequest, error) {
	request, err := s.PresignClient.PresignDeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		log.Printf("Couldn't get a presigned request to delete object %v. Here's why: %v\n", objectKey, err)
	}
	return request, err
}

func (s *S3Storage) UploadFileByteWithContentType(ctx context.Context, s3URL string, data []byte, contentType string) error {
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.Bucket,
		Key:         &s3URL,
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	return err
}

// UploadFileBytePublic uploads data with public-read ACL and returns the permanent direct URL.
func (s *S3Storage) UploadFileBytePublic(ctx context.Context, key string, data []byte, contentType string) (string, error) {
	acl := types.ObjectCannedACLPublicRead
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.Bucket,
		Key:         &key,
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
		ACL:         acl,
	})
	if err != nil {
		return "", err
	}
	publicURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.Bucket, s.Region, key)
	return publicURL, nil
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
		log.Printf("Couldn't get a presigned post request to put %v:%v. Here's why: %v\n", s.Bucket, objectKey, err)
	}
	return request, err
}
