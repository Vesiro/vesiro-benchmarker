package query

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkQueryJson measures query generation as the bound ID array grows.
func BenchmarkQueryJson(b *testing.B) {
	sizes := []int{10, 100, 1000, 10000, 50000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("IDs_%d", size), func(b *testing.B) {
			benchmarkQueryJsonWithSize(b, size)
		})
	}
}

func benchmarkQueryJsonWithSize(b *testing.B, numIDs int) {
	tmpDir := b.TempDir()
	idsFile := filepath.Join(tmpDir, "ids.txt")
	templateFile := filepath.Join(tmpDir, "template.json")

	ids := make([]string, numIDs)
	for i := 0; i < numIDs; i++ {
		ids[i] = fmt.Sprintf("%d", 4000000000000+i)
	}
	idsJSON, _ := json.Marshal(ids)

	err := os.WriteFile(idsFile, idsJSON, 0644)
	if err != nil {
		b.Fatalf("failed to write IDs file: %v", err)
	}

	tmplContent := fmt.Sprintf(`{
  "name": "benchmark-template",
  "index": "test-index",
  "description": "Benchmark template for ID array query",
  "bindings": {
    "IDArray": {
      "source": "file",
      "path": "%s"
    }
  },
  "template": {
    "query": {
      "terms": {
        "record_id": "___RAWJSON___{{.IDArray}}___RAWJSON___"
      }
    }
  }
}`, idsFile)

	err = os.WriteFile(templateFile, []byte(tmplContent), 0644)
	if err != nil {
		b.Fatalf("failed to write template file: %v", err)
	}

	tmpl, err := LoadTemplateFromFile(templateFile)
	if err != nil {
		b.Fatalf("failed to load template: %v", err)
	}

	rng := rand.New(rand.NewSource(12345))
	resolver, err := NewResolver(tmpl, rng, false)
	if err != nil {
		b.Fatalf("failed to create resolver: %v", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		q := resolver.Query()
		raw, err := q.Json()
		if err != nil {
			b.Fatalf("failed to generate JSON: %v", err)
		}
		_ = raw
	}

	b.ReportAllocs()
}

// BenchmarkQueryJsonNoRawJSON is the same measurement without raw JSON
// markers, as a baseline for plain string substitution.
func BenchmarkQueryJsonNoRawJSON(b *testing.B) {
	tmpDir := b.TempDir()
	templateFile := filepath.Join(tmpDir, "template.json")

	tmplContent := `{
  "name": "simple-template",
  "index": "test-index",
  "description": "Simple template without raw JSON",
  "bindings": {
    "Term": {
      "source": "inline",
      "terms": ["value1", "value2", "value3"]
    }
  },
  "template": {
    "query": {
      "match": {
        "field": "{{.Term}}"
      }
    }
  }
}`

	err := os.WriteFile(templateFile, []byte(tmplContent), 0644)
	if err != nil {
		b.Fatalf("failed to write template file: %v", err)
	}

	tmpl, err := LoadTemplateFromFile(templateFile)
	if err != nil {
		b.Fatalf("failed to load template: %v", err)
	}

	rng := rand.New(rand.NewSource(12345))
	resolver, err := NewResolver(tmpl, rng, false)
	if err != nil {
		b.Fatalf("failed to create resolver: %v", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		q := resolver.Query()
		raw, err := q.Json()
		if err != nil {
			b.Fatalf("failed to generate JSON: %v", err)
		}
		_ = raw
	}

	b.ReportAllocs()
}

// BenchmarkQueryJsonNoValidation is the same measurement with JSON
// validation skipped, as a benchmark run does.
func BenchmarkQueryJsonNoValidation(b *testing.B) {
	sizes := []int{10, 100, 1000, 10000, 50000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("IDs_%d", size), func(b *testing.B) {
			benchmarkQueryJsonNoValidationWithSize(b, size)
		})
	}
}

func benchmarkQueryJsonNoValidationWithSize(b *testing.B, numIDs int) {
	tmpDir := b.TempDir()
	idsFile := filepath.Join(tmpDir, "ids.txt")
	templateFile := filepath.Join(tmpDir, "template.json")

	ids := make([]string, numIDs)
	for i := 0; i < numIDs; i++ {
		ids[i] = fmt.Sprintf("%d", 4000000000000+i)
	}
	idsJSON, _ := json.Marshal(ids)

	err := os.WriteFile(idsFile, idsJSON, 0644)
	if err != nil {
		b.Fatalf("failed to write IDs file: %v", err)
	}

	tmplContent := fmt.Sprintf(`{
  "name": "benchmark-template",
  "index": "test-index",
  "description": "Benchmark template for ID array query",
  "bindings": {
    "IDArray": {
      "source": "file",
      "path": "%s"
    }
  },
  "template": {
    "query": {
      "terms": {
        "record_id": "___RAWJSON___{{.IDArray}}___RAWJSON___"
      }
    }
  }
}`, idsFile)

	err = os.WriteFile(templateFile, []byte(tmplContent), 0644)
	if err != nil {
		b.Fatalf("failed to write template file: %v", err)
	}

	tmpl, err := LoadTemplateFromFile(templateFile)
	if err != nil {
		b.Fatalf("failed to load template: %v", err)
	}

	rng := rand.New(rand.NewSource(12345))
	resolver, err := NewResolver(tmpl, rng, true)
	if err != nil {
		b.Fatalf("failed to create resolver: %v", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		q := resolver.Query()
		raw, err := q.Json()
		if err != nil {
			b.Fatalf("failed to generate JSON: %v", err)
		}
		_ = raw
	}

	b.ReportAllocs()
}
