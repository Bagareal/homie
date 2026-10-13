// Package nut reads a UPS from a Network UPS Tools server (upsd, TCP port 3493).
// The protocol is plain text and reading needs no login: every question is one
// line ("LIST VAR myups") and the answer is a BEGIN ... END block.
package nut

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// DefaultPort is where upsd listens.
const DefaultPort = "3493"

const timeout = 6 * time.Second

// Target is one upsd server. URL is nut://host[:port][/ups]; without a UPS name
// the first one the server lists is used.
type Target struct {
	URL string
}

// Data is what the server told us about one UPS.
type Data struct {
	// UPS is the one that was read; List holds every UPS the server has.
	UPS  string   `json:"ups"`
	List []string `json:"upsList,omitempty"`
	// Vars are all the variables of the UPS (battery.charge, ups.status, ...).
	Vars map[string]string `json:"vars"`
	// Clients are the addresses of the machines connected to this UPS, primary and
	// secondary alike: upsd does not say which is which.
	Clients []string `json:"clients"`
}

// Result is what the dashboard receives for one widget.
type Result struct {
	Online    bool              `json:"online"`
	Error     string            `json:"error,omitempty"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Data      Data              `json:"data"`
	Failed    []string          `json:"failed,omitempty"`
	Notes     map[string]string `json:"notes,omitempty"`
}

// Client reads upsd servers. The zero value is ready to use.
type Client struct{}

func New() *Client { return &Client{} }

// ParseURL splits nut://host[:port][/ups] into an address and a UPS name.
func ParseURL(raw string) (addr, ups string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "nut" || u.Hostname() == "" {
		return "", "", errors.New("invalid address: use nut://host or nut://host:3493")
	}
	port := u.Port()
	if port == "" {
		port = DefaultPort
	}
	ups = strings.Trim(u.Path, "/")
	if !upsName.MatchString(ups) {
		return "", "", errors.New("invalid UPS name in the address")
	}
	return net.JoinHostPort(u.Hostname(), port), ups, nil
}

// upsName is what upsd allows in a UPS name; it is also what keeps a typed name
// from smuggling a second command into the conversation.
var upsName = regexp.MustCompile(`^[A-Za-z0-9._@-]{0,64}$`)

// Ping connects and lists the UPS units, to test an address.
func (c *Client) Ping(ctx context.Context, t Target) ([]string, error) {
	s, _, err := open(ctx, t)
	if err != nil {
		return nil, err
	}
	defer s.close()
	return s.upsList()
}

// Fetch reads the variables and the clients of the UPS. It never fails half way:
// an unreachable server gives Online=false, a UPS that cannot be read is an error.
func (c *Client) Fetch(ctx context.Context, t Target) Result {
	res := Result{UpdatedAt: time.Now().UTC(), Data: Data{Vars: map[string]string{}, Clients: []string{}}}
	s, name, err := open(ctx, t)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer s.close()
	list, err := s.upsList()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Data.List = list
	if name == "" {
		if len(list) == 0 {
			res.Error = "the server has no UPS configured"
			return res
		}
		name = list[0]
	}
	res.Data.UPS = name
	vars, err := s.vars(name)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Online = true
	res.Data.Vars = vars
	clients, err := s.clients(name)
	if err != nil {
		res.Failed = append(res.Failed, "clients")
		res.Notes = map[string]string{"clients": err.Error()}
	} else {
		res.Data.Clients = clients
	}
	return res
}

type session struct {
	conn net.Conn
	r    *bufio.Reader
}

func open(ctx context.Context, t Target) (*session, string, error) {
	addr, ups, err := ParseURL(t.URL)
	if err != nil {
		return nil, "", err
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, "", fmt.Errorf("cannot reach %s: %s", addr, trimDial(err))
	}
	// One deadline for the whole conversation.
	_ = conn.SetDeadline(time.Now().Add(timeout))
	return &session{conn: conn, r: bufio.NewReader(conn)}, ups, nil
}

func trimDial(err error) string {
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		return op.Err.Error()
	}
	return err.Error()
}

func (s *session) close() {
	_, _ = s.conn.Write([]byte("LOGOUT\n"))
	_ = s.conn.Close()
}

// list sends a LIST command and returns the lines between BEGIN and END, split
// into words (quoted words keep their spaces).
func (s *session) list(what string) ([][]string, error) {
	if _, err := fmt.Fprintf(s.conn, "LIST %s\n", what); err != nil {
		return nil, err
	}
	var rows [][]string
	begun := false
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("the server stopped answering: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "ERR "):
			return nil, errors.New(describe(strings.TrimPrefix(line, "ERR ")))
		case strings.HasPrefix(line, "BEGIN LIST "):
			begun = true
		case strings.HasPrefix(line, "END LIST "):
			return rows, nil
		case begun:
			rows = append(rows, words(line))
		}
	}
}

func describe(code string) string {
	switch strings.Fields(code + " ")[0] {
	case "UNKNOWN-UPS":
		return "the server does not know that UPS name"
	case "ACCESS-DENIED":
		return "the server refuses this machine (check LISTEN and the access rules of upsd)"
	case "DATA-STALE":
		return "the server lost contact with the UPS"
	case "DRIVER-NOT-CONNECTED":
		return "the UPS driver is not running on the server"
	}
	return "the server answered: " + code
}

func (s *session) upsList() ([]string, error) {
	rows, err := s.list("UPS")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows { // UPS <name> "<description>"
		if len(r) >= 2 && r[0] == "UPS" {
			out = append(out, r[1])
		}
	}
	return out, nil
}

func (s *session) vars(ups string) (map[string]string, error) {
	rows, err := s.list("VAR " + ups)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows { // VAR <ups> <name> "<value>"
		if len(r) >= 4 && r[0] == "VAR" {
			out[r[2]] = r[3]
		}
	}
	return out, nil
}

func (s *session) clients(ups string) ([]string, error) {
	rows, err := s.list("CLIENT " + ups)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, r := range rows { // CLIENT <ups> <address>
		if len(r) >= 3 && r[0] == "CLIENT" {
			out = append(out, normalizeAddr(r[2]))
		}
	}
	return out, nil
}

// normalizeAddr writes the machine talking to itself the same way whatever the
// server reports.
func normalizeAddr(a string) string {
	a = strings.TrimPrefix(strings.ToLower(a), "::ffff:")
	if a == "::1" || a == "localhost" {
		return "127.0.0.1"
	}
	return a
}

// words splits a protocol line on spaces; a word in double quotes may contain
// spaces, and inside it \" and \\ stand for " and \.
func words(line string) []string {
	var out []string
	var cur strings.Builder
	quoted, have := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quoted && c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
		case c == '"':
			quoted, have = !quoted, true
		case c == ' ' && !quoted:
			if have || cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteByte(c)
		}
	}
	if have || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
