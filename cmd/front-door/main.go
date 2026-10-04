package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/tsnet"
)

type tailnetIdentity struct {
	lc *local.Client
}

func (t tailnetIdentity) Caller(r *http.Request) (string, bool) {
	who, err := t.lc.WhoIs(r.Context(), r.RemoteAddr)
	if err != nil || who.Node == nil || who.UserProfile == nil {
		return "", false
	}
	if len(who.Node.Tags) > 0 {
		return "", false
	}
	return who.UserProfile.LoginName, who.UserProfile.LoginName != ""
}

type testIdentity struct{}

func (testIdentity) Caller(r *http.Request) (string, bool) {
	login := r.Header.Get("X-Test-Login")
	return login, login != ""
}

func formKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err == nil && len(key) >= 32 {
		return key, nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return key, os.WriteFile(path, key, 0o600)
}

func main() {
	hostname := flag.String("hostname", "machines", "tailnet node name")
	tags := flag.String("tags", "tag:flox-machines", "comma-separated tags the node advertises")
	secretFile := flag.String("secret-file", "", "file holding an OAuth client secret or auth key")
	stateDir := flag.String("state-dir", "/var/lib/flox-machines-front-door", "tsnet state directory")
	machine := flag.String("machine", "machine", "path to the machine CLI")
	testListen := flag.String("test-listen", "", "for tests: serve plain HTTP on this address with identity from X-Test-Login")
	formKeyFile := flag.String("form-key-file", "", "form token key, created if missing (default <state-dir>/form.key)")
	flag.Parse()

	if *formKeyFile == "" {
		*formKeyFile = filepath.Join(*stateDir, "form.key")
	}
	key, err := formKey(*formKeyFile)
	if err != nil {
		log.Fatalf("form key: %v", err)
	}
	cli := ExecCLI{Path: *machine}

	if *testListen != "" {
		log.Printf("serving test mode on %s", *testListen)
		log.Fatal(http.ListenAndServe(*testListen, NewServer(cli, testIdentity{}, key, time.Now)))
	}

	srv := &tsnet.Server{
		Hostname:      *hostname,
		Dir:           *stateDir,
		AdvertiseTags: strings.Split(*tags, ","),
		UserLogf:      log.Printf,
	}
	if *secretFile != "" {
		raw, err := os.ReadFile(*secretFile)
		if err != nil {
			log.Fatalf("secret: %v", err)
		}
		srv.AuthKey = NodeSecret(string(raw))
	}
	defer srv.Close()

	log.Printf("starting tsnet node %s", *hostname)
	ctx := context.Background()
	if _, err := srv.Up(ctx); err != nil {
		log.Fatalf("tsnet: %v", err)
	}
	lc, err := srv.LocalClient()
	if err != nil {
		log.Fatalf("tsnet local client: %v", err)
	}
	handler := NewServer(cli, tailnetIdentity{lc: lc}, key, time.Now)

	ln, err := listen(ctx, srv, lc)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", ln.Addr())
	log.Fatal(http.Serve(ln, handler))
}

func listen(ctx context.Context, srv *tsnet.Server, lc *local.Client) (net.Listener, error) {
	st, err := lc.StatusWithoutPeers(ctx)
	if err != nil {
		return nil, fmt.Errorf("tsnet status: %w", err)
	}
	if len(st.CertDomains) > 0 {
		return srv.ListenTLS("tcp", ":443")
	}
	return srv.Listen("tcp", ":80")
}
