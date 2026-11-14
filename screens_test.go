package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/andree-bjorkgard/remote-bluetooth/v2/bluez"
	"github.com/andree-bjorkgard/remote-bluetooth/v2/client"
	"github.com/andree-bjorkgard/rofi"
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
		if len(o.Cmds) != 1 || o.Cmds[0] != "open" {
			t.Errorf("option %d cmds %q, want [open]", i, o.Cmds)
		}
		if !o.UseMarkup || !o.IsMultiline {
			t.Errorf("option %d: markup %v multiline %v, want both", i, o.UseMarkup, o.IsMultiline)
		}
		if len(o.Info) != 1 || !strings.HasPrefix(o.Info[0], `<span size="small" color="#C3C3C3">`) {
			t.Errorf("option %d info %q, want one small grey line", i, o.Info)
		}
	}

	// toneOn: status in green, detail plain.
	if o := opts[0]; o.Label != "ATH" ||
		!strings.Contains(o.Info[0], `<span color="#61C766">▶ Sound from ingenting</span> · connected to both`) {
		t.Errorf("ATH row: label %q info %q", o.Label, o.Info)
	}
	// toneNeutral: plain.
	if o := opts[1]; o.Label != "MX Vertical" || o.Info[0] != small("Connected on ingenting") {
		t.Errorf("MX Vertical row: label %q info %q", o.Label, o.Info)
	}
	// toneDim: name and status line dimmed.
	if o := opts[2]; o.Label != `<span color="#928374">MX Master 3S</span>` ||
		!strings.Contains(o.Info[0], `<span color="#928374">Not connected</span>`) {
		t.Errorf("MX Master 3S row: label %q info %q", o.Label, o.Info)
	}
}

func TestListOptionsDimDetail(t *testing.T) {
	v := merge("laptop", nil, []client.Device{rdev("ingenting", dev("K", "Keychron", false, bluez.AudioNone))})[0]
	o := listOptions([]deviceView{v}, "laptop")[0]
	if !strings.Contains(o.Info[0], `<span color="#928374">Not connected · paired on ingenting</span>`) {
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
	opts := deviceOptions(v, false)
	if len(opts) != len(acts) {
		t.Fatalf("got %d options, want %d", len(opts), len(acts))
	}
	if opts[0].Label != "Play here" {
		t.Errorf("first option %q, want the first action Play here", opts[0].Label)
	}
	for i, o := range opts {
		want := "act|A|" + strconv.Itoa(int(acts[i].Kind)) + "|" + acts[i].Machine
		if o.Value != want {
			t.Errorf("option %d value %q, want %q", i, o.Value, want)
		}
		if len(o.Cmds) != 1 || o.Cmds[0] != "run" {
			t.Errorf("option %d cmds %q, want [run]", i, o.Cmds)
		}
		if strings.Contains(o.Value, "||") {
			t.Errorf("option %d value %q contains ||", i, o.Value)
		}
		if acts[i].Hint != "" && (len(o.Info) != 1 || o.Info[0] != small(acts[i].Hint)) {
			t.Errorf("option %d info %q, want hint %q", i, o.Info, acts[i].Hint)
		}
	}
	if opts[len(opts)-1].Label != "Back" || opts[len(opts)-1].Value != "act|A|"+strconv.Itoa(int(actBack))+"|" {
		t.Errorf("last option %q %q, want Back", opts[len(opts)-1].Label, opts[len(opts)-1].Value)
	}
	if o := opts[2]; o.Label != "Disconnect on ingenting" || o.Value != "act|A|"+strconv.Itoa(int(actDisconnectOn))+"|ingenting" {
		t.Errorf("third option %q %q", o.Label, o.Value)
	}
}

func TestDeviceOptionsBusy(t *testing.T) {
	v := situations()["ATH"]
	opts := deviceOptions(v, true)
	if opts[0].Label != "Switching…" {
		t.Errorf("first option %q, want Switching…", opts[0].Label)
	}
	if opts[0].Value != "act|A|"+strconv.Itoa(int(actPlayHere))+"|" {
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
	for _, o := range deviceOptions(v, false) {
		if strings.Contains(o.Label, "a&b") {
			t.Errorf("label %q not escaped", o.Label)
		}
	}
}

func TestParseAction(t *testing.T) {
	addr, kind, machine, ok := parseAction("act|00:0A:45:19:F3:A6|3|ingenting")
	if !ok || addr != "00:0A:45:19:F3:A6" || kind != actConnectOn || machine != "ingenting" {
		t.Errorf("got %q %v %q %v", addr, kind, machine, ok)
	}
	if _, _, _, ok := parseAction("dev|A"); ok {
		t.Error("dev value parsed as action")
	}
	if _, _, _, ok := parseAction("act|A|x|"); ok {
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
	got := deviceMessage(v, nil)
	if want := "▶ Sound from ingenting\nConnected here and on ingenting"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got = deviceMessage(v, []string{"ingenting"})
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
	opts := deviceOptions(v, true)
	if opts[0].Label != "Switching…" || opts[0].Value != "act|A|"+strconv.Itoa(int(actPlayHere))+"|" {
		t.Errorf("first option %q %q, want Switching…", opts[0].Label, opts[0].Value)
	}
	if n := len(opts); n != len(v.actions())+1 {
		t.Errorf("got %d options, want actions plus Switching…", n)
	}
}

// rofi joins value and commands with "||" and splits them back; an action
// with no machine ends in "|", which that split moves into the command.
func TestSelectionRoundTrip(t *testing.T) {
	for _, value := range []string{"act|A|6|", "act|A|3|ingenting", "dev|A"} {
		data := value + "||run"
		parts := strings.Split(data, "||") // as rofi.getValue does
		sel := selection(rofi.Value{Value: parts[0], Cmd: parts[1]})
		if sel.Value != value || sel.Cmd != "run" {
			t.Errorf("%q: got %q %q", value, sel.Value, sel.Cmd)
		}
	}
}
