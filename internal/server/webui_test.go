package server_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/webui"
)

// Whoever runs the koko server reads in its log whether it carries the built
// web UI or only the placeholder page.
func TestTheStartLogSaysWhetherTheWebUIIsBuilt(t *testing.T) {
	h := harness.Start(t)
	want := fmt.Sprintf("web_ui_built=%t", webui.Built())
	if !strings.Contains(h.Log(), want) {
		t.Errorf("the start log does not say %s:\n%s", want, h.Log())
	}
}
