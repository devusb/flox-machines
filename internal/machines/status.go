package machines

import (
	"encoding/json"
	"strconv"
	"strings"
)

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
	Tailscale *TailscaleStatus `json:"tailscale,omitempty"`
}

func (s Status) MarshalJSON() ([]byte, error) {
	if !s.Exists {
		return json.Marshal(struct {
			Name   string `json:"name"`
			Exists bool   `json:"exists"`
		}{s.Name, false})
	}
	type plain Status
	return json.Marshal(plain(s))
}

func ParseTailscaleStatus(raw []byte) (TailscaleStatus, error) {
	var st struct {
		BackendState string
		AuthURL      string
		Self         *struct {
			DNSName string
			UserID  int64
		}
		User map[string]struct {
			LoginName string
		}
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return TailscaleStatus{}, err
	}
	out := TailscaleStatus{State: st.BackendState, AuthURL: st.AuthURL}
	if st.Self != nil {
		out.DNSName = strings.TrimRight(st.Self.DNSName, ".")
		if st.Self.UserID != 0 {
			out.Owner = st.User[strconv.FormatInt(st.Self.UserID, 10)].LoginName
		}
	}
	return out, nil
}
