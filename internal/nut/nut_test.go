package nut

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
)

func TestWords(t *testing.T) {
	got := words(`VAR myups ups.mfr "American Power \"Conversion\" \\ Corp"`)
	want := []string{"VAR", "myups", "ups.mfr", `American Power "Conversion" \ Corp`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := words(`VAR u x ""`); len(got) != 4 || got[3] != "" {
		t.Fatalf("empty value lost: %q", got)
	}
}

func TestParseURL(t *testing.T) {
	for _, c := range []struct {
		in, addr, ups string
		bad           bool
	}{
		{"nut://192.168.1.10", "192.168.1.10:3493", "", false},
		{"nut://pi.local:3500/myups", "pi.local:3500", "myups", false},
		{"http://192.168.1.10", "", "", true},
		{"nut://", "", "", true},
		{"nut://host/my%20ups", "", "", true},
	} {
		addr, ups, err := ParseURL(c.in)
		if (err != nil) != c.bad || addr != c.addr || ups != c.ups {
			t.Errorf("%q: got %q %q %v", c.in, addr, ups, err)
		}
	}
}

// fakeUPSD answers like upsd does for the commands the client sends.
func fakeUPSD(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					switch sc.Text() {
					case "LIST UPS":
						c.Write([]byte("BEGIN LIST UPS\nUPS myups \"UPS APC via USB\"\nEND LIST UPS\n"))
					case "LIST VAR myups":
						c.Write([]byte("BEGIN LIST VAR myups\nVAR myups battery.charge \"100\"\nVAR myups ups.status \"OL CHRG\"\nVAR myups ups.model \"Back-UPS ES 700G\"\nEND LIST VAR myups\n"))
					case "LIST CLIENT myups":
						c.Write([]byte("BEGIN LIST CLIENT myups\nCLIENT myups ::1\nCLIENT myups 192.168.1.20\nEND LIST CLIENT myups\n"))
					case "LOGOUT":
						return
					default:
						c.Write([]byte("ERR UNKNOWN-UPS\n"))
					}
				}
			}()
		}
	}()
	return l.Addr().String()
}

func TestFetch(t *testing.T) {
	addr := fakeUPSD(t)
	r := New().Fetch(context.Background(), Target{URL: "nut://" + addr})
	if !r.Online || r.Error != "" {
		t.Fatalf("not online: %+v", r)
	}
	d := r.Data
	if d.UPS != "myups" || d.Vars["battery.charge"] != "100" || d.Vars["ups.model"] != "Back-UPS ES 700G" {
		t.Fatalf("bad data: %+v", d)
	}
	if len(d.Clients) != 2 || d.Clients[0] != "127.0.0.1" || d.Clients[1] != "192.168.1.20" {
		t.Fatalf("bad clients: %v", d.Clients)
	}
}

func TestFetchUnknownUPS(t *testing.T) {
	addr := fakeUPSD(t)
	r := New().Fetch(context.Background(), Target{URL: "nut://" + addr + "/nope"})
	if r.Online || !strings.Contains(r.Error, "UPS name") {
		t.Fatalf("expected an unknown UPS error: %+v", r)
	}
}

func TestFetchUnreachable(t *testing.T) {
	r := New().Fetch(context.Background(), Target{URL: "nut://127.0.0.1:1"})
	if r.Online || r.Error == "" {
		t.Fatalf("expected offline: %+v", r)
	}
}
