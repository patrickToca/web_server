package r2

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Client wraps the S3 client configured for Cloudflare R2.
type Client struct {
	s3Client   *s3.Client
	presigner  *s3.PresignClient
	bucketName string
}

// Config holds the R2 connection details.
type Config struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	BucketName      string
}

// NewClient creates an S3 client pointed at Cloudflare R2.
// R2 does not use a specific region, but the AWS SDK requires one;
// "auto" is the conventional placeholder for R2 [citation:2][citation:9].
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		),
		awsconfig.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("r2: load config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})

	return &Client{
		s3Client:   s3Client,
		presigner:  s3.NewPresignClient(s3Client),
		bucketName: cfg.BucketName,
	}, nil
}

// PresignGetObject returns a presigned URL for reading an object.
// The URL is valid for the specified duration and requires no
// additional authentication when used.
func (c *Client) PresignGetObject(ctx context.Context, key string, expiresIn time.Duration) (string, error) {
	req, err := c.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucketName),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiresIn))
	if err != nil {
		return "", fmt.Errorf("r2: presign: %w", err)
	}
	return req.URL, nil
}
