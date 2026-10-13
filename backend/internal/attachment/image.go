package attachment

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"strings"

	_ "golang.org/x/image/webp"
)

// measured are the types whose size in pixels is read, the images a statement shows in place.
var measured = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// inline are the types a download may show in place. SVG is left out: it runs scripts where it is shown.
var inline = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"application/pdf": true, "text/plain": true,
}

// dimensions reads an image's size from its header alone; anything that is not one, or does not decode, has none.
func dimensions(contentType string, data []byte) (width, height *int) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || !measured[mediaType] {
		return nil, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, nil
	}
	return &cfg.Width, &cfg.Height
}

// contentTypeFor trusts a specific type the client sent and sniffs the bytes otherwise, so a browser that says
// nothing still gets a sensible download.
func contentTypeFor(declared string, data []byte) string {
	declared = strings.TrimSpace(declared)
	if declared != "" && declared != "application/octet-stream" && !strings.ContainsAny(declared, "\r\n") {
		if _, _, err := mime.ParseMediaType(declared); err == nil {
			return declared
		}
	}
	return http.DetectContentType(data)
}

// ShowsInPlace says whether a file of contentType may be shown in the browser rather than downloaded.
func ShowsInPlace(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && inline[mediaType]
}
