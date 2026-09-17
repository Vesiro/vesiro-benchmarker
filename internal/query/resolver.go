package query

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"text/template"
)

type fieldResolver struct {
	Rng     *rand.Rand
	Terms   []string
	Indices []int
	Index   int
	SplitOn string
}

type Resolver struct {
	FieldResolvers map[string]*fieldResolver
	Template       Template
	ParsedTemplate *template.Template
	SkipValidation bool
}

// NewResolver builds a Resolver for t. When skipValidation is set, rendered
// queries are returned without being parsed as JSON, which a benchmark run
// wants once its templates are known to be correct.
func NewResolver(t Template, rng *rand.Rand, skipValidation bool) (Resolver, error) {
	frs := make(map[string]*fieldResolver)
	for k, v := range t.Bindings {
		rng2 := rand.New(rand.NewSource(rng.Int63()))
		terms, err := v.Terms()
		if err != nil {
			return Resolver{}, err
		}
		// A binding with nothing to hand out cannot render a query, and the
		// resolver would panic on the first one. Fail here, naming the binding,
		// rather than part-way into a run.
		if len(terms) == 0 {
			if v.Path != "" {
				return Resolver{}, fmt.Errorf("binding %q has no terms: %s is empty", k, v.Path)
			}
			return Resolver{}, fmt.Errorf("binding %q has no terms", k)
		}
		frs[k] = newFieldResolver(rng2, terms, v.SplitOn)
	}

	// Parsed once here, then reused for every query this resolver renders.
	funcMap := template.FuncMap{
		"toJson": func(v interface{}) (string, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
	}
	parsedTmpl, err := template.New(t.Name).Funcs(funcMap).Parse(string(t.Template))
	if err != nil {
		return Resolver{}, err
	}

	return Resolver{
		FieldResolvers: frs,
		Template:       t,
		ParsedTemplate: parsedTmpl,
		SkipValidation: skipValidation,
	}, nil
}

// Query renders the next query, drawing one value from each binding.
func (r *Resolver) Query() Query {
	vars := make(map[string]any)
	for k, v := range r.FieldResolvers {
		if v.SplitOn == "" {
			vars[k] = v.next()
		} else {
			vars[k] = strings.Split(v.next(), v.SplitOn)
		}
	}
	return Query{
		Template:       r.Template,
		ParsedTemplate: r.ParsedTemplate,
		Vars:           vars,
		SkipValidation: r.SkipValidation,
	}
}

func newFieldResolver(rng *rand.Rand, terms []string, split string) *fieldResolver {
	return &fieldResolver{
		Rng:     rng,
		Terms:   terms,
		Indices: rng.Perm(len(terms)),
		Index:   0,
		SplitOn: split,
	}
}

// next returns the next term, reshuffling once every term has been used.
func (fr *fieldResolver) next() string {
	if fr.Index == len(fr.Terms) {
		fr.Index = 0
		fr.Indices = fr.Rng.Perm(len(fr.Terms))
	}
	t := fr.Terms[fr.Indices[fr.Index]]
	fr.Index++
	return t
}
