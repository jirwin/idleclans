package web

import (
	"embed"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jirwin/idleclans/pkg/openai"
	"go.uber.org/zap"
)

// Embed the keyimages directory - use all:keyimages to include even if empty
// Add images to pkg/web/keyimages/ with names like: mountain.png, godly.png, etc.
//
//go:embed all:keyimages
var keyImagesFS embed.FS

// KeyReferenceImages holds loaded reference images for key detection
type KeyReferenceImages struct {
	images []openai.ReferenceImage
	logger *zap.Logger
}

// NewKeyReferenceImages creates a new KeyReferenceImages and loads embedded images
func NewKeyReferenceImages(logger *zap.Logger) *KeyReferenceImages {
	k := &KeyReferenceImages{
		images: make([]openai.ReferenceImage, 0),
		logger: logger,
	}

	// Read embedded image files
	entries, err := keyImagesFS.ReadDir("keyimages")
	if err != nil {
		logger.Debug("No embedded key reference images found", zap.Error(err))
		return k
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		ext := strings.ToLower(filepath.Ext(filename))

		// Only process image files
		var mimeType string
		switch ext {
		case ".png":
			mimeType = "image/png"
		case ".jpg", ".jpeg":
			mimeType = "image/jpeg"
		case ".gif":
			mimeType = "image/gif"
		case ".webp":
			mimeType = "image/webp"
		default:
			continue
		}

		// Extract key type from filename (without extension)
		keyType := strings.TrimSuffix(filename, ext)

		// Read the embedded image
		imagePath := "keyimages/" + filename
		imageData, err := keyImagesFS.ReadFile(imagePath)
		if err != nil {
			logger.Warn("Failed to read embedded key reference image",
				zap.String("file", filename),
				zap.Error(err))
			continue
		}

		base64Data := base64.StdEncoding.EncodeToString(imageData)

		k.images = append(k.images, openai.ReferenceImage{
			Label:      keyType,
			Base64Data: base64Data,
			MimeType:   mimeType,
		})

		logger.Info("Loaded embedded key reference image",
			zap.String("keyType", keyType),
			zap.Int("size", len(imageData)))
	}

	if len(k.images) > 0 {
		logger.Info("Key reference images loaded", zap.Int("count", len(k.images)))
	}
	return k
}

// GetImages returns the loaded reference images
func (k *KeyReferenceImages) GetImages() []openai.ReferenceImage {
	return k.images
}

// HasImages returns true if reference images are loaded
func (k *KeyReferenceImages) HasImages() bool {
	return len(k.images) > 0
}

// String returns a summary of loaded images
func (k *KeyReferenceImages) String() string {
	if len(k.images) == 0 {
		return "No key reference images loaded"
	}

	types := make([]string, len(k.images))
	for i, img := range k.images {
		types[i] = img.Label
	}
	return fmt.Sprintf("Key reference images: %s", strings.Join(types, ", "))
}
