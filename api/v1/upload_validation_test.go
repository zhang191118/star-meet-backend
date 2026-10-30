package v1

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net"
	"testing"
)

func TestValidateImageBytesAcceptsDecodedPNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	buffer, ext, contentType, err := validateImageBytes(data.Bytes())
	if err != nil {
		t.Fatalf("validate png: %v", err)
	}
	if len(buffer) == 0 || ext != "png" || contentType != "image/png" {
		t.Fatalf("unexpected image metadata: bytes=%d ext=%q type=%q", len(buffer), ext, contentType)
	}
}

func TestValidateImageBytesRejectsFilenameOnlyPayload(t *testing.T) {
	if _, _, _, err := validateImageBytes([]byte("not an image, despite a .jpg filename")); err == nil {
		t.Fatal("expected non-image payload to be rejected")
	}
}

func TestPublicImageURLRejectsPrivateAndPlainHTTPHosts(t *testing.T) {
	for _, raw := range []string{
		"http://example.com/image.png",
		"https://127.0.0.1/image.png",
		"https://169.254.169.254/latest/meta-data",
		"https://10.0.0.1/image.png",
		"https://localhost/image.png",
	} {
		if _, err := publicImageURL(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestPublicImageURLAcceptsPublicHTTPSHost(t *testing.T) {
	originalLookup := lookupPublicImageIPs
	lookupPublicImageIPs = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	t.Cleanup(func() { lookupPublicImageIPs = originalLookup })

	parsed, err := publicImageURL("https://example.com/image.png")
	if err != nil {
		t.Fatalf("expected public HTTPS URL to pass: %v", err)
	}
	if parsed.Hostname() != "example.com" {
		t.Fatalf("unexpected host: %s", parsed.Hostname())
	}
}
