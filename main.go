package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/Protarium-Network/super-mario-maker-nex-server/nex_smm"
	"github.com/Protarium-Network/super-mario-maker-nex-server/smmadmin"
)

var serverBuildString = "development"

func main() {
	if err := configure(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	nex_smm.StartPickupServer()
	go runServer("authentication", nex_smm.StartAuthenticationServer)
	go runServer("secure", nex_smm.StartSecureServer)
	go runServer("admin", smmadmin.Start)

	select {}
}

func runServer(name string, server func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			fmt.Fprintf(os.Stderr, "%s server crashed: %v\n%s", name, recovered, debug.Stack())
			os.Exit(1)
		}
	}()
	server()
}
