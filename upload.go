package main

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

var ErrInvalidAssetSizeLimit = errors.New("maxAssetSize cannot be negative")

type LocalAssetUploader struct {
	tmpDirPath    string
	outputDirPath string

	urlPrefix string

	maxAssetUploadSize int64
}

type localAssetUploaderConfig struct {
	TmpDirPath         string
	OutputDirPath      string
	URLPrefix          string
	MaxAssetUploadSize int64
}

type assetMeta struct {
	Hash     hash.Hash
	MimeInfo *mimetype.MIME

	Path string
}

func NewLocalAssetUploader(
	cfg localAssetUploaderConfig,
) (*LocalAssetUploader, error) {
	if err := checkDirRW(cfg.TmpDirPath); err != nil {
		return nil, fmt.Errorf("check RW on %q: %w", cfg.TmpDirPath, err)
	}
	if err := checkDirRW(cfg.OutputDirPath); err != nil {
		return nil, fmt.Errorf("check RW on %q: %w", cfg.OutputDirPath, err)
	}

	// maxAssetSize = 0 essentially disables uploading
	if cfg.MaxAssetUploadSize < 0 {
		return nil, ErrInvalidAssetSizeLimit
	}

	if !strings.HasPrefix(cfg.URLPrefix, "/") {
		return nil, fmt.Errorf("URL prefix %q must start with /", cfg.URLPrefix)
	}
	if strings.ContainsAny(cfg.URLPrefix, "?#") {
		return nil, fmt.Errorf(
			"URL prefix %q must not contain any query or fragment",
			cfg.URLPrefix,
		)
	}

	return &LocalAssetUploader{
		tmpDirPath:         cfg.TmpDirPath,
		outputDirPath:      cfg.OutputDirPath,
		urlPrefix:          cfg.URLPrefix,
		maxAssetUploadSize: cfg.MaxAssetUploadSize,
	}, nil
}

func (u *LocalAssetUploader) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	outFilePath, err := u.handleAssetUpload(w, r)
	if err != nil {
		u.handleAssetUploadErr(err, w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	err = json.NewEncoder(w).Encode(struct {
		AssetPath string
	}{
		AssetPath: path.Join(
			u.urlPrefix, path.Base(outFilePath),
		),
	})
	if err != nil {
		// log
		return
	}
}

func (u *LocalAssetUploader) handleAssetUploadErr(
	err error,
	w http.ResponseWriter,
) {
	var maxErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxErr):
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
	default:
		http.Error(w, "server error", http.StatusInternalServerError)
	}
}

func (u *LocalAssetUploader) handleAssetUpload(
	w http.ResponseWriter,
	r *http.Request,
) (string, error) {
	assetUploadReader := http.MaxBytesReader(w, r.Body, u.maxAssetUploadSize)
	defer assetUploadReader.Close()

	meta, err := u.writeAssetUploadToDisk(assetUploadReader)
	if err != nil {
		return "", err
	}

	outputPath, err := u.moveAssetToOutputDir(meta)
	if err != nil {
		os.Remove(meta.Path)
		return "", err
	}

	return outputPath, nil
}

func (u *LocalAssetUploader) writeAssetUploadToDisk(
	r io.ReadCloser,
) (assetMeta, error) {
	var err error
	outFile, err := os.CreateTemp(
		u.tmpDirPath,
		"ul-*",
	)
	if err != nil {
		return assetMeta{}, err
	}
	outFilePath := outFile.Name()

	defer func() {
		if err != nil {
			os.Remove(outFilePath)
		}
	}()

	h := sha512.New()
	_, err = io.Copy(
		io.MultiWriter(outFile, h),
		r,
	)
	if err != nil {
		_ = outFile.Close()
		return assetMeta{}, err
	}

	if err = outFile.Close(); err != nil {
		return assetMeta{}, err
	}

	mime, err := mimetype.DetectFile(outFilePath)
	if err != nil {
		return assetMeta{}, err
	}

	meta := assetMeta{
		Hash:     h,
		MimeInfo: mime,
		Path:     outFilePath,
	}

	return meta, nil
}

func (u *LocalAssetUploader) moveAssetToOutputDir(
	m assetMeta,
) (string, error) {
	baseName := hex.EncodeToString(m.Hash.Sum(nil))
	outputName := baseName + m.MimeInfo.Extension()
	outputPath := filepath.Join(
		u.outputDirPath,
		outputName,
	)

	err := os.Rename(m.Path, outputPath)
	if err != nil {
		return "", err
	}

	return outputPath, nil
}
