package deeplink

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

// Service holds configuration, processors, and templates. Create with [New].
type Service struct {
	config       Config
	registry     *Registry
	clicks       *clickTracker
	skipPatterns []*regexp.Regexp
	templates    map[string]*template.Template
}

// New creates a Service. Store and BaseURL are required.
func New(cfg Config) (*Service, error) {
	cfg.defaults()

	if cfg.Store == nil {
		return nil, fmt.Errorf("deeplink: Store is required")
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("deeplink: BaseURL is required")
	}

	patterns, err := compileSkipPatterns(cfg.SkipPaths)
	if err != nil {
		return nil, fmt.Errorf("deeplink: %w", err)
	}

	templates, err := loadTemplates(cfg.TemplateDir)
	if err != nil {
		return nil, fmt.Errorf("deeplink: %w", err)
	}

	return &Service{
		config:       cfg,
		registry:     NewRegistry(),
		clicks:       newClickTracker(cfg.Store, cfg.Logger, cfg.ClickBufferSize, cfg.ClickFlushInterval),
		skipPatterns: patterns,
		templates:    templates,
	}, nil
}

// Register adds a [Processor]. Panics if the type is already registered.
func (s *Service) Register(p Processor) {
	s.registry.Register(p)
}

// Config returns a copy of the service configuration.
func (s *Service) Config() Config {
	return s.config
}

// Types returns all registered processor type names.
func (s *Service) Types() []string {
	return s.registry.Types()
}

// Close releases resources held by the service.
// It drains any buffered click events and closes the HTTP transport.
func (s *Service) Close() error {
	s.clicks.stop()
	if t, ok := s.config.HTTPClient.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
	return nil
}

// loadTemplates loads HTML templates from dir. Missing files are skipped;
// the corresponding handler falls back to a 302 redirect.
func loadTemplates(dir string) (map[string]*template.Template, error) {
	if dir == "" {
		return map[string]*template.Template{}, nil
	}

	files := map[string]string{
		"link":    "link.html",
		"preview": "preview.html",
	}

	templates := make(map[string]*template.Template, len(files))
	for name, file := range files {
		path := filepath.Join(dir, file)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		tmpl, err := template.ParseFiles(path)
		if err != nil {
			return nil, err
		}
		templates[name] = tmpl
	}

	return templates, nil
}
