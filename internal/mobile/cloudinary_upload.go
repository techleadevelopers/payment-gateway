package mobile

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxMobileImageUploadBytes = 12 << 20
	maxMobileVideoUploadBytes = 60 << 20
)

type cloudinaryConfig struct {
	CloudName string
	APIKey    string
	APISecret string
}

type cloudinaryUploadResult struct {
	SecureURL    string `json:"secure_url"`
	PublicID     string `json:"public_id"`
	ResourceType string `json:"resource_type"`
	Format       string `json:"format"`
	Bytes        int64  `json:"bytes"`
}

func loadCloudinaryConfig() (*cloudinaryConfig, error) {
	cfg := &cloudinaryConfig{
		APIKey:    strings.TrimSpace(os.Getenv("CLOUDINARY_API_KEY")),
		APISecret: strings.TrimSpace(os.Getenv("CLOUDINARY_API_SECRET")),
	}

	rawURL := strings.Trim(strings.TrimSpace(os.Getenv("CLOUDINARY_URL")), `"`)
	if rawURL != "" {
		u, err := url.Parse(rawURL)
		if err != nil {
			return nil, fmt.Errorf("CLOUDINARY_URL invalida: %w", err)
		}
		cfg.CloudName = strings.TrimSpace(u.Host)
		if u.User != nil {
			if key := u.User.Username(); key != "" {
				cfg.APIKey = key
			}
			if secret, ok := u.User.Password(); ok && secret != "" {
				cfg.APISecret = secret
			}
		}
	}

	if cfg.CloudName == "" {
		cfg.CloudName = strings.TrimSpace(os.Getenv("CLOUDINARY_CLOUD_NAME"))
	}
	if cfg.CloudName == "" || cfg.APIKey == "" || cfg.APISecret == "" {
		return nil, errors.New("Cloudinary nao configurado: defina CLOUDINARY_URL ou CLOUDINARY_CLOUD_NAME/CLOUDINARY_API_KEY/CLOUDINARY_API_SECRET")
	}
	return cfg, nil
}

func (s *Server) handleUploadAvatar(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	upload, _, err := uploadMobileMultipartMedia(w, r, uid, "avatar")
	if err != nil {
		writeUploadError(w, err)
		return
	}

	db := mobileDB(s.db)
	if err := db.ensureMobileMediaSchema(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "erro ao preparar schema de midia"})
		return
	}
	if err := db.UpdateUser(r.Context(), uid, map[string]any{"avatar_url": upload.SecureURL}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "erro ao salvar avatar"})
		return
	}
	user, _ := db.GetUserByID(r.Context(), uid)
	var safeUser any
	if user != nil {
		safeUser = s.sanitizeUser(user)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"avatar_url": upload.SecureURL,
		"upload":     upload,
		"user":       safeUser,
	})
}

func (s *Server) handleUploadKYCMedia(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	upload, normalizedKind, err := uploadMobileMultipartMedia(w, r, uid, kind)
	if err != nil {
		writeUploadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":   normalizedKind,
		"url":    upload.SecureURL,
		"upload": upload,
	})
}

func uploadMobileMultipartMedia(w http.ResponseWriter, r *http.Request, userID, kind string) (*cloudinaryUploadResult, string, error) {
	maxBytes := int64(maxMobileVideoUploadBytes)
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		return nil, "", uploadClientError("arquivo invalido ou acima do limite")
	}
	if kind == "" {
		kind = strings.TrimSpace(r.FormValue("kind"))
	}
	kind = normalizeUploadKind(kind)
	if kind == "" {
		return nil, "", uploadClientError("tipo de upload invalido")
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, "", uploadClientError("campo file e obrigatorio")
	}
	defer file.Close()

	allowedBytes := int64(maxMobileImageUploadBytes)
	if kind == "facial_video" {
		allowedBytes = maxMobileVideoUploadBytes
	}
	if header.Size > allowedBytes {
		return nil, "", uploadClientError("arquivo acima do limite permitido")
	}

	cfg, err := loadCloudinaryConfig()
	if err != nil {
		return nil, "", err
	}

	folder := fmt.Sprintf("chainfx/mobile/users/%s/avatar", userID)
	if kind != "avatar" {
		folder = fmt.Sprintf("chainfx/mobile/users/%s/kyc/%s", userID, kind)
	}
	publicID := fmt.Sprintf("%s_%d", kind, time.Now().UnixNano())
	upload, err := uploadToCloudinary(r.Context(), cfg, file, header, folder, publicID)
	return upload, kind, err
}

func uploadToCloudinary(ctx context.Context, cfg *cloudinaryConfig, file multipart.File, header *multipart.FileHeader, folder, publicID string) (*cloudinaryUploadResult, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	fileName := sanitizeUploadFileName(header.Filename)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	params := map[string]string{
		"folder":    folder,
		"public_id": publicID,
		"timestamp": timestamp,
	}
	params["signature"] = signCloudinaryParams(params, cfg.APISecret)
	params["api_key"] = cfg.APIKey
	for key, value := range params {
		if err := writer.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("https://api.cloudinary.com/v1_1/%s/auto/upload", url.PathEscape(cfg.CloudName))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		SecureURL    string `json:"secure_url"`
		PublicID     string `json:"public_id"`
		ResourceType string `json:"resource_type"`
		Format       string `json:"format"`
		Bytes        int64  `json:"bytes"`
		Error        struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(payload.Error.Message)
		if msg == "" {
			msg = "falha ao enviar arquivo para Cloudinary"
		}
		return nil, fmt.Errorf("cloudinary: %s", msg)
	}
	if payload.SecureURL == "" {
		return nil, errors.New("cloudinary nao retornou secure_url")
	}
	return &cloudinaryUploadResult{
		SecureURL:    payload.SecureURL,
		PublicID:     payload.PublicID,
		ResourceType: payload.ResourceType,
		Format:       payload.Format,
		Bytes:        payload.Bytes,
	}, nil
}

func signCloudinaryParams(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		if key != "file" && key != "api_key" && key != "signature" && params[key] != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "&") + secret))
	return hex.EncodeToString(sum[:])
}

func normalizeUploadKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "avatar":
		return "avatar"
	case "document", "document_front", "front":
		return "document_front"
	case "document_back", "back":
		return "document_back"
	case "selfie", "face", "facial_photo":
		return "selfie"
	case "facial_video", "video":
		return "facial_video"
	default:
		return ""
	}
}

func sanitizeUploadFileName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "upload"
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, base)
}

type uploadError string

func (e uploadError) Error() string { return string(e) }

func uploadClientError(message string) error { return uploadError(message) }

func writeUploadError(w http.ResponseWriter, err error) {
	var clientErr uploadError
	if errors.As(err, &clientErr) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": clientErr.Error()})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}
