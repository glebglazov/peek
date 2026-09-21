package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

const usage = `peek shares single files over the local network.

  peek serve [--port N] [--tailscale]
                            run the server; keep this running
                            --tailscale serves HTTPS on your MagicDNS name,
                            reachable from your tailnet only; add --http for
                            plain HTTP
  peek add <file> [alias]   share a file, named after it unless you say otherwise
  peek rm <alias>           stop sharing
  peek list | ls            show what is shared
  peek version              show the installed version
`

// version is set by the Makefile from git describe.
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "peek: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	command, args := args[0], args[1:]
	switch command {
	case "serve":
		return serve(args)
	case "add":
		if len(args) == 0 || len(args) > 2 {
			return errors.New("usage: peek add <file> [alias]")
		}
		alias := ""
		if len(args) == 2 {
			alias = args[1]
		}
		return report(request{Command: "add", Path: mustAbs(args[0]), Alias: alias})
	case "rm":
		if len(args) != 1 {
			return errors.New("usage: peek rm <alias>")
		}
		return report(request{Command: "rm", Alias: args[0]})
	case "list", "ls":
		return report(request{Command: "list"})
	case "version", "--version":
		fmt.Println(version)
		return nil
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q; run peek --help", command)
	}
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := flags.Int("port", 8080, "port to listen on")
	viaTailscale := flags.Bool("tailscale", false, "serve on this machine's Tailscale name, and only to the tailnet")
	plainHTTP := flags.Bool("http", false, "with --tailscale, serve plain HTTP instead of HTTPS")
	if err := flags.Parse(args); err != nil {
		return err
	}

	state, err := stateDir()
	if err != nil {
		return err
	}
	registry, err := LoadRegistry(filepath.Join(state, "registry.json"))
	if err != nil {
		return err
	}

	endpoint := lanEndpoint(*port)
	if *viaTailscale {
		endpoint, err = tailscaleEndpoint(*port, filepath.Join(state, "certs"), !*plainHTTP)
		if err != nil {
			return err
		}
	}

	control := &Control{registry: registry, baseURL: endpoint.BaseURL}
	controlSocket, err := control.Listen(filepath.Join(state, "control.sock"))
	if err != nil {
		return err
	}
	shares, err := net.Listen("tcp", endpoint.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", endpoint.ListenAddr, err)
	}
	if endpoint.TLS != nil {
		shares = tls.NewListener(shares, endpoint.TLS)
	}

	fmt.Printf("peek serves on %s\n", endpoint.BaseURL)
	for _, share := range registry.List() {
		fmt.Printf("  %s\n", shareURL(endpoint.BaseURL, share.Alias))
	}

	failed := make(chan error, 2)
	go func() { failed <- control.Accept(controlSocket) }()
	go func() { failed <- http.Serve(shares, shareHandler(registry)) }()
	return <-failed
}

// report runs one CLI command against the daemon and prints what it says.
func report(req request) error {
	state, err := stateDir()
	if err != nil {
		return err
	}

	lines, err := ask(filepath.Join(state, "control.sock"), req)
	if err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	return nil
}

// The CLI resolves the path, because the daemon may run from another
// directory and a relative path would mean something else there.
func mustAbs(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absolute
}

func stateDir() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config directory: %w", err)
	}
	return filepath.Join(config, "peek"), nil
}
