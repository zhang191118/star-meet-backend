package v1

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxImageUploadBytes int64 = 10 << 20
const maxImagePixels = 16_000_000

var lookupPublicImageIPs = net.LookupIP

func readImageUpload(file *multipart.FileHeader) ([]byte, string, string, error) {
	if file == nil || file.Size <= 0 || file.Size > maxImageUploadBytes {
		return nil, "", "", fmt.Errorf("image must be between 1 byte and %d MB", maxImageUploadBytes>>20)
	}
	src, err := file.Open()
	if err != nil {
		return nil, "", "", err
	}
	defer src.Close()
	buffer, err := io.ReadAll(io.LimitReader(src, maxImageUploadBytes+1))
	if err != nil {
		return nil, "", "", err
	}
	if int64(len(buffer)) > maxImageUploadBytes {
		return nil, "", "", fmt.Errorf("image exceeds %d MB", maxImageUploadBytes>>20)
	}
	return validateImageBytes(buffer)
}

func validateImageBytes(buffer []byte) ([]byte, string, string, error) {
	if len(buffer) == 0 || int64(len(buffer)) > maxImageUploadBytes {
		return nil, "", "", fmt.Errorf("invalid image size")
	}
	contentType := http.DetectContentType(buffer)
	ext := map[string]string{
		"image/jpeg": "jpg",
		"image/png":  "png",
		"image/gif":  "gif",
	}[contentType]
	if ext == "" {
		return nil, "", "", fmt.Errorf("unsupported image content type")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(buffer))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxImagePixels {
		return nil, "", "", fmt.Errorf("invalid or oversized image dimensions")
	}
	return buffer, ext, contentType, nil
}

func publicImageURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, fmt.Errorf("only public HTTPS image URLs are allowed")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return nil, fmt.Errorf("private image hosts are not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return nil, fmt.Errorf("private image hosts are not allowed")
		}
		return parsed, nil
	}
	ips, err := lookupPublicImageIPs(host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("image host cannot be resolved")
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return nil, fmt.Errorf("private image hosts are not allowed")
		}
	}
	return parsed, nil
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsLinkLocalMulticast() || ip.IsMulticast()
}

func safeImageHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				ips, lookupErr := lookupPublicImageIPs(host)
				if lookupErr != nil || len(ips) == 0 {
					return nil, fmt.Errorf("image host cannot be resolved")
				}
				ip = ips[0]
			}
			if isPrivateIP(ip) {
				return nil, fmt.Errorf("private image hosts are not allowed")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		},
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: transport}
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if _, err := publicImageURL(req.URL.String()); err != nil {
			return err
		}
		return nil
	}
	return client
}

func imageSizeError() string {
	return "图片必须是 16MP 以内的 JPG、PNG 或 GIF，且不超过 " + strconv.FormatInt(maxImageUploadBytes>>20, 10) + "MB"
}
