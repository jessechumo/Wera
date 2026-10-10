package api

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"wera/internal/profile"
	"wera/internal/store"
)

// putAvatar is PUT /api/profile/avatar: a multipart "file" image, stored
// re-encoded as a 256x256 JPEG.
func (s *Server) putAvatar(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, profile.MaxAvatarBytes+64<<10)
	if err := r.ParseMultipartForm(profile.MaxAvatarBytes); err != nil {
		s.writeError(w, http.StatusBadRequest, "upload an image of at most 4 MB")
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "no file uploaded")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "could not read the upload")
		return
	}
	img, err := profile.NormalizeAvatar(data)
	if errors.Is(err, profile.ErrBadImage) {
		s.writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "could not process the image")
		return
	}
	if err := store.SaveAvatar(r.Context(), s.Pool, currentUser(r).ID, "image/jpeg", img); err != nil {
		s.writeError(w, http.StatusInternalServerError, "could not save the image")
		return
	}
	s.me(w, r)
}

// getAvatar is GET /api/profile/avatar.
func (s *Server) getAvatar(w http.ResponseWriter, r *http.Request) {
	ct, data, _, ok, err := store.Avatar(r.Context(), s.Pool, currentUser(r).ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if !ok {
		s.writeError(w, http.StatusNotFound, "no profile picture")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400") // URLs carry ?v=<version>
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

// deleteAvatar is DELETE /api/profile/avatar.
func (s *Server) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	if err := store.DeleteAvatar(r.Context(), s.Pool, currentUser(r).ID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.me(w, r)
}

// getResumeFile is GET /api/profile/resume/file: the user's own PDF,
// shown inline (the bytes were checked to be a PDF on upload).
func (s *Server) getResumeFile(w http.ResponseWriter, r *http.Request) {
	name, data, _, ok, err := store.ResumeFile(r.Context(), s.Pool, currentUser(r).ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if !ok {
		s.writeError(w, http.StatusNotFound, "no resume file on record (pasted text only)")
		return
	}
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", disposition+`; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

// safeFilename keeps a short ASCII name for Content-Disposition.
func safeFilename(name string) string {
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	var b strings.Builder
	for _, r := range base {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._- ", r)) {
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if !strings.HasSuffix(strings.ToLower(out), ".pdf") || len(out) < 5 {
		out = "resume.pdf"
	}
	if len(out) > 80 {
		out = out[len(out)-80:]
	}
	return out
}
