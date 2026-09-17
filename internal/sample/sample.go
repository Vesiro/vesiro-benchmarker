package sample

import (
	"encoding/json"
	"time"

	"github.com/Vesiro/vesiro-benchmarker/internal/query"
)

type Sample struct {
	Query          query.Query
	ClientDuration time.Duration
	Status         int
	Body           json.RawMessage
}
