package profile

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/gif" // decoders registered for image.Decode
	"image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Avatar limits: uploads up to MaxAvatarBytes, decoded only when the
// image header claims at most maxAvatarPixels (a decompression-bomb
// guard), stored as a 256x256 JPEG.
const (
	MaxAvatarBytes  = 4 << 20
	maxAvatarPixels = 40_000_000
	avatarSize      = 256
)

// ErrBadImage is returned for uploads that are not a usable image.
var ErrBadImage = errors.New("upload a JPEG, PNG, GIF or WebP image")

// NormalizeAvatar decodes an uploaded image, center-crops it to a square,
// scales it to 256x256 and re-encodes it as JPEG. Only the re-encoded
// pixels are stored and served, never the uploaded bytes (no metadata,
// no polyglot payloads).
func NormalizeAvatar(data []byte) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxAvatarPixels {
		return nil, ErrBadImage
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrBadImage
	}
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	crop := image.Rect(0, 0, side, side).Add(image.Pt(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2))

	dst := image.NewRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src) // transparent -> white
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
