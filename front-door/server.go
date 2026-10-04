package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type CLI interface {
	Status(ctx context.Context, name string) (Status, error)
	Create(ctx context.Context, name, owner string) error
	Login(ctx context.Context, name string) error
}

type Identity interface {
	Caller(r *http.Request) (login string, ok bool)
}

type ExecCLI struct {
	Path string
}

func (c ExecCLI) run(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, c.Path, args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("machine %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("machine %s: %w", args[0], err)
	}
	return out, nil
}

func (c ExecCLI) Status(ctx context.Context, name string) (Status, error) {
	out, err := c.run(ctx, "status", name, "--json")
	if err != nil {
		return Status{}, err
	}
	var s Status
	if err := json.Unmarshal(out, &s); err != nil {
		return Status{}, fmt.Errorf("machine status: %w", err)
	}
	return s, nil
}

func (c ExecCLI) Create(ctx context.Context, name, owner string) error {
	_, err := c.run(ctx, "create", name, "--owner", owner)
	return err
}

func (c ExecCLI) Login(ctx context.Context, name string) error {
	_, err := c.run(ctx, "login", name)
	return err
}

//go:embed templates/page.html
var templateFS embed.FS

var pageTemplate = template.Must(template.ParseFS(templateFS, "templates/page.html"))

const loginInterval = time.Minute

type server struct {
	cli CLI
	id  Identity
	key []byte
	now func() time.Time

	mu        sync.Mutex
	lastLogin map[string]time.Time
}

type page struct {
	State   PageState
	Login   string
	Name    string
	Token   string
	Status  Status
	Message string
	Refresh bool
}

func NewServer(cli CLI, id Identity, key []byte, now func() time.Time) http.Handler {
	s := &server{cli: cli, id: id, key: key, now: now, lastLogin: map[string]time.Time{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("POST /create", s.create)
	mux.HandleFunc("POST /login", s.login)
	return mux
}

func (s *server) token(login string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(login))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *server) caller(w http.ResponseWriter, r *http.Request) (login, name string, ok bool) {
	login, ok = s.id.Caller(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", "", false
	}
	name, err := MachineName(login)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		s.render(w, page{State: StateConflict, Login: login, Message: fmt.Sprintf("No machine can be made for %s: %v.", login, err)})
		return "", "", false
	}
	return login, name, true
}

func (s *server) checkToken(w http.ResponseWriter, r *http.Request, login string) bool {
	if !hmac.Equal([]byte(r.PostFormValue("token")), []byte(s.token(login))) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (s *server) maybeLogin(ctx context.Context, name string) error {
	s.mu.Lock()
	last, seen := s.lastLogin[name]
	now := s.now()
	if seen && now.Sub(last) < loginInterval {
		s.mu.Unlock()
		return nil
	}
	s.lastLogin[name] = now
	s.mu.Unlock()
	return s.cli.Login(ctx, name)
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	login, name, ok := s.caller(w, r)
	if !ok {
		return
	}
	p := page{Login: login, Name: name, Token: s.token(login)}
	status, err := s.cli.Status(r.Context(), name)
	if err != nil {
		log.Printf("status %s: %v", name, err)
		p.Message = err.Error()
		s.render(w, p)
		return
	}
	p.Status = status
	p.State = PageStateFor(login, status)
	if p.State == StateLogin {
		if err := s.maybeLogin(r.Context(), name); err != nil {
			log.Printf("login %s: %v", name, err)
			p.Message = err.Error()
		}
	}
	p.Refresh = p.State == StateBooting || p.State == StateLogin || p.State == StateClaim
	s.render(w, p)
}

func (s *server) create(w http.ResponseWriter, r *http.Request) {
	login, name, ok := s.caller(w, r)
	if !ok || !s.checkToken(w, r, login) {
		return
	}
	status, err := s.cli.Status(r.Context(), name)
	if err == nil && !status.Exists {
		if err := s.cli.Create(r.Context(), name, login); err != nil {
			log.Printf("create %s: %v", name, err)
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	login, name, ok := s.caller(w, r)
	if !ok || !s.checkToken(w, r, login) {
		return
	}
	status, err := s.cli.Status(r.Context(), name)
	if err == nil {
		switch PageStateFor(login, status) {
		case StateLogin, StateClaim:
			s.mu.Lock()
			s.lastLogin[name] = s.now()
			s.mu.Unlock()
			if err := s.cli.Login(r.Context(), name); err != nil {
				log.Printf("login %s: %v", name, err)
			}
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) render(w http.ResponseWriter, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTemplate.Execute(w, p); err != nil {
		log.Printf("render: %v", err)
	}
}
