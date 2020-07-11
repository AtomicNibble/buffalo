package render

import "github.com/gobuffalo/envy"

// Plain renders the named files using the 'text/plain'
// content type and the github.com/gobuffalo/plush
// package for templating. If more than 1 file is provided
// the second file will be considered a "layout" file
// and the first file will be the "content" file which will
// be placed into the "layout" using "<%= yield %>".
func (e *Engine) Plain(names ...string) Renderer {
	hr := &templateRenderer{
		Engine:         e,
		contentType:    "text/plain; charset=utf-8",
		names:          names,
		reloadManifest: envy.Get("GO_ENV", "development") != "production",
	}
	return hr
}
