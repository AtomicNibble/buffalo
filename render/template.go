package render

import (
	"fmt"
	"html/template"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/gobuffalo/buffalo/internal/takeon/github.com/gobuffalo/syncx"
	"github.com/gobuffalo/buffalo/internal/takeon/github.com/markbates/errx"
	"github.com/gobuffalo/envy"
	"github.com/gobuffalo/packd"
	"github.com/sirupsen/logrus"
)

type templateRenderer struct {
	*Engine
	contentType    string
	names          []string
	aliases        syncx.StringMap
	reloadManifest bool
}

func (s *templateRenderer) ContentType() string {
	return s.contentType
}

func (s *templateRenderer) resolve(name string) ([]byte, error) {
	if s.TemplatesBox == nil {
		return nil, fmt.Errorf("no templates box is defined")
	}

	if s.TemplatesBox.Has(name) {
		return s.TemplatesBox.Find(name)
	}

	v, ok := s.aliases.Load(name)
	if !ok {
		return nil, fmt.Errorf("could not find template %s", name)
	}

	return s.TemplatesBox.Find(v)
}

func (s *templateRenderer) Render(w io.Writer, data Data) error {
	if s.TemplatesBox != nil {
		err := s.TemplatesBox.Walk(func(p string, f packd.File) error {
			base := filepath.Base(p)

			dir := filepath.Dir(p)

			var exts []string
			sep := strings.Split(base, ".")
			if len(sep) >= 1 {
				base = sep[0]
			}
			if len(sep) > 1 {
				exts = sep[1:]
			}

			for _, ext := range exts {
				pn := filepath.Join(dir, base+"."+ext)
				s.aliases.Store(pn, p)
			}

			return nil
		})
		if err != nil {
			return err
		}
	}

	var body template.HTML
	var err error
	for _, name := range s.names {
		body, err = s.exec(name, data)
		if err != nil {
			return errx.Wrap(err, name)
		}
		data["yield"] = body
	}
	w.Write([]byte(body))
	return nil
}

func fixExtension(name string, ct string) string {
	if filepath.Ext(name) == "" {
		switch {
		case strings.Contains(ct, "html"):
			name += ".html"
		case strings.Contains(ct, "javascript"):
			name += ".js"
		case strings.Contains(ct, "markdown"):
			name += ".md"
		}
	}
	return name
}

// partialFeeder returns template string for the name from `TemplateBox`.
// It should be registered as helper named `partialFeeder` so plush can
// find it with the name.
func (s *templateRenderer) partialFeeder(name string) (string, error) {
	ct := strings.ToLower(s.contentType)

	d, f := filepath.Split(name)
	name = filepath.Join(d, "_"+f)
	name = fixExtension(name, ct)

	b, err := s.resolve(name)
	return string(b), err
}

func (s *templateRenderer) exec(name string, data Data) (template.HTML, error) {
	ct := strings.ToLower(s.contentType)
	data["contentType"] = ct

	name = fixExtension(name, ct)

	// Try to use localized version
	templateName := name
	if languages, ok := data["languages"].([]string); ok {
		ll := len(languages)
		if ll > 0 {
			// Default language is the last in the list
			defaultLanguage := languages[ll-1]
			ext := filepath.Ext(name)
			rawName := strings.TrimSuffix(name, ext)

			for _, l := range languages {
				var candidateName string
				if l == defaultLanguage {
					break
				}
				candidateName = rawName + "." + strings.ToLower(l) + ext
				if _, err := s.resolve(candidateName); err == nil {
					// Replace name with the existing suffixed version
					templateName = candidateName
					break
				}
			}
		}
	}

	// Set current_template to context
	if _, ok := data["current_template"]; !ok {
		data["current_template"] = templateName
	}

	source, err := s.resolve(templateName)
	if err != nil {
		return "", err
	}

	helpers := map[string]interface{}{}

	for k, v := range s.Helpers {
		helpers[k] = v
	}
	helpers["partialFeeder"] = s.partialFeeder

	helpers = s.addAssetsHelpers(helpers)

	body := string(source)
	for _, ext := range s.exts(name) {
		te, ok := s.TemplateEngines[ext]
		if !ok {
			logrus.Errorf("could not find a template engine for %s", ext)
			continue
		}
		body, err = te(body, data, helpers)
		if err != nil {
			return "", err
		}
	}

	return template.HTML(body), nil
}

func (s *templateRenderer) exts(name string) []string {
	exts := []string{}
	for {
		ext := filepath.Ext(name)
		if ext == "" {
			break
		}
		name = strings.TrimSuffix(name, ext)
		exts = append(exts, strings.ToLower(ext[1:]))
	}
	if len(exts) == 0 {
		return []string{"html"}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(exts)))
	return exts
}

func (s *templateRenderer) assetPath(file string) (string, error) {
	if assetMapLoaded == 0 || s.reloadManifest {
		// Only load it once, if race
		assetMapLock.Lock()
		defer assetMapLock.Unlock()

		if assetMapLoaded == 0 || s.reloadManifest {

			s.reloadManifest = false
			atomic.CompareAndSwapInt32(&assetMapLoaded, 0, 1)

			manifest, err := s.AssetsBox.FindString("manifest.json")

			if err != nil {
				manifest, err = s.AssetsBox.FindString("assets/manifest.json")
				if err != nil {
					return assetPathFor(file), nil
				}
			}

			err = loadManifest(manifest)
			if err != nil {
				return assetPathFor(file), fmt.Errorf("your manifest.json is not correct: %s", err)
			}
		}
	}

	return assetPathFor(file), nil
}
// Template renders the named files using the specified
// content type and the github.com/gobuffalo/plush
// package for templating. If more than 1 file is provided
// the second file will be considered a "layout" file
// and the first file will be the "content" file which will
// be placed into the "layout" using "{{yield}}".
func (e *Engine) Template(c string, names ...string) Renderer {
	return &templateRenderer{
		Engine:         e,
		contentType:    c,
		names:          names,
		aliases:        syncx.StringMap{},
		reloadManifest: envy.Get("GO_ENV", "development") != "production",
	}
}
