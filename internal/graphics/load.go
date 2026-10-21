package graphics

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// maxPixels bounds an image's area, whatever its encoding: the attached
// terminal decodes it, not the server.
const maxPixels = 64 << 20

// tempMarker is what Kitty requires in the path of a t=t temporary file.
const tempMarker = "tty-graphics-protocol"

// pngTrailer is the IEND chunk every complete PNG ends with. Checking it
// catches an image the terminal parser truncated.
var pngTrailer = []byte{0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xAE, 0x42, 0x60, 0x82}

// loaded is a decoded transmission: the image as PNG bytes, whatever the app
// sent, so the server stores and re-sends one compact format.
type loaded struct {
	png           []byte
	width, height int
}

// load turns a transmission's payload into a PNG no larger than limit bytes.
// The error text is a Kitty error ("EINVAL:...") ready to send to the app.
func load(cmd command, data []byte, limit int64) (loaded, string) {
	raw, errText := payload(cmd, data, limit)
	if errText != "" {
		return loaded{}, errText
	}
	if cmd.compressed {
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return loaded{}, "EINVAL:not zlib data"
		}
		raw, err = io.ReadAll(io.LimitReader(zr, limit+1))
		if err != nil {
			return loaded{}, "EINVAL:bad zlib data"
		}
		if int64(len(raw)) > limit {
			return loaded{}, "EFBIG:image too large"
		}
	}
	switch cmd.format {
	case formatPNG:
		cfg, err := png.DecodeConfig(bytes.NewReader(raw))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || !bytes.HasSuffix(raw, pngTrailer) {
			return loaded{}, "EINVAL:bad PNG data"
		}
		if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
			return loaded{}, "EFBIG:image too large"
		}
		return loaded{raw, cfg.Width, cfg.Height}, ""
	case formatRGB, formatRGBA:
		return fromRaw(cmd, raw)
	}
	return loaded{}, "EINVAL:unsupported format"
}

// fromRaw wraps f=24 and f=32 pixel data as a PNG.
func fromRaw(cmd command, raw []byte) (loaded, string) {
	w, h, bpp := cmd.width, cmd.height, 4
	if cmd.format == formatRGB {
		bpp = 3
	}
	if w <= 0 || h <= 0 || int64(w)*int64(h) > maxPixels {
		return loaded{}, "EINVAL:bad image size"
	}
	if int64(len(raw)) != int64(w)*int64(h)*int64(bpp) {
		return loaded{}, "EINVAL:pixel data does not match s and v"
	}
	img := &image.NRGBA{Pix: raw, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
	if bpp == 3 {
		pix := make([]byte, w*h*4)
		for i, j := 0, 0; i < len(raw); i, j = i+3, j+4 {
			pix[j], pix[j+1], pix[j+2], pix[j+3] = raw[i], raw[i+1], raw[i+2], 0xFF
		}
		img.Pix = pix
	}
	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&out, img); err != nil {
		return loaded{}, "EINVAL:cannot encode image"
	}
	return loaded{out.Bytes(), w, h}, ""
}

// payload returns the transmitted bytes: decoded from the sequence itself
// (t=d) or read from a file on this machine (t=f, t=t). Shared memory (t=s)
// is refused. Nothing here is ever forwarded to the attached terminal.
func payload(cmd command, data []byte, limit int64) ([]byte, string) {
	switch cmd.medium {
	case 'd':
		if int64(len(data)) > limit/3*4+8 {
			return nil, "EFBIG:image too large"
		}
		raw, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			raw, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(string(data), "="))
		}
		if err != nil {
			return nil, "EINVAL:bad base64 data"
		}
		if int64(len(raw)) > limit {
			return nil, "EFBIG:image too large"
		}
		return raw, ""
	case 'f', 't':
		name, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			name, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(string(data), "="))
		}
		if err != nil || len(name) == 0 {
			return nil, "EINVAL:bad file path"
		}
		return readFile(string(name), cmd, cmd.medium == 't', limit)
	case 's':
		return nil, "EINVAL:shared memory transmission is not supported"
	}
	return nil, "EINVAL:unsupported transmission medium"
}

// readFile reads a regular file for t=f and t=t. A temporary file must live in
// a temp directory and have tty-graphics-protocol in its path, and is deleted
// once read, as in Kitty. Symlinks are resolved first so those checks apply
// to the real file.
func readFile(name string, cmd command, temporary bool, limit int64) ([]byte, string) {
	if !filepath.IsAbs(name) || strings.ContainsRune(name, 0) {
		return nil, "EINVAL:file path must be absolute"
	}
	real, err := filepath.EvalSymlinks(name)
	if err != nil {
		return nil, "ENOENT:cannot read file"
	}
	if temporary && !(strings.Contains(real, tempMarker) && inTempDir(real)) {
		return nil, "EINVAL:not a graphics temporary file"
	}
	// O_NONBLOCK keeps opening a FIFO from hanging; it is refused below.
	f, err := os.OpenFile(real, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, "ENOENT:cannot read file"
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, "EINVAL:not a regular file"
	}
	if temporary {
		defer os.Remove(real)
	}
	offset := max(cmd.offset, 0)
	length := max(info.Size()-offset, 0)
	if cmd.size > 0 {
		length = min(length, cmd.size)
	}
	if length > limit {
		return nil, "EFBIG:image too large"
	}
	raw := make([]byte, length)
	if _, err := io.ReadFull(io.NewSectionReader(f, offset, length), raw); err != nil {
		return nil, "EINVAL:cannot read file"
	}
	return raw, ""
}

// inTempDir reports whether the already symlink-resolved path is inside a
// temporary directory Kitty also accepts.
func inTempDir(real string) bool {
	for _, dir := range []string{os.TempDir(), "/tmp", "/dev/shm"} {
		root, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(root, real); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}
