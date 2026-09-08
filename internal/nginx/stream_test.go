package nginx

import (
	"strings"
	"testing"

	"github.com/hnrobert/sslly-nginx/internal/config"
)

// TestGenerateConfig_StreamOrderDeterministic: stream upstream/listen blocks
// follow proxy.yaml declaration order and stay stable across generations
// (map iteration must not reshuffle them).
func TestGenerateConfig_StreamOrderDeterministic(t *testing.T) {
	cfg := &config.Config{
		CORS: map[string]config.CORSConfig{},
		Ports: map[string][]string{
			"<tcp>9301": {"8301"},
			"<tcp>9302": {"8302"},
			"<tcp>9303": {"8303"},
		},
		OrderedPorts: []string{"<tcp>9302", "<tcp>9303", "<tcp>9301"},
	}

	var first string
	for i := 0; i < 10; i++ {
		ng := GenerateConfig(cfg, nil)
		if i == 0 {
			first = ng
			continue
		}
		if ng != first {
			t.Fatalf("stream generation not deterministic across runs (run %d)", i)
		}
	}

	// The stream section lists upstreams in OrderedPorts order.
	i1 := strings.Index(first, "upstream stream_tcp_9302")
	i2 := strings.Index(first, "upstream stream_tcp_9303")
	i3 := strings.Index(first, "upstream stream_tcp_9301")
	if !(i1 >= 0 && i2 > i1 && i3 > i2) {
		t.Fatalf("stream upstreams not in declaration order: %d %d %d\n%s", i1, i2, i3, first)
	}
}
