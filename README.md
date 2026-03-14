# rembed

rembed embeds README-style markdown into an application and renders it as a single standalone HTML page with inline CSS, inline JavaScript, syntax highlighting, and theme switching.

The package currently lives inside the nodeman repository so it can be extracted into its own repository with minimal cleanup later.

## What It Does

- render embedded markdown as polished standalone HTML
- inline referenced assets such as `assets/logo.svg` as data URLs
- rewrite relative links to a repository or other absolute base URL
- write versioned local docs that open directly with `file://`
- open generated docs in the default browser

## Example

```go
package main

import (
	_ "embed"
	"log"

	"github.com/arcmantle/rembed"
)

//go:embed README.md
var embeddedREADME []byte

func main() {
	html, err := rembed.RenderHTML(string(embeddedREADME), rembed.WriteOptions{
		Title:      "My App Documentation",
		Version:    "dev",
		SourcePath: "README.md",
		LinkBaseURL: rembed.GitHubRawBaseURL("owner", "repo", "main"),
	})
	if err != nil {
		log.Fatal(err)
	}

	_ = html
}
```

## Typical README Flow

```go
docPath, err := rembed.WriteDocsWithOptions(baseDir, string(embeddedREADME), rembed.WriteOptions{
	Title:      "My App Documentation",
	Version:    version,
	SourcePath: "embedded README.md",
	Force:      force,
	InlineAssets: map[string]rembed.InlineAsset{
		"assets/logo.svg": {
			Data:     embeddedLogo,
			MIMEType: "image/svg+xml",
		},
	},
	LinkBaseURL: rembed.GitHubRawBaseURL("owner", "repo", "main"),
})
```

## Web Bundle

The standalone page UI is built from `web/` and embedded from `webdist/`.

To rebuild it:

```bash
cd rembed/web
pnpm install
pnpm build
```