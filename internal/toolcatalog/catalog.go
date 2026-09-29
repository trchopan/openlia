package toolcatalog

import "fmt"

var allowed = map[string]bool{
	"media-transcripts": true,
	"ocr":               true,
	"office":            true,
	"pdf":               true,
}

func Validate(values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !allowed[value] {
			return fmt.Errorf("unknown tool capability %q", value)
		}
		if seen[value] {
			return fmt.Errorf("duplicate tool capability %q", value)
		}
		seen[value] = true
	}
	return nil
}
