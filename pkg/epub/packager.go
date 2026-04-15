package epub

import (
	"archive/zip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// PackageEPUB creates an EPUB file at destPath from the buildDir.
// The EPUB specification requires that the mimetype file be the first entry,
// uncompressed, and without extra field headers.
func PackageEPUB(buildDir, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	tmpFile := destPath + ".tmp"
	outFile, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	defer func() {
		outFile.Close()
		os.Remove(tmpFile)
	}()

	zipWriter := zip.NewWriter(outFile)

	// 1. Write uncompressed mimetype as the first entry
	mimetypePath := filepath.Join(buildDir, "mimetype")
	mimetypeData, err := os.ReadFile(mimetypePath)
	if err != nil {
		mimetypeData = []byte("application/epub+zip")
	}

	fh := &zip.FileHeader{
		Name:   "mimetype",
		Method: zip.Store,
	}
	mw, err := zipWriter.CreateHeader(fh)
	if err != nil {
		return err
	}
	if _, err := mw.Write(mimetypeData); err != nil {
		return err
	}

	// 2. Add all other files under META-INF and OEBPS using Deflate
	err = filepath.Walk(buildDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(buildDir, path)
		if err != nil {
			return err
		}

		// Skip mimetype as it's already written
		if relPath == "mimetype" || relPath == ".DS_Store" {
			return nil
		}

		fileHeader, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		fileHeader.Name = filepath.ToSlash(relPath)
		fileHeader.Method = zip.Deflate

		w, err := zipWriter.CreateHeader(fileHeader)
		if err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(w, f)
		return err
	})

	if err != nil {
		return err
	}

	if err := zipWriter.Close(); err != nil {
		return err
	}
	if err := outFile.Close(); err != nil {
		return err
	}

	// Replace existing destination
	_ = os.Remove(destPath)
	return os.Rename(tmpFile, destPath)
}

// FindCalibreBinary locates the ebook-convert command on the user's system.
func FindCalibreBinary() string {
	possible := []string{
		"/opt/homebrew/bin/ebook-convert",
		"/usr/local/bin/ebook-convert",
		"/Applications/calibre.app/Contents/MacOS/ebook-convert",
	}
	for _, p := range possible {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("ebook-convert"); err == nil {
		return p
	}
	return ""
}

// OptimizeEPUBWithCalibre converts and polishes the EPUB using Calibre.
// Calibre handles CSS flattening, font size remapping, fake margin removal,
// and cross-reader compatibility across Apple Books, Kindle, and Kobo.
func OptimizeEPUBWithCalibre(epubPath string) bool {
	calibre := FindCalibreBinary()
	if calibre == "" {
		return false
	}

	tmpOut := epubPath + ".calibre.epub"
	cfgDir := filepath.Join(os.TempDir(), "calibre_cfg")
	_ = os.MkdirAll(cfgDir, 0755)

	cmd := exec.Command(calibre, epubPath, tmpOut, "--epub-version", "3", "--dont-split-on-page-breaks", "--flow-size", "0", "--preserve-cover-aspect-ratio")
	cmd.Env = append(os.Environ(), "CALIBRE_CONFIG_DIRECTORY="+cfgDir)

	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmpOut)
		return false
	}

	fi, err := os.Stat(tmpOut)
	if err == nil && fi.Size() > 1000 {
		_ = os.Remove(epubPath)
		_ = os.Rename(tmpOut, epubPath)
		return true
	}
	_ = os.Remove(tmpOut)
	return false
}
