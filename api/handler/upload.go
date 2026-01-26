/**
 * Image upload handler for MinIO storage
 */
package handler

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/sirupsen/logrus"
	"gochat/config"
	"gochat/tools"
)

var minioClient *minio.Client

// InitMinioClient initializes the MinIO client and creates bucket if needed
func InitMinioClient() error {
	cfg := config.Conf.Common.CommonMinIO
	if cfg.Endpoint == "" {
		logrus.Info("MinIO not configured, skipping initialization")
		return nil
	}

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to create MinIO client: %w", err)
	}
	minioClient = client

	// Create bucket if not exists
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.BucketName)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		err = client.MakeBucket(ctx, cfg.BucketName, minio.MakeBucketOptions{})
		if err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
		logrus.Infof("Created MinIO bucket: %s", cfg.BucketName)

		// Set bucket policy to allow public read
		policy := fmt.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Principal": {"AWS": ["*"]},
					"Action": ["s3:GetObject"],
					"Resource": ["arn:aws:s3:::%s/*"]
				}
			]
		}`, cfg.BucketName)
		err = client.SetBucketPolicy(ctx, cfg.BucketName, policy)
		if err != nil {
			logrus.Warnf("Failed to set bucket policy: %v", err)
		}
	}

	logrus.Infof("MinIO client initialized successfully, endpoint: %s, bucket: %s", cfg.Endpoint, cfg.BucketName)
	return nil
}

// allowedImageTypes defines the allowed MIME types for image uploads
var allowedImageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// UploadImage handles image upload to MinIO
func UploadImage(c *gin.Context) {
	if minioClient == nil {
		tools.FailWithMsg(c, "image upload service not available")
		return
	}

	// Get the uploaded file
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		tools.FailWithMsg(c, "no image file provided")
		return
	}
	defer file.Close()

	// Validate file size
	if header.Size > config.MaxImageSizeBytes {
		tools.FailWithMsg(c, fmt.Sprintf("image size exceeds maximum allowed (%d MB)", config.MaxImageSizeBytes/(1024*1024)))
		return
	}

	// Validate content type
	contentType := header.Header.Get("Content-Type")
	ext, ok := allowedImageTypes[contentType]
	if !ok {
		tools.FailWithMsg(c, "invalid image type, allowed: jpeg, png, gif, webp")
		return
	}

	// Generate unique filename with date-based path
	now := time.Now()
	objectName := fmt.Sprintf("%d/%02d/%s%s",
		now.Year(), now.Month(),
		uuid.New().String(), ext)

	// Upload to MinIO
	cfg := config.Conf.Common.CommonMinIO
	ctx := context.Background()
	_, err = minioClient.PutObject(ctx, cfg.BucketName, objectName, file, header.Size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		logrus.Errorf("failed to upload image to MinIO: %v", err)
		tools.FailWithMsg(c, "failed to upload image")
		return
	}

	// Build the public URL
	var imageURL string
	if cfg.UseSSL {
		imageURL = fmt.Sprintf("https://%s/%s/%s", cfg.Endpoint, cfg.BucketName, objectName)
	} else {
		imageURL = fmt.Sprintf("http://%s/%s/%s", cfg.Endpoint, cfg.BucketName, objectName)
	}

	logrus.Infof("Image uploaded successfully: %s", imageURL)
	tools.SuccessWithMsg(c, "ok", map[string]string{
		"imageUrl": imageURL,
	})
}

// isValidImageExtension checks if the file extension is allowed
func isValidImageExtension(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	for _, allowedExt := range allowedImageTypes {
		if ext == allowedExt {
			return true
		}
	}
	return false
}

// getContentTypeFromFilename returns the MIME type based on file extension
func getContentTypeFromFilename(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
