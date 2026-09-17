package benchmark

import (
	"encoding/json"
	"math/rand"
	"os"

	"github.com/Vesiro/vesiro-benchmarker/internal/query"
)

// QueryConfig describes how to turn a template file on disk into the template a
// run generates queries from.
type QueryConfig struct {
	TemplatePath    string
	ExtraFieldsJSON string
}

func PrepareQuery(config QueryConfig) (query.Template, error) {
	tmpl, err := loadQueryTemplate(config.TemplatePath)
	if err != nil {
		return query.Template{}, err
	}
	extraFields, err := ParseExtraFields(config.ExtraFieldsJSON)
	if err != nil {
		return query.Template{}, err
	}
	if err := ApplyExtraFields(&tmpl, extraFields); err != nil {
		return query.Template{}, err
	}
	return tmpl, nil
}

// loadQueryTemplate accepts both a full template document and a bare query
// body, so a folder of raw JSON queries works the same as a template file.
func loadQueryTemplate(filePath string) (query.Template, error) {
	isTemplate, err := isTemplateDocument(filePath)
	if err != nil {
		return query.Template{}, err
	}
	if isTemplate {
		return query.LoadTemplateFromFile(filePath)
	}
	return query.LoadRawTemplateFromFile(filePath)
}

func isTemplateDocument(filePath string) (bool, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return false, err
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, err
	}

	_, ok := obj["template"]
	return ok, nil
}

func ParseExtraFields(raw string) (map[string]any, error) {
	var extraFields map[string]any
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &extraFields); err != nil {
			return nil, err
		}
	}
	return extraFields, nil
}

func ApplyExtraFields(tmpl *query.Template, extraFields map[string]any) error {
	if len(extraFields) == 0 {
		return nil
	}

	var obj map[string]any
	if err := json.Unmarshal(tmpl.Template, &obj); err != nil {
		return err
	}

	for k, v := range extraFields {
		obj[k] = v
	}

	raw, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	tmpl.Template = raw
	return nil
}

func NewResolver(template query.Template, seed int64, skipValidation bool) (query.Resolver, error) {
	rng := rand.New(rand.NewSource(seed))
	return query.NewResolver(template, rng, skipValidation)
}
