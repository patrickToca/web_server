package web

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

// TestTemplatesParse walks every template file and parses it with the
// production function map. A template that fails to parse fails the
// build; the /test route that used to serve this purpose is gone.
func TestTemplatesParse(t *testing.T) {
	root := filepath.Join("..", "templates")

	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".html") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(files) == 0 {
		t.Fatalf("no templates found under %s", root)
	}

	funcs := GetTemplateFuncs()

	// Parse each file individually. Parsing all files at once would
	// fail on any file that uses {{template}} to include another,
	// because the include target would need to be defined first. The
	// production code uses LoadHTMLGlob, which parses them all
	// together; the parse test verifies each file is at least
	// syntactically valid on its own. Cross-file include resolution is
	// exercised by the handler tests, which render real pages.
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			_, err := template.New(filepath.Base(f)).Funcs(funcs).ParseFiles(f)
			if err != nil {
				t.Errorf("parse %s: %v", f, err)
			}
		})
	}
}
