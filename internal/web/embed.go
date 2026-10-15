package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"io/fs"
	"strings"
)

//go:embed dist/front/* dist/admin/* templates/*
var assets embed.FS

var assetVersions = map[string]string{
	"front": assetVersion("front"),
	"admin": assetVersion("admin"),
}

// Version the entire bundle so changes to scripts, styles or chunks invalidate cached entry points.
func assetVersion(bundle string) string {
	hash := sha256.New()
	err := fs.WalkDir(assets, "dist/"+bundle, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		hash.Write([]byte(path + "\x00"))
		hash.Write(data)
		return nil
	})
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

func ParseTemplates() *template.Template {
	return template.Must(template.New("").Funcs(template.FuncMap{
		"assetURL": func(bundle, name string) string {
			// Keep the version in relative imports too: query strings are dropped
			// when a split chunk imports the entry point again.
			return "/static/" + bundle + "/" + assetVersions[bundle] + "/" + name
		},
	}).ParseFS(Templates(), "*.html"))
}

func Front() fs.FS {
	sub, _ := fs.Sub(assets, "dist/front")
	return versionedFS{FS: sub, version: assetVersions["front"]}
}

func Admin() fs.FS {
	sub, _ := fs.Sub(assets, "dist/admin")
	return versionedFS{FS: sub, version: assetVersions["admin"]}
}

// Serve a versioned directory while retaining unversioned paths for older pages.
// Relative imports and CSS URLs stay within the same bundle version.
type versionedFS struct {
	fs.FS
	version string
}

func (v versionedFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return v.FS.Open(strings.TrimPrefix(name, v.version+"/"))
}

func Templates() fs.FS {
	sub, _ := fs.Sub(assets, "templates")
	return sub
}
