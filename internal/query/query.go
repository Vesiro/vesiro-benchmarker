package query

import (
	"bytes"
	"encoding/json"
	"sync"
	"text/template"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

type Query struct {
	Template       Template
	ParsedTemplate *template.Template
	Vars           map[string]any
	SkipValidation bool
}

func (q Query) Json() (json.RawMessage, error) {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	if err := q.ParsedTemplate.Execute(buf, q.Vars); err != nil {
		return nil, err
	}

	resultBytes := buf.Bytes()
	resultBytes = stripRawJSONMarkersInPlace(resultBytes)

	if q.SkipValidation {
		result := make([]byte, len(resultBytes))
		copy(result, resultBytes)
		return json.RawMessage(result), nil
	}

	// Parse JSON to validate the rendered payload.
	var obj any
	if err := json.Unmarshal(resultBytes, &obj); err != nil {
		return nil, err
	}

	result := make([]byte, len(resultBytes))
	copy(result, resultBytes)
	return json.RawMessage(result), nil
}

// stripRawJSONMarkersInPlace removes raw JSON markers
func stripRawJSONMarkersInPlace(input []byte) []byte {
	if !bytes.Contains(input, []byte("___RAWJSON___")) {
		return input
	}

	result := bytes.ReplaceAll(input, []byte(`"___RAWJSON___`), nil)
	result = bytes.ReplaceAll(result, []byte(`___RAWJSON___"`), nil)
	return result
}
