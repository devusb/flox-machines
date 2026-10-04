package main

type TailscaleStatus struct {
	State   string `json:"state"`
	AuthURL string `json:"authURL"`
	DNSName string `json:"dnsName"`
	Owner   string `json:"owner"`
}

type Status struct {
	Name      string           `json:"name"`
	Exists    bool             `json:"exists"`
	Owner     string           `json:"owner"`
	Running   bool             `json:"running"`
	Reachable bool             `json:"reachable"`
	Tailscale *TailscaleStatus `json:"tailscale"`
}

type PageState string

const (
	StateNone       PageState = "none"
	StateBooting    PageState = "booting"
	StateLogin      PageState = "login"
	StateClaim      PageState = "claim"
	StateReady      PageState = "ready"
	StateWrongOwner PageState = "wrong-owner"
	StateConflict   PageState = "conflict"
)

func PageStateFor(caller string, s Status) PageState {
	switch {
	case !s.Exists:
		return StateNone
	case s.Owner != caller:
		return StateConflict
	case s.Tailscale == nil:
		return StateBooting
	case s.Tailscale.State == "NeedsLogin" && s.Tailscale.AuthURL == "":
		return StateLogin
	case s.Tailscale.State == "NeedsLogin":
		return StateClaim
	case s.Tailscale.State == "Running" && s.Tailscale.Owner == caller:
		return StateReady
	case s.Tailscale.State == "Running":
		return StateWrongOwner
	default:
		return StateBooting
	}
}
