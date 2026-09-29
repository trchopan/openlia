package toolcatalog

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

var allowed = map[string]bool{
	"media-transcripts": true,
	"ocr":               true,
	"office":            true,
	"pdf":               true,
}

const DefaultDebianSnapshot = "20260505T000000Z"

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

func Canonical(values []string) ([]string, error) {
	if err := Validate(values); err != nil {
		return nil, err
	}
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result, nil
}

// ManagedHermesImage returns a project-specific image reference so separate
// deployments on one Docker host cannot overwrite each other's tool images.
func ManagedHermesImage(project string, tools []string, baseTag, baseDigest, debianSnapshot string) string {
	canonical, _ := Canonical(tools)
	identity := strings.Join([]string{project, baseTag, baseDigest, debianSnapshot, strings.Join(canonical, ",")}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	displayProject := project
	if len(displayProject) > 80 {
		displayProject = displayProject[:80]
	}
	return fmt.Sprintf("openlia-hermes:%s-tools-%x", displayProject, digest[:8])
}
