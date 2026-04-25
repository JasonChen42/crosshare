package organizer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

// Organizer handles filing photos/videos into date-based directories.
type Organizer struct {
	targetDir string
}

// New creates a new Organizer with the given target directory.
func New(targetDir string) *Organizer {
	return &Organizer{targetDir: targetDir}
}

// OrganizeFile moves a file from tempPath into the date-based directory structure.
// It returns the final path where the file was stored.
// dateTaken is the primary date source; if zero, it tries EXIF, then file mod time.
func (o *Organizer) OrganizeFile(tempPath string, originalName string, dateTaken time.Time) (string, error) {
	// Determine the date for directory organization
	fileDate := dateTaken
	if fileDate.IsZero() {
		fileDate = o.extractDateFromEXIF(tempPath)
	}
	if fileDate.IsZero() {
		info, err := os.Stat(tempPath)
		if err == nil {
			fileDate = info.ModTime()
		} else {
			fileDate = time.Now()
		}
	}

	// Create directory structure: YYYY/YYYY-MM-DD/
	yearDir := fileDate.Format("2006")
	dayDir := fileDate.Format("2006-01-02")
	destDir := filepath.Join(o.targetDir, yearDir, dayDir)

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", destDir, err)
	}

	// Determine final filename (handle duplicates)
	finalName := o.uniqueFilename(destDir, originalName)
	finalPath := filepath.Join(destDir, finalName)

	// Try rename first (fast, same filesystem)
	if err := os.Rename(tempPath, finalPath); err != nil {
		// Cross-filesystem: fall back to copy + delete
		if err := copyFile(tempPath, finalPath); err != nil {
			return "", fmt.Errorf("failed to copy file: %w", err)
		}
		os.Remove(tempPath)
	}

	return finalPath, nil
}

// extractDateFromEXIF tries to read the DateTimeOriginal from EXIF metadata.
func (o *Organizer) extractDateFromEXIF(filePath string) time.Time {
	f, err := os.Open(filePath)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()

	x, err := exif.Decode(f)
	if err != nil {
		return time.Time{}
	}

	dt, err := x.DateTime()
	if err != nil {
		return time.Time{}
	}

	return dt
}

// uniqueFilename returns a filename that doesn't conflict with existing files.
// If "photo.jpg" exists, it tries "photo_1.jpg", "photo_2.jpg", etc.
func (o *Organizer) uniqueFilename(dir, name string) string {
	destPath := filepath.Join(dir, name)
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		return name
	}

	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)

	for i := 1; i < 10000; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		candidatePath := filepath.Join(dir, candidate)
		if _, err := os.Stat(candidatePath); os.IsNotExist(err) {
			return candidate
		}
	}

	// Fallback: use timestamp
	return fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext)
}

// copyFile copies src to dst.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return dstFile.Sync()
}
