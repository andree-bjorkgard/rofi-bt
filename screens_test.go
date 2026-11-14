package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/andree-bjorkgard/remote-bluetooth/v2/bluez"
	"github.com/andree-bjorkgard/remote-bluetooth/v2/client"
)

func TestListOptions(t *testing.T) {
	s := situations()
	views := []deviceView{s["ATH"], s["MX Vertical"], s["MX Master 3S"]}
	opts := listOptions(views, "laptop")
	if len(opts) != 3 {
		t.Fatalf("got %d options, want 3", len(opts))
	}
	for i, o := range opts {
		if want := "dev|" + views[i].Address; o.Value != want {
			t.Errorf("option %d value %q, want %q", i, o.Value, want)
		}
		if len(o.Cmds) != 2 || o.Cmds[0] != "toggle" || o.Cmds[1] != "open" {
			t.Errorf("option %d cmds %q, want [toggle open]", i, o.Cmds)
		}
		if !o.UseMarkup || !o.IsMultiline {
			t.Errorf("option %d: markup %v multiline %v, want both", i, o.UseMarkup, o.IsMultiline)
		}
		if len(o.Info) != 1 || !strings.HasPrefix(o.Info[0], `<span size="small">`) {
			t.Errorf("option %d info %q, want one small line", i, o.Info)
		}
		// No fixed colours or opacity in a row: rofi colours the text by
		// the row's state, black when the row is highlighted.
		if strings.Contains(o.Label+o.Info[0], "color=") || strings.Contains(o.Label+o.Info[0], "alpha") {
			t.Errorf("option %d has a fixed colour: %q %q", i, o.Label, o.Info)
		}
	}

	// Connected here: a normal row, status in bold.
	if o := opts[0]; o.Label != "ATH" || o.IsUrgent ||
		o.Info[0] != `<span size="small"><b>▶ Sound from ingenting</b></span>` {
		t.Errorf("ATH row: label %q info %q", o.Label, o.Info)
	}
	// Connected elsewhere only: grey.
	if o := opts[1]; o.Label != "MX Vertical" || !o.IsUrgent || o.Info[0] != small("Connected on ingenting") {
		t.Errorf("MX Vertical row: label %q info %q", o.Label, o.Info)
	}
	// toneDim: an urgent (grey) row.
	if o := opts[2]; o.Label != "MX Master 3S" || !o.IsUrgent ||
		o.Info[0] != small("Not connected") {
		t.Errorf("MX Master 3S row: label %q info %q", o.Label, o.Info)
	}
}

func TestListOptionsDimDetail(t *testing.T) {
	v := merge("laptop", nil, []client.Device{rdev("ingenting", dev("K", "Keychron", false, bluez.AudioNone))})[0]
	o := listOptions([]deviceView{v}, "laptop")[0]
	if !o.IsUrgent || o.Info[0] != small("Not connected · paired on ingenting") {
		t.Errorf("info %q, want dim status and detail", o.Info)
	}
}

func TestListOptionsBattery(t *testing.T) {
	d := dev("G", "Glove80", true, bluez.AudioNone)
	d.HasBattery, d.BatteryPercent = true, 80
	o := listOptions(merge("laptop", []bluez.Device{d}, nil), "laptop")[0]
	if !strings.Contains(o.Info[0], "80%") {
		t.Errorf("info %q, want battery", o.Info)
	}
}

func TestListOptionsEscapes(t *testing.T) {
	v := merge("laptop", []bluez.Device{dev("E", "<Tom & Jerry>", false, bluez.AudioNone)}, nil)[0]
	o := listOptions([]deviceView{v}, "laptop")[0]
	if !strings.Contains(o.Label, "&lt;Tom &amp; Jerry&gt;") || strings.Contains(o.Label, "<Tom") {
		t.Errorf("label %q not escaped", o.Label)
	}
	v = merge("laptop", nil, []client.Device{rdev("a<b", dev("E", "E", true, bluez.AudioNone))})[0]
	o = listOptions([]deviceView{v}, "laptop")[0]
	if !strings.Contains(o.Info[0], "a&lt;b") {
		t.Errorf("info %q, machine not escaped", o.Info)
	}
}

func TestDeviceOptions(t *testing.T) {
	v := situations()["ATH"]
	acts := v.actions()
	opts := deviceOptions(v, nil)
	if len(opts) != len(acts) {
		t.Fatalf("got %d options, want %d", len(opts), len(acts))
	}
	if opts[0].Label != "Play here" {
		t.Errorf("first option %q, want the first action Play here", opts[0].Label)
	}
	for i, o := range opts {
		machine := acts[i].Machine
		if machine == "" {
			machine = "-"
		}
		want := "act|A|" + machine + "|" + strconv.Itoa(int(acts[i].Kind))
		if o.Value != want {
			t.Errorf("option %d value %q, want %q", i, o.Value, want)
		}
		if len(o.Cmds) != 1 || o.Cmds[0] != "run" {
			t.Errorf("option %d cmds %q, want [run]", i, o.Cmds)
		}
		if strings.Contains(o.Value, "||") {
			t.Errorf("option %d value %q contains ||", i, o.Value)
		}
		if o.Icon == "" || !strings.HasSuffix(o.Icon, ".svg") {
			t.Errorf("option %d icon %q, want an svg", i, o.Icon)
		}
		if acts[i].Hint != "" && (len(o.Info) != 1 || o.Info[0] != small(acts[i].Hint)) {
			t.Errorf("option %d info %q, want hint %q", i, o.Info, acts[i].Hint)
		}
	}
	if opts[len(opts)-1].Label != "Back" || opts[len(opts)-1].Value != "act|A|-|"+strconv.Itoa(int(actBack)) {
		t.Errorf("last option %q %q, want Back", opts[len(opts)-1].Label, opts[len(opts)-1].Value)
	}
	if o := opts[2]; o.Label != "Disconnect on ingenting" || o.Value != "act|A|ingenting|"+strconv.Itoa(int(actDisconnectOn)) {
		t.Errorf("third option %q %q", o.Label, o.Value)
	}
}

func TestDeviceOptionsBusy(t *testing.T) {
	v := situations()["ATH"]
	opts := deviceOptions(v, &playHereBusy)
	if opts[0].Label != "Switching…" {
		t.Errorf("first option %q, want Switching…", opts[0].Label)
	}
	if opts[0].Value != "act|A|-|"+strconv.Itoa(int(actPlayHere)) {
		t.Errorf("first option value %q", opts[0].Value)
	}
	for _, o := range opts[1:] {
		if o.Label == "Switching…" || o.Label == "Play here" {
			t.Errorf("unexpected %q row", o.Label)
		}
	}
}

func TestDeviceOptionsEscapes(t *testing.T) {
	v := merge("laptop", nil, []client.Device{rdev("a&b", dev("K", "K", false, bluez.AudioNone))})[0]
	for _, o := range deviceOptions(v, nil) {
		if strings.Contains(o.Label, "a&b") {
			t.Errorf("label %q not escaped", o.Label)
		}
	}
}

func TestParseAction(t *testing.T) {
	addr, kind, machine, ok := parseAction("act|00:0A:45:19:F3:A6|ingenting|3")
	if !ok || addr != "00:0A:45:19:F3:A6" || kind != actConnectOn || machine != "ingenting" {
		t.Errorf("got %q %v %q %v", addr, kind, machine, ok)
	}
	if _, _, _, ok := parseAction("dev|A"); ok {
		t.Error("dev value parsed as action")
	}
	if _, _, _, ok := parseAction("act|A|-|x"); ok {
		t.Error("bad kind parsed")
	}
}

func TestListMessage(t *testing.T) {
	if got, want := listMessage("andree", nil), "andree (this computer)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := listMessage("andree", []string{"ingenting", "x<y"}), "andree (this computer) · ingenting, x&lt;y"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDeviceMessage(t *testing.T) {
	v := situations()["ATH"]
	got := deviceMessage(v, "")
	if want := "▶ Sound from ingenting\nConnected here and on ingenting"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got = deviceMessage(v, playHereProgress([]string{"ingenting"}))
	if want := `<span color="#FDD835">… Moving the sound here: ingenting has let go</span>` + "\n▶ Sound from ingenting\nConnected here and on ingenting"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPlayHereNotice(t *testing.T) {
	cases := []struct {
		released      []string
		summary, body string
	}{
		{[]string{"ingenting"}, "ATH: sound moved here", "ingenting let go and is back to normal"},
		{[]string{"a", "b"}, "ATH: sound moved here", "a and b let go and are back to normal"},
	}
	for _, c := range cases {
		s, b := playHereNotice("ATH", client.PlayHereResult{Released: c.released}, nil)
		if s != c.summary || b != c.body {
			t.Errorf("%v: got %q / %q", c.released, s, b)
		}
	}
	failed := []struct {
		res  client.PlayHereResult
		body string
	}{
		{client.PlayHereResult{Released: []string{"a", "b"}, Failed: map[string]error{"b": errors.New("x")}},
			"a let go and is back to normal. b could not be restored yet; it will restore itself within 15 seconds."},
		{client.PlayHereResult{Released: []string{"a"}, Failed: map[string]error{"a": errors.New("x")}},
			"a could not be restored yet; it will restore itself within 15 seconds."},
		{client.PlayHereResult{Released: []string{"a", "b"}, Failed: map[string]error{"a": errors.New("x"), "b": errors.New("y")}},
			"a and b could not be restored yet; they will restore themselves within 15 seconds."},
	}
	for _, c := range failed {
		s, b := playHereNotice("ATH", c.res, nil)
		if s != "ATH: sound moved here" || b != c.body {
			t.Errorf("%v: got %q / %q, want body %q", c.res, s, b, c.body)
		}
	}
	s, b := playHereNotice("ATH", client.PlayHereResult{}, client.ErrNothingPlayingHere)
	if s != "Nothing is playing here" || b != "start the sound here, then try again" {
		t.Errorf("nothing playing: got %q / %q", s, b)
	}
	s, b = playHereNotice("ATH", client.PlayHereResult{}, client.ErrNotHeldElsewhere)
	if s != "Could not move the sound" || b != client.ErrNotHeldElsewhere.Error() {
		t.Errorf("other error: got %q / %q", s, b)
	}
}

func TestStableOrder(t *testing.T) {
	order := stableOrder([]string{"B", "A", "Z"}, []string{"A", "C", "B"})
	if got := strings.Join(order, ","); got != "B,A,C" {
		t.Errorf("got %s, want B,A,C", got)
	}
}

// While the holder has let go it no longer sends, so actions() drops Play
// here; the Switching… row must stay.
func TestDeviceOptionsBusyAfterRelease(t *testing.T) {
	v := merge("laptop", []bluez.Device{dev("A", "ATH", true, bluez.AudioIdle)},
		[]client.Device{rdev("ingenting", dev("A", "ATH", true, bluez.AudioNone))})[0]
	opts := deviceOptions(v, &playHereBusy)
	if opts[0].Label != "Switching…" || opts[0].Value != "act|A|-|"+strconv.Itoa(int(actPlayHere)) {
		t.Errorf("first option %q %q, want Switching…", opts[0].Label, opts[0].Value)
	}
	if n := len(opts); n != len(v.actions())+1 {
		t.Errorf("got %d options, want actions plus Switching…", n)
	}
}

// rofi joins value and commands with "||" and splits them back; no action
// value may hold "||" or end in "|", or that split cuts it apart.
func TestActionRoundTrip(t *testing.T) {
	for _, o := range deviceOptions(situations()["ATH"], nil) {
		data := strings.Join(append([]string{o.Value}, o.Cmds...), "||") // as rofi.mapOptions
		parts := strings.Split(data, "||")                               // as rofi.getValue
		if parts[0] != o.Value || parts[1] != "run" {
			t.Errorf("%q: came back as %q %q", o.Value, parts[0], parts[1])
		}
		if _, _, _, ok := parseAction(parts[0]); !ok {
			t.Errorf("%q does not parse", parts[0])
		}
	}
	if _, kind, machine, ok := parseAction("act|A|-|" + strconv.Itoa(int(actBack))); !ok || kind != actBack || machine != "" {
		t.Errorf("Back: got %v %q %v", kind, machine, ok)
	}
}

func TestMain(m *testing.M) {
	// Icons go to a throwaway cache so deviceOptions has paths to give.
	dir, err := os.MkdirTemp("", "rofi-bt-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CACHE_HOME", dir)
	if err := installIcons(); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestActionIcons(t *testing.T) {
	for k := actPlayHere; k <= actBack; k++ {
		p := actionIcon(k)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("kind %d: icon %q: %v", k, p, err)
		}
	}
}

func TestSelection(t *testing.T) {
	m := &menu{order: []string{"A", "B", "C"}}
	if _, ok := m.selection(); ok {
		t.Error("first render selects a row")
	}
	m.rendered = true
	if _, ok := m.selection(); ok {
		t.Error("list update selects a row")
	}
	m.addr = "B" // open B
	if row, ok := m.selection(); !ok || row != 0 {
		t.Errorf("opening a device: %d %v, want first row", row, ok)
	}
	m.shown = "B"
	if _, ok := m.selection(); ok {
		t.Error("device update selects a row")
	}
	m.addr = "" // back
	if row, ok := m.selection(); !ok || row != 1 {
		t.Errorf("back: %d %v, want B's row 1", row, ok)
	}
	m.shown, m.order = "Z", []string{"A"} // the device went away
	if row, ok := m.selection(); !ok || row != 0 {
		t.Errorf("back from a gone device: %d %v, want 0", row, ok)
	}
}

func TestToggle(t *testing.T) {
	cases := []struct {
		name    string
		v       deviceView
		kind    actionKind
		machine string
	}{
		{"connected here", merge("laptop", []bluez.Device{dev("A", "A", true, bluez.AudioNone)}, nil)[0], actDisconnectHere, ""},
		{"paired here", merge("laptop", []bluez.Device{dev("A", "A", false, bluez.AudioNone)},
			[]client.Device{rdev("ingenting", dev("A", "A", true, bluez.AudioNone))})[0], actConnectHere, ""},
		{"only there, connected", merge("laptop", nil, []client.Device{rdev("ingenting", dev("A", "A", true, bluez.AudioNone))})[0], actDisconnectOn, "ingenting"},
		{"only there", merge("laptop", nil, []client.Device{rdev("ingenting", dev("A", "A", false, bluez.AudioNone))})[0], actConnectOn, "ingenting"},
	}
	for _, c := range cases {
		if a := c.v.toggle(); a.Kind != c.kind || a.Machine != c.machine {
			t.Errorf("%s: got %v %q, want %v %q", c.name, a.Kind, a.Machine, c.kind, c.machine)
		}
	}
}

var playHereBusy = action{Kind: actPlayHere, Label: "Play here"}

// Sound plays here and the headphones are also on ingenting: Play on is
// offered, and Play here is not.
func TestPlayOnAction(t *testing.T) {
	v := merge("laptop", []bluez.Device{dev("A", "ATH", true, bluez.AudioSending)},
		[]client.Device{rdev("ingenting", dev("A", "ATH", true, bluez.AudioIdle))})[0]
	acts := v.actions()
	if acts[0].Kind != actPlayOn || acts[0].Machine != "ingenting" || acts[0].Label != "Play on ingenting" {
		t.Fatalf("first action %+v, want Play on ingenting", acts[0])
	}
	for _, a := range acts {
		if a.Kind == actPlayHere {
			t.Error("Play here offered while the sound plays here")
		}
	}
	// Not offered when the sound is not playing here.
	v = merge("laptop", []bluez.Device{dev("A", "ATH", true, bluez.AudioIdle)},
		[]client.Device{rdev("ingenting", dev("A", "ATH", true, bluez.AudioIdle))})[0]
	for _, a := range v.actions() {
		if a.Kind == actPlayOn {
			t.Error("Play on offered while nothing plays here")
		}
	}
}

func TestDeviceOptionsPlayOnBusy(t *testing.T) {
	v := merge("laptop", []bluez.Device{dev("A", "ATH", true, bluez.AudioSending)},
		[]client.Device{rdev("ingenting", dev("A", "ATH", true, bluez.AudioIdle))})[0]
	busy := action{Kind: actPlayOn, Machine: "ingenting", Label: "Play on ingenting"}
	opts := deviceOptions(v, &busy)
	if opts[0].Label != "Switching…" || opts[0].Value != "act|A|ingenting|"+strconv.Itoa(int(actPlayOn)) {
		t.Errorf("first option %q %q", opts[0].Label, opts[0].Value)
	}
	if len(opts) != len(v.actions()) {
		t.Errorf("got %d options, want the actions with Play on replaced", len(opts))
	}
}

func TestPlayOnNotice(t *testing.T) {
	cases := []struct {
		err           error
		summary, body string
	}{
		{nil, "ATH: sound moved to ingenting", "This computer let go and is back to normal"},
		{client.ErrNothingPlayingThere, "Nothing is playing on ingenting", "The sound stayed here. Start it on ingenting, then try again."},
		{client.ErrNotConnectedThere, "Could not move the sound", "ATH is not connected on ingenting"},
	}
	for _, c := range cases {
		if s, b := playOnNotice("ATH", "ingenting", c.err); s != c.summary || b != c.body {
			t.Errorf("%v: got %q / %q", c.err, s, b)
		}
	}
}
