// Command page prints whether the web UI it embeds is built, then its
// index.html, for TestAPlainGoBuildCarriesThePlaceholder.
package main

import (
	"fmt"
	"io/fs"
	"os"

	"github.com/cieons/koko/internal/server/webui"
)

func main() {
	page, err := fs.ReadFile(webui.FS(), "index.html")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("built=%t\n%s", webui.Built(), page)
}
