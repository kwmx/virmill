// virmill-dev is build tooling; it is not installed as a public product command.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"virmill.local/core/internal/plugins"
)

func main() {
	var err error
	switch {
	case len(os.Args) == 5 && os.Args[1] == "scaffold":
		err = plugins.Scaffold(os.Args[2], os.Args[3], os.Args[4])
	case len(os.Args) == 6 && os.Args[1] == "pack":
		key, e := os.ReadFile(os.Args[3])
		if e != nil {
			err = e
			break
		}
		private, e := hex.DecodeString(string(key))
		if e != nil {
			err = e
			break
		}
		output, e := os.OpenFile(os.Args[5], os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			err = e
			break
		}
		_, err = plugins.Pack(os.Args[2], ed25519.PrivateKey(private), os.Args[4], output)
		if err == nil {
			err = output.Sync()
		}
		output.Close()
	default:
		err = fmt.Errorf("usage: virmill-dev scaffold DEST SDK_DIR PLUGIN_ID | pack DIR KEY_HEX_FILE KEY_ID OUTPUT")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
