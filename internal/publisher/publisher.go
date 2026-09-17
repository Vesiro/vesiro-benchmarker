package publisher

import (
	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

type Publisher interface {
	Start() error
	Finish() error
	Publish(sample.Sample) error
}
