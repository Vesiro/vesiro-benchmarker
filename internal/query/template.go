package query

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Binding struct {
	Source      string   `json:"source"`
	TermsInline []string `json:"terms,omitempty"`
	Path        string   `json:"path,omitempty"`
	Query       string   `json:"query,omitempty"`
	SplitOn     string   `json:"split_on,omitempty"`
}

type Template struct {
	Name        string             `json:"name"`
	Index       string             `json:"index"`
	Description string             `json:"description"`
	Bindings    map[string]Binding `json:"bindings"`
	Template    json.RawMessage    `json:"template"`
}

func LoadTemplateFromFile(filePath string) (Template, error) {
	tmplFile, err := os.ReadFile(filePath)
	if err != nil {
		return Template{}, err
	}
	var tmpl Template
	if err := json.Unmarshal(tmplFile, &tmpl); err != nil {
		return Template{}, err
	}
	compact, err := compactRawJson(tmpl.Template)
	if err != nil {
		return Template{}, err
	}
	tmpl.Template = compact
	return tmpl, nil
}

func LoadRawTemplateFromFile(filePath string) (Template, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return Template{}, err
	}
	compacted, err := compactRawJson(raw)
	if err != nil {
		return Template{}, err
	}
	return Template{
		Name:     filepath.Base(filePath),
		Bindings: map[string]Binding{},
		Template: compacted,
	}, nil
}

func compactRawJson(raw json.RawMessage) (json.RawMessage, error) {
	var temp interface{}
	if err := json.Unmarshal(raw, &temp); err != nil {
		return nil, err
	}
	compacted, err := json.Marshal(temp)
	if err != nil {
		return nil, err
	}
	return compacted, nil
}

func (b Binding) Terms() ([]string, error) {
	switch b.Source {
	case "inline":
		return b.inlineTerms()
	case "file":
		return b.fileTerms()
	default:
		return nil, fmt.Errorf("invalid binding source '%s'", b.Source)
	}
}

func (b Binding) inlineTerms() ([]string, error) {
	if b.TermsInline == nil {
		return nil, fmt.Errorf("inline source is missing terms")
	}
	return b.TermsInline, nil
}

func (b Binding) fileTerms() ([]string, error) {
	if b.Path == "" {
		return nil, fmt.Errorf("file source is missing path")
	}

	file, err := os.Open(b.Path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	// The default bufio.Scanner buffer is 64KB, which is too small when a single line
	// contains tens of thousands of IDs. Increase the buffer so each line can hold
	// multi-megabyte JSON arrays of IDs without truncation.
	const maxLineBytes = 64 * 1024 * 1024 // 64MB per line
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 1024*1024), maxLineBytes)

	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	return lines, sc.Err()
}
