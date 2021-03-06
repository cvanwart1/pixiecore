package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/danderson/pixiecore/tftp"
)

//go:generate go-bindata -o pxelinux_autogen.go -prefix=pxelinux -ignore=README.md pxelinux

var (
	// I'm sort of giving you the option to change these ports here,
	// but all of them except the HTTP port are hardcoded in the PXE
	// option ROM, so it's pretty pointless unless you'd playing
	// packet rewriting tricks or doing simulations with packet
	// generators.
	portDHCP   = flag.Int("port-dhcp", 67, "Port to listen on for DHCP requests")
	portPXE    = flag.Int("port-pxe", 4011, "Port to listen on for PXE requests")
	portTFTP   = flag.Int("port-tftp", 69, "Port to listen on for TFTP requests")
	portHTTP   = flag.Int("port-http", 70, "Port to listen on for HTTP requests")
	listenAddr = flag.String("listen-addr", "", "Address to listen on (default all)")

	apiServer  = flag.String("api", "", "Path to the boot API server")
	apiTimeout = flag.Duration("api-timeout", 5*time.Second, "Timeout on boot API server requests")

	kernelFile    = flag.String("kernel", "", "Path to the linux kernel file to boot")
	initrdFile    = flag.String("initrd", "", "Comma-separated list of initrds to pass to the kernel")
	kernelCmdline = flag.String("cmdline", "", "Additional arguments for the kernel commandline")

	debug = flag.Bool("debug", false, "Log more things that aren't directly related to booting a recognized client")
)

func pickBooter() (Booter, error) {
	switch {
	case *apiServer != "":
		if *kernelFile != "" {
			return nil, errors.New("cannot provide -kernel with -api")
		}
		if *initrdFile != "" {
			return nil, errors.New("cannot provide -initrd with -api")
		}
		if *kernelCmdline != "" {
			return nil, errors.New("cannot provide -cmdline with -api")
		}

		log.Printf("Starting Pixiecore in API mode, with server %s", *apiServer)
		return RemoteBooter(*apiServer, *apiTimeout)

	case *kernelFile != "":
		if *apiServer != "" {
			return nil, errors.New("cannot provide -api with -kernel")
		}

		log.Printf("Starting Pixiecore in static mode")
		var initrds []string
		if *initrdFile != "" {
			initrds = strings.Split(*initrdFile, ",")
		}
		return StaticBooter(*kernelFile, initrds, *kernelCmdline), nil

	default:
		return nil, errors.New("must specify either -api, or -kernel/-initrd")
	}
}

func main() {
	flag.Parse()

	booter, err := pickBooter()
	if err != nil {
		flag.Usage()
		fmt.Fprintf(os.Stderr, "\nERROR: %s\n", err)
		os.Exit(1)
	}

	pxelinux, err := Asset("lpxelinux.0")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	ldlinux, err := Asset("ldlinux.c32")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	go func() {
		addrDHCP := fmt.Sprintf("%s:%d", *listenAddr, *portDHCP)
		log.Fatalln(serveProxyDHCP(addrDHCP, booter))
	}()
	go func() {
		addrPXE := fmt.Sprintf("%s:%d", *listenAddr, *portPXE)
		log.Fatalln(servePXE(addrPXE, *portHTTP))
	}()
	go func() {
		addrTFTP := fmt.Sprintf("%s:%d", *listenAddr, *portTFTP)
		tftp.Logf = func(msg string, args ...interface{}) { Log("TFTP", msg, args...) }
		tftp.Debug = func(msg string, args ...interface{}) { Debug("TFTP", msg, args...) }
		log.Fatalln(tftp.ListenAndServe("udp4", addrTFTP, tftp.Blob(pxelinux)))
	}()
	go func() {
		log.Fatalln(serveHTTP(*listenAddr, *portHTTP, booter, ldlinux))
	}()
	recordLogs(*debug)
}
