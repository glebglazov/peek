package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// An Endpoint separates the address peek binds to from the URL it hands to
// other people. The two differ on purpose: a Tailscale share binds to the
// tailnet address only, and is reached by a name the LAN cannot resolve.
type Endpoint struct {
	ListenAddr string
	BaseURL    string
	// TLS is nil when the share is served over plain HTTP.
	TLS *tls.Config
}

func lanEndpoint(port int) Endpoint {
	return Endpoint{
		ListenAddr: fmt.Sprintf(":%d", port),
		BaseURL:    "http://" + net.JoinHostPort(lanAddress(), strconv.Itoa(port)),
	}
}

// tailscaleEndpoint keeps the share inside the tailnet: it binds to the
// Tailscale address alone, so a machine on the same Wi-Fi that is not on the
// tailnet gets no answer at all. The MagicDNS name is a real name, so unlike
// the LAN address it can carry a certificate that browsers already trust.
func tailscaleEndpoint(port int, certDir string, secure bool) (Endpoint, error) {
	name, address, err := tailscaleSelf()
	if err != nil {
		return Endpoint{}, err
	}

	endpoint := Endpoint{
		ListenAddr: net.JoinHostPort(address, strconv.Itoa(port)),
		BaseURL:    "http://" + net.JoinHostPort(name, strconv.Itoa(port)),
	}
	if !secure {
		return endpoint, nil
	}

	certificate, err := tailscaleCertificate(name, certDir)
	if err != nil {
		return Endpoint{}, err
	}
	endpoint.BaseURL = "https://" + net.JoinHostPort(name, strconv.Itoa(port))
	endpoint.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	return endpoint, nil
}

// tailscaleCertificate asks Tailscale for a Let's Encrypt certificate for this
// node. Tailscale keeps its own copy and hands back the same one until it
// nears expiry, so asking on every start is how peek renews.
func tailscaleCertificate(name, certDir string) (tls.Certificate, error) {
	binary, err := tailscaleBinary()
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.MkdirAll(certDir, 0o700); err != nil {
		return tls.Certificate{}, fmt.Errorf("create %s: %w", certDir, err)
	}

	certFile := filepath.Join(certDir, name+".crt")
	keyFile := filepath.Join(certDir, name+".key")
	issue := exec.Command(binary, "cert", "--cert-file", certFile, "--key-file", keyFile, name)
	if output, err := issue.CombinedOutput(); err != nil {
		return tls.Certificate{}, fmt.Errorf(
			"tailscale could not issue a certificate for %s: %s\n"+
				"turn on HTTPS Certificates in the tailnet admin console, or serve plain HTTP with --http",
			name, strings.TrimSpace(string(output)))
	}
	return tls.LoadX509KeyPair(certFile, keyFile)
}

// lanAddress is the address to hand to somebody else on the network. The
// loopback address works only on this machine, so it is never the answer.
func lanAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "localhost"
	}
	for _, addr := range addrs {
		network, ok := addr.(*net.IPNet)
		if !ok || network.IP.IsLoopback() || network.IP.To4() == nil {
			continue
		}
		return network.IP.String()
	}
	return "localhost"
}

// tailscaleSelf reads this node's MagicDNS name and IPv4 address from the
// Tailscale CLI, which is the only source that stays correct when the tailnet
// or the machine name changes.
func tailscaleSelf() (name, address string, err error) {
	binary, err := tailscaleBinary()
	if err != nil {
		return "", "", err
	}

	output, err := exec.Command(binary, "status", "--json").Output()
	if err != nil {
		return "", "", fmt.Errorf("ask tailscale for this node: %w", err)
	}

	var status struct {
		Self struct {
			DNSName      string   `json:"DNSName"`
			TailscaleIPs []string `json:"TailscaleIPs"`
			Online       bool     `json:"Online"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(output, &status); err != nil {
		return "", "", fmt.Errorf("read tailscale status: %w", err)
	}

	name = strings.TrimSuffix(status.Self.DNSName, ".")
	if name == "" {
		return "", "", errors.New("tailscale reports no MagicDNS name for this machine; is it logged in and is MagicDNS on?")
	}
	for _, ip := range status.Self.TailscaleIPs {
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
			return name, ip, nil
		}
	}
	return "", "", errors.New("tailscale reports no IPv4 address for this machine")
}

func tailscaleBinary() (string, error) {
	if path, err := exec.LookPath("tailscale"); err == nil {
		return path, nil
	}
	// The macOS app ships its CLI inside the bundle and does not always put it
	// on the PATH.
	const bundled = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
	if _, err := exec.LookPath(bundled); err == nil {
		return bundled, nil
	}
	return "", errors.New("tailscale is not installed")
}
