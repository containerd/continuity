package commands

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/containerd/continuity"
)

func readManifest(path string) (*continuity.Manifest, error) {
	p, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading manifest: %v", err)
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".json" {
		return continuity.UnmarshalJSON(p)
	}
	return continuity.Unmarshal(p)
}
