package organizer

import (
	"errors"
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

	originalName = filepath.Base(originalName)
	if originalName == "" || originalName == "." || originalName == string(filepath.Separator) {
		return "", errors.New("invalid destination filename")
	}

	for i := 0; i < 10000; i++ {
		finalName := filenameCandidate(originalName, i)
		finalPath := filepath.Join(destDir, finalName)
		err := moveFileNoReplace(tempPath, finalPath)
		switch {
		case err == nil:
			return finalPath, nil
		case errors.Is(err, os.ErrExist):
			continue
		default:
			return "", fmt.Errorf("failed to store file: %w", err)
		}
	}

	finalName := filenameCandidate(originalName, int(time.Now().UnixNano()))
	finalPath := filepath.Join(destDir, finalName)
	if err := moveFileNoReplace(tempPath, finalPath); err != nil {
		return "", fmt.Errorf("failed to store file: %w", err)
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

func filenameCandidate(name string, suffix int) string {
	if suffix == 0 {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s_%d%s", base, suffix, ext)
}

// moveFileNoReplace atomically claims dst before installing src so concurrent
// uploads can never overwrite one another.
func moveFileNoReplace(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		_ = os.Remove(src)
		return nil
	} else if errors.Is(err, os.ErrExist) {
		return os.ErrExist
	}

	if err := copyFileExclusive(src, dst); err != nil {
		return err
	}
	_ = os.Remove(src)
	return nil
}

func copyFileExclusive(src, dst string) (resultErr error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	info, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			_ = dstFile.Close()
			_ = os.Remove(dst)
		}
	}()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	if err := dstFile.Sync(); err != nil {
		return err
	}
	return dstFile.Close()
}
