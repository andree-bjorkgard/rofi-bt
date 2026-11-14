// Command rofi-bt is a rofi-blocks menu for Bluetooth devices: the trusted
// devices of this machine and, through remote-bluetooth, of other machines,
// merged into one row per device.
//
// stdout is the rofi-blocks protocol; diagnostics go to stderr.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/andree-bjorkgard/remote-bluetooth/v2/bluez"
	"github.com/andree-bjorkgard/remote-bluetooth/v2/client"
	"github.com/andree-bjorkgard/rofi"
)

const (
	appName         = "Bluetooth"
	deviceCacheName = "bluetooth-devices"
	actionTimeout   = 40 * time.Second
	playHereTimeout = 60 * time.Second
)

var (
	errNoLocalBluetooth = errors.New("Bluetooth is not available on this computer")
	errNoRemote         = errors.New("remote-bluetooth is not set up")
)

// result reports a finished action.
type result struct {
	addr    string
	kind    actionKind
	machine string
	err     error
	play    client.PlayHereResult
}

func main() {
	log.SetOutput(os.Stderr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	model, rofiEvents := rofi.NewRofiBlock()
	model.Prompt = "Bluetooth devices"
	model.Message = "Loading devices..."
	model.Render()

	here, err := os.Hostname()
	if err != nil {
		log.Println("hostname:", err)
		here = "this computer"
	}
	m := &menu{model: &model, here: here, local: map[string]bluez.Device{}}

	// Local devices.
	var localChanges <-chan bluez.Change
	adapter, err := bluez.Open("")
	if err != nil {
		log.Println("local bluetooth:", err)
		adapter = nil
	} else {
		defer adapter.Close()
		initial, changes, err := adapter.Watch(ctx)
		if err != nil {
			log.Println("watch local devices:", err)
		} else {
			localChanges = changes
			var opts []rofi.Option
			for _, d := range initial {
				m.local[d.Address] = d
				if d.Trusted {
					opts = append(opts, rofi.Option{Value: d.Address})
				}
			}
			// Order local devices by recent use once, so the list does not
			// jump around while the menu is open.
			for _, o := range rofi.SortUsingHistory(opts, deviceCacheName) {
				m.order = append(m.order, o.Value)
			}
		}
	}
	m.adapter = adapter

	// Remote devices. Without a secret remote-bluetooth is simply not set up.
	var remote *client.Client
	var remoteEvents <-chan client.Event
	if secret, err := client.LoadSecret(); err != nil {
		log.Println("remote devices disabled:", err)
	} else if remote, err = client.New(client.Config{
		Secret:  secret,
		Servers: client.ParseServers(os.Getenv(client.ServersEnv)),
	}); err != nil {
		log.Println("remote devices disabled:", err)
		remote = nil
	} else {
		go remote.Run(ctx)
		remoteEvents = remote.Events()
	}
	m.remote = remote

	if adapter == nil && remote == nil {
		model.Message = "No Bluetooth adapter and remote-bluetooth is not set up"
		model.Render()
	} else {
		m.refresh()
		m.render()
	}

	results := make(chan result)
	connected := make(chan result)

	for {
		select {
		case c, ok := <-localChanges:
			if !ok {
				localChanges = nil
				continue
			}
			if c.Removed {
				delete(m.local, c.Device.Address)
			} else {
				m.local[c.Device.Address] = c.Device
			}
			m.refresh()

		case _, ok := <-remoteEvents:
			if !ok {
				remoteEvents = nil
				continue
			}
			m.refresh()

		case v := <-rofiEvents:
			if !m.handle(ctx, v, results) {
				continue
			}

		case r := <-results:
			if !m.finished(r, connected) {
				continue
			}

		case r := <-connected:
			name, body := r.addr, ""
			if v, ok := m.view(r.addr); ok {
				name = v.Alias
				if v.Battery >= 0 {
					body = batteryLabel(uint8(v.Battery))
				}
			}
			sendNotification(fmt.Sprintf("Connected to \"%s\"%s", name, where(r)), body)
			continue
		}
		m.render()
	}
}

// menu is the state of the open menu. Only the event loop touches it.
type menu struct {
	model   *rofi.Model
	here    string
	adapter *bluez.Adapter // nil without local Bluetooth
	remote  *client.Client // nil without remote-bluetooth

	local map[string]bluez.Device
	views map[string]deviceView
	order []string // addresses, stable while the menu is open

	// addr is the device whose screen is shown; empty for the list.
	addr string
	// A running Play here: the device and the machines that let go.
	busyAddr string
	holders  []string
	// errMsg is shown under the message until the next navigation.
	errMsg string
}

// refresh recomputes the merged views from the local and remote devices.
func (m *menu) refresh() {
	local := make([]bluez.Device, 0, len(m.local))
	for _, d := range m.local {
		local = append(local, d)
	}
	sort.Slice(local, func(i, j int) bool { return local[i].Address < local[j].Address })
	var remote []client.Device
	if m.remote != nil {
		remote = m.remote.Devices()
	}
	views := merge(m.here, local, remote)
	m.views = make(map[string]deviceView, len(views))
	addrs := make([]string, 0, len(views))
	for _, v := range views {
		m.views[v.Address] = v
		addrs = append(addrs, v.Address)
	}
	m.order = stableOrder(m.order, addrs)
	if _, ok := m.views[m.addr]; !ok {
		m.addr = "" // the device is gone: back to the list
	}
}

func (m *menu) view(addr string) (deviceView, bool) {
	v, ok := m.views[addr]
	return v, ok
}

func (m *menu) render() {
	if v, ok := m.view(m.addr); ok {
		busy := m.busyAddr == v.Address
		var holders []string
		if busy {
			holders = m.holders
		}
		m.model.Prompt = v.Alias
		m.model.Message = deviceMessage(v, holders)
		m.model.Options = deviceOptions(v, busy)
	} else {
		var others []string
		if m.remote != nil {
			others = m.remote.Servers()
		}
		views := make([]deviceView, 0, len(m.order))
		for _, a := range m.order {
			views = append(views, m.views[a])
		}
		m.model.Prompt = "Bluetooth devices"
		m.model.Message = listMessage(m.here, others)
		m.model.Options = listOptions(views, m.here)
	}
	if m.errMsg != "" {
		m.model.Message += "\n" + errorLine(m.errMsg)
	}
	m.model.Render()
}

// handle reacts to a selected row. It reports whether to render.
func (m *menu) handle(ctx context.Context, sel rofi.Value, results chan<- result) bool {
	sel = selection(sel)
	if addr, ok := strings.CutPrefix(sel.Value, devPrefix); ok && sel.Cmd == "open" {
		if _, ok := m.view(addr); !ok {
			return false
		}
		m.addr, m.errMsg = addr, ""
		return true
	}
	addr, kind, machine, ok := parseAction(sel.Value)
	if !ok || sel.Cmd != "run" {
		return false
	}
	if kind == actBack {
		m.addr, m.errMsg = "", ""
		return true
	}
	v, ok := m.view(addr)
	if !ok {
		return false
	}
	m.errMsg = ""
	rofi.SaveToHistory(deviceCacheName, addr)
	r := result{addr: addr, kind: kind, machine: machine}
	adapter, remote := m.adapter, m.remote

	if kind == actPlayHere {
		switch {
		case m.busyAddr != "":
			return false
		case adapter == nil:
			m.errMsg = "Could not move the sound: " + errNoLocalBluetooth.Error()
			return true
		case remote == nil:
			m.errMsg = "Could not move the sound: " + errNoRemote.Error()
			return true
		}
		m.busyAddr, m.holders = addr, v.others(sending)
		go func() {
			r.play, r.err = playHere(ctx, remote, adapter, addr)
			results <- r
		}()
		return true
	}

	switch kind {
	case actConnectHere, actConnectOn:
		sendNotification(fmt.Sprintf("Connecting to \"%s\"%s", v.Alias, where(r)), "")
	case actDisconnectHere, actDisconnectOn:
		sendNotification(fmt.Sprintf("Disconnecting \"%s\"%s", v.Alias, where(r)), "")
	case actMoveHere:
		sendNotification(fmt.Sprintf("Moving \"%s\" here", v.Alias), "")
	}
	from := v.others(connected)
	// Run in the background so the menu keeps updating meanwhile.
	go func() {
		r.err = act(ctx, adapter, remote, r, from)
		results <- r
	}()
	return false
}

// finished reacts to a finished action. It reports whether to render.
func (m *menu) finished(r result, connected chan<- result) bool {
	name := r.addr
	if v, ok := m.view(r.addr); ok {
		name = v.Alias
	}
	if r.kind == actPlayHere {
		m.busyAddr, m.holders = "", nil
		for server, err := range r.play.Failed {
			log.Printf("play here: %s: %s", server, err)
		}
		sendNotification(playHereNotice(name, r.play, r.err))
		return true
	}
	if r.err != nil {
		m.errMsg = fmt.Sprintf("Could not %s \"%s\": %s", verb(r.kind), name, r.err)
		sendNotification(m.errMsg, "")
		return true
	}
	switch r.kind {
	case actConnectHere, actConnectOn, actMoveHere:
		// Give the device a moment to report its battery level.
		go func() {
			time.Sleep(time.Second)
			connected <- r
		}()
	default:
		sendNotification(fmt.Sprintf("Disconnected \"%s\"%s", name, where(r)), "")
	}
	return false
}

// act runs a connect, disconnect or move. from are the other machines the
// device is connected on, for Move here.
func act(ctx context.Context, adapter *bluez.Adapter, remote *client.Client, r result, from []string) error {
	ctx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()
	switch r.kind {
	case actConnectHere, actDisconnectHere, actMoveHere:
		if adapter == nil {
			return errNoLocalBluetooth
		}
	}
	switch r.kind {
	case actConnectOn, actDisconnectOn, actMoveHere:
		if remote == nil {
			return errNoRemote
		}
	}
	switch r.kind {
	case actConnectHere:
		return adapter.Connect(ctx, r.addr)
	case actDisconnectHere:
		return adapter.Disconnect(ctx, r.addr)
	case actConnectOn:
		return remote.Connect(ctx, r.machine, r.addr)
	case actDisconnectOn:
		return remote.Disconnect(ctx, r.machine, r.addr)
	case actMoveHere:
		for _, machine := range from {
			if err := remote.Disconnect(ctx, machine, r.addr); err != nil {
				return err
			}
		}
		return adapter.Connect(ctx, r.addr)
	}
	return fmt.Errorf("unknown action %d", r.kind)
}

// playHere moves the device's sound to this machine. Whether this machine
// sends is asked of the adapter, not the menu, which belongs to the loop.
func playHere(ctx context.Context, remote *client.Client, adapter *bluez.Adapter, addr string) (client.PlayHereResult, error) {
	ctx, cancel := context.WithTimeout(ctx, playHereTimeout)
	defer cancel()
	return remote.PlayHere(ctx, addr, func(ctx context.Context) bool {
		d, err := adapter.Device(ctx, addr)
		return err == nil && d.Audio == bluez.AudioSending
	})
}

func verb(k actionKind) string {
	switch k {
	case actConnectHere, actConnectOn:
		return "connect"
	case actDisconnectHere, actDisconnectOn:
		return "disconnect"
	case actMoveHere:
		return "move"
	}
	return "run"
}

// where is " on <machine>" for an action on another machine.
func where(r result) string {
	if r.machine == "" {
		return ""
	}
	return " on " + r.machine
}

func small(s string) string {
	return smallMarkup(markupEscape(s))
}

func markupEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func batteryLabel(percent uint8) string {
	var icon string
	switch {
	case percent >= 90:
		icon = ""
	case percent >= 70:
		icon = ""
	case percent >= 50:
		icon = ""
	case percent >= 30:
		icon = ""
	default:
		icon = ""
	}
	return fmt.Sprintf("%s   %d%%", icon, percent)
}

// sendNotification shows a desktop notification; body may be empty.
func sendNotification(summary, body string) {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return
	}
	args := []string{"-a", appName, summary}
	if body != "" {
		args = append(args, body)
	}
	_ = exec.Command("notify-send", args...).Run()
}
