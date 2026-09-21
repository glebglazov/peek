# peek

Share single files over your local network, or over your tailnet. peek serves
only the files you register. Everything else on the disk stays out of reach.

## Use

Start the server and keep it running:

    peek serve                    # HTTP on your LAN address
    peek serve --tailscale        # HTTPS on your MagicDNS name, tailnet only
    peek serve --tailscale --http # the same, without a certificate
    peek serve --port 9000

Register files from another terminal. peek prints the URL to send:

    peek add ~/report.html            # http://192.168.2.66:8080/report.html
    peek add ~/work/report.html spec  # http://192.168.2.66:8080/spec
    peek list
    peek rm spec

The root URL shows an index of everything shared, newest change first,
with the time each file last changed.

## How it works

The server owns the share list. The CLI talks to it over a Unix socket in
`~/Library/Application Support/peek`, where the list is also stored, so shares
survive a restart of the server.

`--tailscale` binds to this machine's Tailscale address only. A machine on the
same Wi-Fi that is not on your tailnet gets no answer.

It also serves HTTPS, because a MagicDNS name can hold a certificate that
browsers already trust. On start, peek asks `tailscale cert` for one and keeps
it in `certs/` next to the registry. Tailscale hands back the same certificate
until it nears expiry, so a restart is how peek renews. This needs HTTPS
Certificates to be on in the tailnet admin console; without it, use `--http`.

The LAN mode stays on HTTP. A certificate for `192.168.2.66` cannot be signed
by an authority the other machine trusts, so HTTPS there would only show your
colleague a browser warning.

## Install

    make install       # builds and copies to ~/.local/bin
    make install-dev   # the same, built with the race detector

`PREFIX` chooses where it lands. `make test` and `make test-race` run the
tests.
