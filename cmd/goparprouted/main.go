package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ansiwen/goparprouted/pkg/arp"
)

var (
	debug     bool
	arpperm   bool
	defaultrt bool
)

func main() {
	log.SetOutput(os.Stdout)

	flag.BoolVar(&debug, "d", false, "Enable debug mode")
	flag.BoolVar(&arpperm, "p", false, "Make ARP entries permanent")
	flag.BoolVar(&defaultrt, "r", false, "First interface has default route")
	flag.Parse()
	interfaces := flag.Args()

	if len(interfaces) == 0 {
		fmt.Println("Usage: goparprouted [-d] [-p] [-r] interface [interface ...]")
		os.Exit(1)
	}

	log.Println("Starting parprouted")

	arpProxy := arp.NewARPProxy()
	arpProxy.SetDebug(debug)
	arpProxy.SetDefaultRt(defaultrt)
	arpProxy.SetARPPerm(arpperm)

	for _, ifaceName := range interfaces {
		iface, err := net.InterfaceByName(ifaceName)
		if err != nil {
			log.Fatalf("failed to get interface %s: %v", ifaceName, err)
		}

		arpProxy.AddInterface(iface)
	}

	arpProxy.Start()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Println("Shutting down goparprouted")
	arpProxy.Cleanup()
}
