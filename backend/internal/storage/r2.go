package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ReplayStore defines the contract for generating presigned URLs for session replay chunks.
type ReplayStore interface {
	GeneratePresignedPut(ctx context.Context, objectKey string, sizeBytes int64, expires time.Duration) (string, error)
	GeneratePresignedGet(ctx context.Context, objectKey string, expires time.Duration) (string, error)
	PutObject(ctx context.Context, objectKey string, data io.Reader, sizeBytes int64) error
	Bucket() string
}

// Config holds the parameters required to initialize S3 / Cloudflare R2 storage.
type Config struct {
	AccountID       string
	AccessKeyID     string
	AccessKeySecret string
	BucketName      string
	Endpoint        string
	Region          string
}

// R2Storage implements ReplayStore for Cloudflare R2 and S3-compatible object storages.
type R2Storage struct {
	client        *s3.Client
	presignClient *s3.PresignClient
	bucket        string
}

// NewR2 creates an R2Storage instance from configuration.
func NewR2(cfg Config) (*R2Storage, error) {
	if cfg.BucketName == "" {
		return nil, errors.New("storage: bucket name cannot be empty")
	}
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, errors.New("storage: access key id and secret are required")
	}

	endpoint := cfg.Endpoint
	if endpoint == "" && cfg.AccountID != "" {
		endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)
	}
	if endpoint == "" {
		return nil, errors.New("storage: endpoint or account id is required")
	}

	region := cfg.Region
	if region == "" {
		region = "auto"
	}

	usePathStyle := strings.Contains(endpoint, "localhost") || strings.Contains(endpoint, "127.0.0.1")

	s3Options := s3.Options{
		Region:                       region,
		Credentials:                  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret, ""),
		BaseEndpoint:                 aws.String(endpoint),
		UsePathStyle:                 usePathStyle,
		RequestChecksumCalculation:   aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation:  aws.ResponseChecksumValidationWhenRequired,
	}

	client := s3.New(s3Options)
	presignClient := s3.NewPresignClient(client)

	return &R2Storage{
		client:        client,
		presignClient: presignClient,
		bucket:        cfg.BucketName,
	}, nil
}

// Bucket returns the configured bucket name.
func (s *R2Storage) Bucket() string {
	return s.bucket
}

// GeneratePresignedPut generates a presigned PUT URL locked to Content-Type: application/gzip and Content-Length.
func (s *R2Storage) GeneratePresignedPut(ctx context.Context, objectKey string, sizeBytes int64, expires time.Duration) (string, error) {
	if objectKey == "" {
		return "", errors.New("storage: object key cannot be empty")
	}
	if expires <= 0 {
		expires = 5 * time.Minute
	}

	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(objectKey),
		ContentType: aws.String("application/gzip"),
	}
	if sizeBytes > 0 {
		input.ContentLength = aws.Int64(sizeBytes)
	}

	req, err := s.presignClient.PresignPutObject(ctx, input, s3.WithPresignExpires(expires))
	if err != nil {
		return "", fmt.Errorf("storage: presign put object: %w", err)
	}

	return req.URL, nil
}


// GeneratePresignedGet generates a presigned GET URL for downloading a replay chunk.
func (s *R2Storage) GeneratePresignedGet(ctx context.Context, objectKey string, expires time.Duration) (string, error) {
	if objectKey == "" {
		return "", errors.New("storage: object key cannot be empty")
	}
	if expires <= 0 {
		expires = 15 * time.Minute
	}

	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	}

	req, err := s.presignClient.PresignGetObject(ctx, input, s3.WithPresignExpires(expires))
	if err != nil {
		return "", fmt.Errorf("storage: presign get object: %w", err)
	}

	return req.URL, nil
}

// PutObject uploads an object directly to Cloudflare R2 bucket.
func (s *R2Storage) PutObject(ctx context.Context, objectKey string, data io.Reader, sizeBytes int64) error {
	if objectKey == "" {
		return errors.New("storage: object key cannot be empty")
	}

	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(objectKey),
		Body:        data,
		ContentType: aws.String("application/gzip"),
	}
	if sizeBytes > 0 {
		input.ContentLength = aws.Int64(sizeBytes)
	}

	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("storage: put object: %w", err)
	}

	return nil
}

