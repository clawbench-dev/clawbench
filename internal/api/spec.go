// Package api embeds the ClawBench OpenAPI specification and renders the
// endpoint subset needed by each built-in slash command.
//
// The specification is the single source of truth for the AI-facing HTTP
// contract: the prompt fragments injected by the chat handler are derived from
// it here, so an endpoint rename or a new required field cannot silently drift
// away from what the AI is told. internal/handler's drift-guard test asserts
// that every path in the spec is covered by a registered route and vice versa.
package api

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// specYAML is the OpenAPI document, embedded so the rendered prompt fragments
// travel with the binary and cannot be lost at runtime.
//
//go:embed openapi.yaml
var specYAML []byte

// HTTP methods in the order they are rendered within a path item.
var methodOrder = []string{"get", "post", "put", "patch", "delete"}

type spec struct {
	Paths      map[string]pathItem `yaml:"paths"`
	Components components          `yaml:"components"`
}

type components struct {
	Parameters map[string]*parameter `yaml:"parameters"`
}

// pathItem lists the operations supported by one path. Only the methods the
// spec actually uses are declared; unknown keys are ignored by the decoder.
type pathItem struct {
	Get    *operation `yaml:"get"`
	Post   *operation `yaml:"post"`
	Put    *operation `yaml:"put"`
	Patch  *operation `yaml:"patch"`
	Delete *operation `yaml:"delete"`
}

func (p pathItem) method(name string) *operation {
	switch name {
	case "get":
		return p.Get
	case "post":
		return p.Post
	case "put":
		return p.Put
	case "patch":
		return p.Patch
	case "delete":
		return p.Delete
	}
	return nil
}

type operation struct {
	OperationID string       `yaml:"operationId"`
	Tags        []string     `yaml:"tags"`
	Summary     string       `yaml:"summary"`
	Description string       `yaml:"description"`
	Parameters  []parameter  `yaml:"parameters"`
	RequestBody *requestBody `yaml:"requestBody"`
	// Security is a pointer so an explicit `security: []` (public endpoint) is
	// distinguishable from an absent key (inherits the global requirement).
	Security *[]map[string][]string `yaml:"security"`
}

type parameter struct {
	Ref         string  `yaml:"$ref"`
	Name        string  `yaml:"name"`
	In          string  `yaml:"in"`
	Required    bool    `yaml:"required"`
	Description string  `yaml:"description"`
	Schema      *schema `yaml:"schema"`
}

type requestBody struct {
	Required bool                 `yaml:"required"`
	Content  map[string]mediaType `yaml:"content"`
}

type mediaType struct {
	Schema *schema `yaml:"schema"`
}

type schema struct {
	Type        string   `yaml:"type"`
	Description string   `yaml:"description"`
	Required    []string `yaml:"required"`
	// Properties keeps the document order of the YAML mapping, so rendered
	// field lists follow the order the spec author wrote rather than Go's
	// random map iteration.
	Properties yaml.Node `yaml:"properties"`
}

var (
	parsedOnce sync.Once
	parsedSpec *spec
	parsedErr  error
)

// load parses the embedded spec once. The document is static, so a single
// parse is cached for the process lifetime.
func load() (*spec, error) {
	parsedOnce.Do(func() {
		var s spec
		if err := yaml.Unmarshal(specYAML, &s); err != nil {
			parsedErr = fmt.Errorf("api: parse embedded openapi.yaml: %w", err)
			return
		}
		if len(s.Paths) == 0 {
			parsedErr = fmt.Errorf("api: embedded openapi.yaml declares no paths")
			return
		}
		parsedSpec = &s
	})
	return parsedSpec, parsedErr
}

// resolve turns a `$ref` to a component parameter into its definition.
func (s *spec) resolve(p parameter) parameter {
	const prefix = "#/components/parameters/"
	if !strings.HasPrefix(p.Ref, prefix) {
		return p
	}
	if got, ok := s.Components.Parameters[strings.TrimPrefix(p.Ref, prefix)]; ok && got != nil {
		return *got
	}
	return p
}

// propertyNames returns the field names of a schema's properties in document
// order, along with the set of required names.
func (sc *schema) propertyNames() ([]string, map[string]bool) {
	required := make(map[string]bool, len(sc.Required))
	for _, r := range sc.Required {
		required[r] = true
	}
	if sc.Properties.Kind != yaml.MappingNode {
		return nil, required
	}
	names := make([]string, 0, len(sc.Properties.Content)/2)
	for i := 0; i+1 < len(sc.Properties.Content); i += 2 {
		names = append(names, sc.Properties.Content[i].Value)
	}
	return names, required
}

// schemaType reads the type of a named property. Property values are decoded
// lazily so a nested object (or a $ref) degrades to its declared type or
// "object" instead of failing the whole parse.
func (sc *schema) propertyType(name string) string {
	if sc.Properties.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(sc.Properties.Content); i += 2 {
		if sc.Properties.Content[i].Value != name {
			continue
		}
		var child schema
		if err := sc.Properties.Content[i+1].Decode(&child); err != nil {
			return ""
		}
		if child.Type != "" {
			return child.Type
		}
		if child.Properties.Kind == yaml.MappingNode {
			return "object"
		}
		return ""
	}
	return ""
}

// endpoint is one rendered operation: a method, a path, and its inputs.
type endpoint struct {
	OperationID  string
	Tags         []string
	Method       string
	Path         string
	Summary      string
	Description  string
	PathParams   []parameter
	QueryParams  []parameter
	BodyFields   []bodyField
	BodyRequired bool
}

type bodyField struct {
	Name     string
	Type     string
	Required bool
}

// endpointsForOperations collects the operations with the given operationIds.
//
// This is the selection used for prompt rendering: a tag is too coarse a
// filter (the RAG tag also covers index-rebuild and summarize operations that
// a search command must never call), so the caller names the exact operations
// it wants the AI to see.
//
// requiredTags binds each operationId to the tag it is expected to carry. It
// keeps the selection tied to the spec's own grouping, so an operation that is
// retagged or renamed fails loudly here instead of silently disappearing from
// (or appearing in) the prompt.
func endpointsForOperations(ids []string, requiredTags map[string]string) ([]endpoint, error) {
	s, err := load()
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}

	found := make(map[string]bool, len(ids))
	var out []endpoint
	for path, item := range s.Paths {
		for _, method := range methodOrder {
			op := item.method(method)
			if op == nil || op.OperationID == "" || !want[op.OperationID] {
				continue
			}
			found[op.OperationID] = true
			out = append(out, buildEndpoint(s, method, path, op))
		}
	}

	var missing []string
	for _, id := range ids {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("api: operationId(s) not found in spec: %s", strings.Join(missing, ", "))
	}

	// Verify each selected operation still carries its expected tag.
	for _, ep := range out {
		wantTag, ok := requiredTags[ep.OperationID]
		if !ok {
			continue
		}
		if !hasAnyTag(ep.Tags, map[string]bool{wantTag: true}) {
			return nil, fmt.Errorf("api: operation %s no longer carries tag %q (has %v)",
				ep.OperationID, wantTag, ep.Tags)
		}
	}

	sortEndpoints(out)
	return out, nil
}

// buildEndpoint flattens one operation into its renderable form, resolving
// component parameter references and extracting request-body field names.
func buildEndpoint(s *spec, method, path string, op *operation) endpoint {
	ep := endpoint{
		OperationID: op.OperationID,
		Tags:        op.Tags,
		Method:      strings.ToUpper(method),
		Path:        path,
		Summary:     strings.TrimSpace(op.Summary),
		Description: strings.TrimSpace(op.Description),
	}
	for _, raw := range op.Parameters {
		p := s.resolve(raw)
		switch p.In {
		case "path":
			ep.PathParams = append(ep.PathParams, p)
		case "query":
			ep.QueryParams = append(ep.QueryParams, p)
		}
	}
	if op.RequestBody != nil {
		ep.BodyRequired = op.RequestBody.Required
		if mt, ok := op.RequestBody.Content["application/json"]; ok && mt.Schema != nil {
			names, required := mt.Schema.propertyNames()
			for _, n := range names {
				ep.BodyFields = append(ep.BodyFields, bodyField{
					Name:     n,
					Type:     mt.Schema.propertyType(n),
					Required: required[n],
				})
			}
		}
	}
	return ep
}

// sortEndpoints orders endpoints by path then method so rendered prompts are
// stable across requests (and therefore cacheable).
func sortEndpoints(eps []endpoint) {
	sort.Slice(eps, func(i, j int) bool {
		if eps[i].Path != eps[j].Path {
			return eps[i].Path < eps[j].Path
		}
		return eps[i].Method < eps[j].Method
	})
}

func hasAnyTag(got []string, want map[string]bool) bool {
	for _, t := range got {
		if want[t] {
			return true
		}
	}
	return false
}

// SpecRoute is one path+method pair declared in the spec, exposed for the
// drift-guard test that compares the document against the live mux.
type SpecRoute struct {
	Path        string
	Method      string // upper-case
	OperationID string
	Public      bool // declares `security: []`
}

// SpecRoutes returns every path+method pair in the embedded spec.
func SpecRoutes() ([]SpecRoute, error) {
	s, err := load()
	if err != nil {
		return nil, err
	}
	var out []SpecRoute
	for path, item := range s.Paths {
		for _, method := range methodOrder {
			op := item.method(method)
			if op == nil {
				continue
			}
			out = append(out, SpecRoute{
				Path:        path,
				Method:      strings.ToUpper(method),
				OperationID: op.OperationID,
				Public:      op.Security != nil && len(*op.Security) == 0,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out, nil
}
