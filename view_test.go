package main

import (
	"reflect"
	"testing"

	"github.com/andree-bjorkgard/remote-bluetooth/v2/bluez"
	"github.com/andree-bjorkgard/remote-bluetooth/v2/client"
)

func dev(addr, alias string, connected bool, audio bluez.AudioState) bluez.Device {
	return bluez.Device{Address: addr, Alias: alias, Name: alias, Trusted: true, Paired: true, Connected: connected, Audio: audio}
}

func rdev(server string, d bluez.Device) client.Device {
	return client.Device{Server: server, Device: d}
}

// The five situations of the design.
func situations() map[string]deviceView {
	const here = "laptop"
	local := []bluez.Device{
		dev("A", "ATH", true, bluez.AudioIdle),
		dev("G", "Glove80", true, bluez.AudioNone),
		dev("M", "MX Master 3S", false, bluez.AudioNone),
	}
	remote := []client.Device{
		rdev("ingenting", dev("A", "ATH", true, bluez.AudioSending)),
		rdev("ingenting", dev("G", "Glove80", true, bluez.AudioNone)),
		rdev("ingenting", dev("V", "MX Vertical", true, bluez.AudioNone)),
	}
	out := map[string]deviceView{}
	for _, v := range merge(here, local, remote) {
		out[v.Alias] = v
	}
	// ATH sending on both.
	both := merge(here, []bluez.Device{dev("A", "ATH", true, bluez.AudioSending)},
		[]client.Device{rdev("ingenting", dev("A", "ATH", true, bluez.AudioSending))})
	out["ATH both"] = both[0]
	return out
}

func TestStatus(t *testing.T) {
	s := situations()
	cases := []struct {
		name           string
		status, detail string
		tone           tone
	}{
		{"ATH", "▶ Sound from ingenting", "· connected to both", toneOn},
		{"ATH both", "▶ Sound from both", "· connected to both", toneOn},
		{"Glove80", "Connected here", "· and on ingenting", toneOn},
		{"MX Vertical", "Connected on ingenting", "", toneNeutral},
		{"MX Master 3S", "Not connected", "", toneDim},
	}
	for _, c := range cases {
		st, d, tn := s[c.name].status()
		if st != c.status || d != c.detail || tn != c.tone {
			t.Errorf("%s: got %q %q %v, want %q %q %v", c.name, st, d, tn, c.status, c.detail, c.tone)
		}
	}
}

func TestStatusVariants(t *testing.T) {
	sendingHere := merge("laptop", []bluez.Device{dev("A", "ATH", true, bluez.AudioSending)}, nil)[0]
	if st, d, tn := sendingHere.status(); st != "▶ Sound from here" || d != "" || tn != toneOn {
		t.Errorf("here: %q %q %v", st, d, tn)
	}
	elsewhereOnly := merge("laptop", nil, []client.Device{rdev("ingenting", dev("X", "X", false, bluez.AudioNone))})[0]
	if st, d, tn := elsewhereOnly.status(); st != "Not connected" || d != "· paired on ingenting" || tn != toneDim {
		t.Errorf("paired elsewhere: %q %q %v", st, d, tn)
	}
	alsoHere := deviceView{On: []presence{
		{Machine: "laptop", Here: true, Dev: bluez.Device{Connected: true}},
		{Machine: "ingenting", Dev: bluez.Device{Audio: bluez.AudioSending}},
	}}
	if st, d, _ := alsoHere.status(); st != "▶ Sound from ingenting" || d != "· also here" {
		t.Errorf("also here: %q %q", st, d)
	}
}

func TestActions(t *testing.T) {
	s := situations()
	type la struct{ Label, Hint string }
	cases := []struct {
		name string
		want []la
	}{
		{"ATH", []la{
			{"Play here", "ingenting lets go for a moment and the sound comes here. Both stay connected."},
			{"Disconnect here", "ingenting keeps them"},
			{"Disconnect on ingenting", "Only this computer keeps them"},
			{"Back", ""},
		}},
		{"Glove80", []la{
			{"Disconnect here", "ingenting keeps them"},
			{"Disconnect on ingenting", "Only this computer keeps them"},
			{"Back", ""},
		}},
		{"MX Vertical", []la{
			{"Move here", "Disconnect on ingenting, then connect here"},
			{"Disconnect on ingenting", "It stays paired on both"},
			{"Back", ""},
		}},
		{"MX Master 3S", []la{
			{"Connect here", ""},
			{"Back", ""},
		}},
	}
	for _, c := range cases {
		var got []la
		for _, a := range s[c.name].actions() {
			got = append(got, la{a.Label, a.Hint})
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	// Kinds and machines.
	acts := s["MX Vertical"].actions()
	if acts[0].Kind != actMoveHere || acts[1].Kind != actDisconnectOn || acts[1].Machine != "ingenting" || acts[2].Kind != actBack {
		t.Errorf("kinds: %+v", acts)
	}
	// Connect on <m> when not connected anywhere and known on m.
	remoteOnly := merge("laptop", nil, []client.Device{rdev("ingenting", dev("X", "X", false, bluez.AudioNone))})[0]
	acts = remoteOnly.actions()
	if len(acts) != 2 || acts[0].Kind != actConnectOn || acts[0].Label != "Connect on ingenting" || acts[0].Hint != "" {
		t.Errorf("connect on: %+v", acts)
	}
}

func TestMessage(t *testing.T) {
	s := situations()
	cases := map[string][]string{
		"ATH":          {"▶ Sound from ingenting", "Connected here and on ingenting"},
		"Glove80":      {"Connected here and on ingenting"},
		"MX Vertical":  {"Connected on ingenting"},
		"MX Master 3S": {"Not connected · paired here"},
	}
	for name, want := range cases {
		if got := s[name].message(); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
	d := dev("G", "G", true, bluez.AudioNone)
	d.HasBattery, d.BatteryPercent = true, 40
	v := merge("laptop", []bluez.Device{d}, []client.Device{rdev("ingenting", dev("G", "G", true, bluez.AudioNone))})[0]
	if v.Battery != 40 {
		t.Errorf("battery = %d", v.Battery)
	}
	if got := v.message(); !reflect.DeepEqual(got, []string{"Connected here and on ingenting · battery 40%"}) {
		t.Errorf("message = %q", got)
	}
	r := dev("X", "X", true, bluez.AudioNone)
	r.HasBattery, r.BatteryPercent = true, 100
	v = merge("laptop", nil, []client.Device{rdev("ingenting", r)})[0]
	if got := v.message(); !reflect.DeepEqual(got, []string{"Connected on ingenting · battery 100%"}) {
		t.Errorf("message = %q", got)
	}
	if v := merge("laptop", nil, []client.Device{rdev("ingenting", dev("Y", "Y", false, bluez.AudioNone))})[0]; v.Battery != -1 {
		t.Errorf("no battery should be -1, got %d", v.Battery)
	}
}

func TestMergeTrustedOnlyStableOrder(t *testing.T) {
	untrusted := bluez.Device{Address: "U", Alias: "U"}
	local := []bluez.Device{dev("B", "local-b", false, 0), untrusted, dev("A", "local-a", false, 0)}
	remote := []client.Device{
		rdev("zeta", dev("C", "c", false, 0)),
		rdev("zeta", dev("A", "remote-a", false, 0)),
		rdev("alpha", dev("A", "a2", false, 0)),
		rdev("alpha", bluez.Device{Address: "U2", Alias: "U2"}),
	}
	got := merge("laptop", local, remote)
	var addrs []string
	for _, v := range got {
		addrs = append(addrs, v.Address)
	}
	if want := []string{"B", "A", "C"}; !reflect.DeepEqual(addrs, want) {
		t.Fatalf("order = %v, want %v", addrs, want)
	}
	a := got[1]
	if a.Alias != "local-a" {
		t.Errorf("alias = %q, want local's", a.Alias)
	}
	var machines []string
	for _, p := range a.On {
		machines = append(machines, p.Machine)
	}
	if want := []string{"laptop", "alpha", "zeta"}; !reflect.DeepEqual(machines, want) {
		t.Errorf("presences = %v, want %v", machines, want)
	}
	if !a.On[0].Here || a.On[1].Here {
		t.Errorf("Here flags wrong: %+v", a.On)
	}
	if c := got[2]; c.Alias != "c" {
		t.Errorf("remote-only alias = %q", c.Alias)
	}
}
