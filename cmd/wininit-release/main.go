// wininit-release is a builder-only inventory tool, never an installed agent plugin.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/randomtoy/wininit/internal/releasebundle"
)

func main() {
	dir := flag.String("dist", "dist", "artifact directory")
	version := flag.String("version", "", "release version")
	commit := flag.String("commit", "", "source commit")
	signature := flag.String("signature", "unsigned", "packager-verified Authenticode status")
	clean := flag.Bool("require-clean", false, "reject modified source checkouts")
	verify := flag.Bool("verify", false, "verify existing inventory and checksums")
	flag.Parse()
	var err error
	if *verify {
		err = releasebundle.Verify(*dir, *clean)
	} else {
		err = releasebundle.Generate(*dir, *version, *commit, *signature, *clean)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
