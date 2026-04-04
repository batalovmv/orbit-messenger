package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// CompletedPart represents a finished multipart upload part.
type CompletedPart struct {
	Number int
	ETag   string
}

// R2Client wraps AWS S3 SDK for Cloudflare R2 / MinIO.
type R2Client struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
	publicURL string
}

// NewR2Client creates an S3-compatible client for R2 or MinIO.
// endpoint is the internal S3 API URL (e.g. http://minio:9000).
// publicEndpoint is the browser-accessible URL (e.g. http://localhost:9000).
// If publicEndpoint is empty, presigned URLs use the internal endpoint.
func NewR2Client(endpoint, accessKey, secretKey, bucket, publicEndpoint string) (*R2Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		awsconfig.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true // required for MinIO
	})

	// Create a separate presigner that generates browser-accessible URLs.
	// For MinIO in Docker: internal endpoint is http://minio:9000 but
	// browser needs http://localhost:9000.
	presignEndpoint := endpoint
	if publicEndpoint != "" {
		presignEndpoint = publicEndpoint
	}
	presignClient := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(presignEndpoint)
		o.UsePathStyle = true
	})

	return &R2Client{
		client:    client,
		presigner: s3.NewPresignClient(presignClient),
		bucket:    bucket,
		publicURL: publicEndpoint,
	}, nil
}

// Upload stores a file in R2.
func (r *R2Client) Upload(ctx context.Context, key string, body io.Reader, contentType string, size int64) error {
	input := &s3.PutObjectInput{
		Bucket:        &r.bucket,
		Key:           &key,
		Body:          body,
		ContentType:   &contentType,
		ContentLength: &size,
	}
	_, err := r.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("r2 upload %s: %w", key, err)
	}
	return nil
}

// Delete removes a file from R2.
func (r *R2Client) Delete(ctx context.Context, key string) error {
	_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &r.bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("r2 delete %s: %w", key, err)
	}
	return nil
}

// GetObject returns the object body and content type from S3.
func (r *R2Client) GetObject(ctx context.Context, key string) (io.ReadCloser, string, error) {
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &r.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, "", fmt.Errorf("r2 get %s: %w", key, err)
	}
	contentType := "application/octet-stream"
	if out.ContentType != nil {
		contentType = *out.ContentType
	}
	return out.Body, contentType, nil
}

// RangeResult holds the response from a ranged S3 GET.
type RangeResult struct {
	Body         io.ReadCloser
	ContentType  string
	PartSize     int64
	ContentRange string // e.g. "bytes 0-1023/4096"
}

// GetObjectRange fetches a byte range of a file from R2.
// rangeHeader should be in HTTP Range format, e.g. "bytes=0-1023".
func (r *R2Client) GetObjectRange(ctx context.Context, key, rangeHeader string) (*RangeResult, error) {
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &r.bucket,
		Key:    &key,
		Range:  &rangeHeader,
	})
	if err != nil {
		return nil, fmt.Errorf("r2 get range %s: %w", key, err)
	}
	contentType := "application/octet-stream"
	if out.ContentType != nil {
		contentType = *out.ContentType
	}
	partSize := int64(0)
	if out.ContentLength != nil {
		partSize = *out.ContentLength
	}
	contentRange := ""
	if out.ContentRange != nil {
		contentRange = *out.ContentRange
	}
	return &RangeResult{
		Body:         out.Body,
		ContentType:  contentType,
		PartSize:     partSize,
		ContentRange: contentRange,
	}, nil
}

// PresignedGetURL generates a temporary download URL.
func (r *R2Client) PresignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	out, err := r.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &r.bucket,
		Key:    &key,
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("r2 presign %s: %w", key, err)
	}
	return out.URL, nil
}

// InitMultipartUpload starts a multipart upload and returns the upload ID.
func (r *R2Client) InitMultipartUpload(ctx context.Context, key, contentType string) (string, error) {
	out, err := r.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      &r.bucket,
		Key:         &key,
		ContentType: &contentType,
	})
	if err != nil {
		return "", fmt.Errorf("r2 init multipart %s: %w", key, err)
	}
	return *out.UploadId, nil
}

// UploadPart uploads a single part in a multipart upload.
func (r *R2Client) UploadPart(ctx context.Context, key, uploadID string, partNum int, body io.Reader, size int64) (string, error) {
	partNumber := int32(partNum)
	out, err := r.client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:        &r.bucket,
		Key:           &key,
		UploadId:      &uploadID,
		PartNumber:    &partNumber,
		Body:          body,
		ContentLength: &size,
	})
	if err != nil {
		return "", fmt.Errorf("r2 upload part %d for %s: %w", partNum, key, err)
	}
	return *out.ETag, nil
}

// CompleteMultipartUpload finishes a multipart upload.
func (r *R2Client) CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []CompletedPart) error {
	var s3Parts []types.CompletedPart
	for _, p := range parts {
		num := int32(p.Number)
		etag := p.ETag
		s3Parts = append(s3Parts, types.CompletedPart{
			PartNumber: &num,
			ETag:       &etag,
		})
	}

	_, err := r.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   &r.bucket,
		Key:      &key,
		UploadId: &uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: s3Parts,
		},
	})
	if err != nil {
		return fmt.Errorf("r2 complete multipart %s: %w", key, err)
	}
	return nil
}

// AbortMultipartUpload cancels a multipart upload.
func (r *R2Client) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	_, err := r.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   &r.bucket,
		Key:      &key,
		UploadId: &uploadID,
	})
	if err != nil {
		return fmt.Errorf("r2 abort multipart %s: %w", key, err)
	}
	return nil
}

// EnsureBucket creates the bucket if it doesn't exist (for local dev with MinIO)
// and sets a public-read policy so browser can fetch sticker/media assets directly.
func (r *R2Client) EnsureBucket(ctx context.Context) error {
	_, err := r.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &r.bucket})
	if err != nil {
		_, err = r.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &r.bucket})
		if err != nil {
			return fmt.Errorf("create bucket %s: %w", r.bucket, err)
		}
	}

	// Set public-read policy for dev (MinIO). In production R2 handles this via dashboard.
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, r.bucket)
	_, _ = r.client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: &r.bucket,
		Policy: &policy,
	})

	return nil
}
