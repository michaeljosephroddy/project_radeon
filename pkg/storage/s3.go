package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Uploader struct {
	client *s3.Client
	bucket string
	region string
}

// NewS3Uploader builds the S3-backed uploader used for avatar storage.
func NewS3Uploader(client *s3.Client, bucket, region string) *S3Uploader {
	return &S3Uploader{client: client, bucket: bucket, region: region}
}

// Upload writes the provided content to S3 and returns the object's public URL.
// The bucket must have a public-read policy for the URL to be accessible.
func (u *S3Uploader) Upload(ctx context.Context, key, contentType string, body io.Reader) (string, error) {
	_, err := u.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(u.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("s3 put object: %w", err)
	}

	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", u.bucket, u.region, key), nil
}
