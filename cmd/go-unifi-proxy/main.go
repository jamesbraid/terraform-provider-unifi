// Command go-unifi-proxy builds the file:// module proxy the pipelines download
// the pinned go-unifi from, and refuses if the source tree is not the reviewed
// commit.
//
// It replaces .woodpecker/scripts/bootstrap-go-unifi-proxy.sh, and its
// self-test replaces bootstrap-go-unifi-proxy_test.sh -- 150 lines between them.
//
// Usage, as a drop-in for `bash .woodpecker/scripts/bootstrap-go-unifi-proxy.sh`:
//
//	go run ./cmd/go-unifi-proxy
//
// Nothing is printed on success, so a pipeline step stays quiet when it worked.
// Every refusal names what it wanted and what it found, which the shell version
// could not: it asserted with bare `test` under set -e, so a failure ended the
// step in silence at no particular line.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/gounifipin"
)

func main() {
	root := flag.String("repository-root", ".", "the repository whose go.mod declares the version to serve")
	flag.Parse()

	options, err := gounifipin.Options(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := gounifipin.BuildProxy(options); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
