package rembed

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

//go:embed webdist
var bundledSite embed.FS

type payloadData struct {
	Title      string `json:"title"`
	Version    string `json:"version"`
	Generated  string `json:"generated"`
	Markdown   string `json:"markdown"`
	SourcePath string `json:"sourcePath"`
}

// WriteOptions configures standalone docs generation from markdown content.
type WriteOptions struct {
	// Version controls the docs output folder name. Empty defaults to "dev".
	Version string
	// Title sets the rendered page heading. Empty defaults to "Documentation".
	Title string
	// SourcePath is shown in page metadata. Empty defaults to "embedded markdown".
	SourcePath string
	// Force regenerates docs even if the destination file already exists.
	Force bool
	// InlineAssets replaces matching markdown and HTML asset references with data URLs.
	InlineAssets map[string]InlineAsset
	// LinkBaseURL rewrites relative markdown and HTML links against an absolute base URL.
	LinkBaseURL string
}

// InlineAsset describes a binary asset that may be inlined into markdown links.
type InlineAsset struct {
	Data []byte
	// MIMEType is optional. When empty, the type is inferred from the asset path.
	MIMEType string
}

var scriptCloseTagRe = regexp.MustCompile(`(?i)</script>`)
var markdownInlineLinkRe = regexp.MustCompile(`(!?\[[^\]]*\]\()([^\)]+)(\))`)
var htmlSrcHrefRe = regexp.MustCompile(`(?i)(href|src)\s*=\s*(?:"([^"]+)"|'([^']+)')`)

// RewriteRelativeLinks rewrites relative markdown and HTML links to absolute URLs.
//
// baseURL must be an absolute URL and should usually end with '/'.
func RewriteRelativeLinks(markdown, baseURL string) string {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return markdown
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}

	baseParsed, err := url.Parse(base)
	if err != nil || !baseParsed.IsAbs() {
		return markdown
	}

	rewritten := markdownInlineLinkRe.ReplaceAllStringFunc(markdown, func(match string) string {
		parts := markdownInlineLinkRe.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}

		target := strings.TrimSpace(parts[2])
		if !isRelativeReference(target) {
			return match
		}

		resolved := resolveReference(baseParsed, target)
		if resolved == "" {
			return match
		}

		return parts[1] + resolved + parts[3]
	})

	rewritten = htmlSrcHrefRe.ReplaceAllStringFunc(rewritten, func(match string) string {
		parts := htmlSrcHrefRe.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}

		target := strings.TrimSpace(firstNonEmpty(parts[2], parts[3]))
		if !isRelativeReference(target) {
			return match
		}

		resolved := resolveReference(baseParsed, target)
		if resolved == "" {
			return match
		}

		attr := parts[1]
		quote := `"`
		if parts[2] == "" {
			quote = "'"
		}
		return attr + "=" + quote + resolved + quote
	})

	return rewritten
}

// InlineReferencedAssets replaces matching markdown/HTML asset links with data URLs.
//
// Asset map keys should be repository-relative or markdown-relative paths like
// "assets/logo.svg" or "./assets/logo.svg".
func InlineReferencedAssets(markdown string, assets map[string]InlineAsset) string {
	if markdown == "" || len(assets) == 0 {
		return markdown
	}

	dataURLs := make(map[string]string, len(assets))
	for ref, asset := range assets {
		key := normalizeAssetReference(ref)
		if key == "" || len(asset.Data) == 0 {
			continue
		}

		mimeType := strings.TrimSpace(asset.MIMEType)
		if mimeType == "" {
			mimeType = detectMIMEType(ref)
		}

		dataURLs[key] = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(asset.Data)
	}

	if len(dataURLs) == 0 {
		return markdown
	}

	rewritten := markdownInlineLinkRe.ReplaceAllStringFunc(markdown, func(match string) string {
		parts := markdownInlineLinkRe.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}

		target, suffix := splitMarkdownTarget(parts[2])
		resolved, ok := dataURLs[normalizeAssetReference(target)]
		if !ok {
			return match
		}

		return parts[1] + resolved + suffix + parts[3]
	})

	rewritten = htmlSrcHrefRe.ReplaceAllStringFunc(rewritten, func(match string) string {
		parts := htmlSrcHrefRe.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}

		target := firstNonEmpty(parts[2], parts[3])
		resolved, ok := dataURLs[normalizeAssetReference(target)]
		if !ok {
			return match
		}

		attr := parts[1]
		quote := `"`
		if parts[2] == "" {
			quote = "'"
		}
		return attr + "=" + quote + resolved + quote
	})

	return rewritten
}

// GitHubRawBaseURL builds a raw.githubusercontent.com base URL for a repo ref.
//
// If ref is empty, it defaults to "main".
func GitHubRawBaseURL(owner, repo, ref string) string {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if owner == "" || repo == "" {
		return ""
	}

	resolvedRef := strings.TrimSpace(ref)
	if resolvedRef == "" {
		resolvedRef = "main"
	}

	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/", owner, repo, resolvedRef)
}

// RewriteRelativeLinksForGitHub rewrites relative links using a GitHub raw-content base URL.
func RewriteRelativeLinksForGitHub(markdown, owner, repo, ref string) string {
	baseURL := GitHubRawBaseURL(owner, repo, ref)
	if baseURL == "" {
		return markdown
	}

	return RewriteRelativeLinks(markdown, baseURL)
}

// WriteDocs writes markdown content to a standalone docs page using sensible defaults.
//
// It writes to <baseDir>/docs/dev/index.html with title "Documentation".
func WriteDocs(baseDir, markdown string) (string, error) {
	return WriteDocsWithOptions(baseDir, markdown, WriteOptions{})
}

// WriteDocsWithOptions writes markdown content to versioned standalone docs.
//
// This is a convenience wrapper for consumers that embed their own README string.
func WriteDocsWithOptions(baseDir, markdown string, opts WriteOptions) (string, error) {
	preparedMarkdown := prepareMarkdown(markdown, opts)
	payload := makePayloadData([]byte(preparedMarkdown), opts)
	return WriteVersionedDocs(baseDir, payload.Version, []byte(preparedMarkdown), payload.Title, payload.SourcePath, opts.Force)
}

// RenderHTML renders markdown content into a standalone HTML page in memory.
func RenderHTML(markdown string, opts WriteOptions) ([]byte, error) {
	preparedMarkdown := prepareMarkdown(markdown, opts)
	payload := makePayloadData([]byte(preparedMarkdown), opts)
	return buildStandaloneHTML(payload)
}

// WriteVersionedDocs writes a standalone docs page to <baseDir>/docs/<version>/index.html.
// The resulting HTML has inline CSS, inline app JS, and inline markdown payload,
// so it can be opened directly with file:// without running a local web server.
func WriteVersionedDocs(baseDir, version string, markdown []byte, title, sourcePath string, force bool) (string, error) {
	if strings.TrimSpace(baseDir) == "" {
		return "", fmt.Errorf("base directory is required")
	}
	if strings.TrimSpace(version) == "" {
		version = "dev"
	}

	docsVersion := strings.TrimPrefix(version, "v")
	docDir := filepath.Join(baseDir, "docs", docsVersion)
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		return "", fmt.Errorf("create docs directory: %w", err)
	}

	docPath := filepath.Join(docDir, "index.html")
	if !force {
		if info, err := os.Stat(docPath); err == nil && info.Size() > 0 {
			return docPath, nil
		}
	}

	payload := makePayloadData(markdown, WriteOptions{
		Version:    version,
		Title:      title,
		SourcePath: sourcePath,
		Force:      force,
	})

	htmlBytes, err := buildStandaloneHTML(payload)
	if err != nil {
		return "", err
	}

	if err := writeFileAtomic(docPath, htmlBytes, 0o644); err != nil {
		return "", fmt.Errorf("write docs html: %w", err)
	}

	return docPath, nil
}

func buildStandaloneHTML(payload payloadData) ([]byte, error) {
	indexBytes, err := fs.ReadFile(bundledSite, "webdist/index.html")
	if err != nil {
		return nil, fmt.Errorf("read bundled index.html: %w", err)
	}
	cssBytes, err := fs.ReadFile(bundledSite, "webdist/assets/index.css")
	if err != nil {
		return nil, fmt.Errorf("read bundled css: %w", err)
	}
	appBytes, err := fs.ReadFile(bundledSite, "webdist/assets/app.js")
	if err != nil {
		return nil, fmt.Errorf("read bundled app.js: %w", err)
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal docs payload: %w", err)
	}

	inline := string(indexBytes)
	inline, err = replaceOne(
		inline,
		`<link rel="stylesheet" crossorigin href="./assets/index.css">`,
		"<style>\n"+string(cssBytes)+"\n</style>",
		"css link",
	)
	if err != nil {
		return nil, err
	}

	payloadScript := "<script>window.__REMBED_DATA__ = " + string(payloadJSON) + ";</script>"
	appScript := "<script type=\"module\">\n" + escapeInlineScript(string(appBytes)) + "\n</script>"
	inline, err = replaceOne(
		inline,
		`<script type="module" crossorigin src="./assets/app.js"></script>`,
		payloadScript+"\n    "+appScript,
		"app module script",
	)
	if err != nil {
		return nil, err
	}

	inline = strings.ReplaceAll(inline, `<script src="./docs-data.js"></script>`, "")

	if strings.Contains(inline, "./assets/") || strings.Contains(inline, "docs-data.js") {
		return nil, fmt.Errorf("standalone docs html still references external assets")
	}

	return []byte(inline), nil
}

func replaceOne(content, old, replacement, label string) (string, error) {
	if !strings.Contains(content, old) {
		return "", fmt.Errorf("%s not found in bundled index", label)
	}
	return strings.Replace(content, old, replacement, 1), nil
}

func escapeInlineScript(js string) string {
	return scriptCloseTagRe.ReplaceAllString(js, `<\/script>`)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func prepareMarkdown(markdown string, opts WriteOptions) string {
	prepared := markdown
	if len(opts.InlineAssets) > 0 {
		prepared = InlineReferencedAssets(prepared, opts.InlineAssets)
	}
	if strings.TrimSpace(opts.LinkBaseURL) != "" {
		prepared = RewriteRelativeLinks(prepared, opts.LinkBaseURL)
	}
	return prepared
}

func normalizeAssetReference(ref string) string {
	candidate := strings.TrimSpace(ref)
	if candidate == "" {
		return ""
	}

	candidate = strings.ReplaceAll(candidate, `\`, "/")
	candidate = strings.TrimPrefix(candidate, "./")
	candidate = strings.TrimPrefix(candidate, "/")

	cleaned := path.Clean(candidate)
	if cleaned == "." {
		return ""
	}

	return strings.TrimPrefix(cleaned, "./")
}

func splitMarkdownTarget(raw string) (string, string) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", ""
	}

	if strings.HasPrefix(target, "<") {
		if end := strings.Index(target, ">"); end > 1 {
			return target[1:end], target[end+1:]
		}
	}

	for i := 0; i < len(target); i++ {
		switch target[i] {
		case ' ', '\t', '\n', '\r':
			return target[:i], target[i:]
		}
	}

	return target, ""
}

func detectMIMEType(ref string) string {
	ext := strings.ToLower(path.Ext(ref))
	switch ext {
	case ".svg":
		return "image/svg+xml"
	}

	if resolved := mime.TypeByExtension(ext); resolved != "" {
		return resolved
	}

	return "application/octet-stream"
}

func isRelativeReference(target string) bool {
	if target == "" {
		return false
	}

	lower := strings.ToLower(target)
	if strings.HasPrefix(target, "#") ||
		strings.HasPrefix(target, "/") ||
		strings.HasPrefix(target, "//") ||
		strings.HasPrefix(lower, "data:") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "javascript:") {
		return false
	}

	parsed, err := url.Parse(target)
	if err != nil {
		return false
	}

	return parsed.Scheme == ""
}

func resolveReference(base *url.URL, target string) string {
	ref, err := url.Parse(target)
	if err != nil {
		return ""
	}

	return base.ResolveReference(ref).String()
}

func makePayloadData(markdown []byte, opts WriteOptions) payloadData {
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = "dev"
	}

	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "Documentation"
	}

	sourcePath := strings.TrimSpace(opts.SourcePath)
	if sourcePath == "" {
		sourcePath = "embedded markdown"
	}

	return payloadData{
		Title:      title,
		Version:    version,
		Generated:  time.Now().Format("2006-01-02 15:04:05"),
		Markdown:   string(markdown),
		SourcePath: sourcePath,
	}
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, mode); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}

// OpenInBrowser opens a file path in the user's default browser.
func OpenInBrowser(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	return openTarget(abs)
}

func openTarget(target string) error {

	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	case "darwin":
		return exec.Command("open", target).Start()
	default:
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return fmt.Errorf("cannot open browser automatically: xdg-open not found")
		}
		return exec.Command("xdg-open", target).Start()
	}
}
