// Command rofi-bt is a rofi-blocks menu for Bluetooth devices: the trusted
// devices of this machine and, through remote-bluetooth, of other machines.
//
// stdout is the rofi-blocks protocol; diagnostics go to stderr.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/andree-bjorkgard/remote-bluetooth/v2/bluez"
	"github.com/andree-bjorkgard/remote-bluetooth/v2/client"
	"github.com/andree-bjorkgard/rofi"
)

const (
	appName         = "Bluetooth"
	deviceCacheName = "bluetooth-devices"
	// remoteSep separates server and address in an option value. Neither a
	// hostname nor a Bluetooth address contains it.
	remoteSep = "/"
)

// entry is one device in the menu. server is empty for local devices.
type entry struct {
	server string
	dev    bluez.Device
}

func (e entry) key() string {
	if e.server == "" {
		return e.dev.Address
	}
	return e.server + remoteSep + e.dev.Address
}

func (e entry) name() string {
	if e.dev.Alias != "" {
		return e.dev.Alias
	}
	return e.dev.Address
}

// result reports a finished connect or disconnect.
type result struct {
	key string
	cmd string
	err error
}

func main() {
	log.SetOutput(os.Stderr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	model, rofiEvents := rofi.NewRofiBlock()
	model.Prompt = "Bluetooth devices"
	model.Message = "Loading devices..."
	model.Render()

	m := &menu{model: &model, entries: map[string]entry{}}

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
				if d.Trusted {
					e := entry{dev: d}
					m.entries[e.key()] = e
					opts = append(opts, rofi.Option{Value: e.key()})
				}
			}
			// Order local devices by recent use once, so the list does not
			// jump around while the menu is open.
			for _, o := range rofi.SortUsingHistory(opts, deviceCacheName) {
				m.order = append(m.order, o.Value)
			}
		}
	}

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

	if adapter == nil && remote == nil {
		model.Message = "No Bluetooth adapter and remote-bluetooth is not set up"
		model.Render()
	} else {
		model.Message = "Select a device"
		m.render()
	}

	results := make(chan result)
	connected := make(chan string)

	for {
		select {
		case c, ok := <-localChanges:
			if !ok {
				localChanges = nil
				continue
			}
			if c.Removed || !c.Device.Trusted {
				m.remove(c.Device.Address)
			} else {
				m.upsert(entry{dev: c.Device})
			}

		case ev, ok := <-remoteEvents:
			if !ok {
				remoteEvents = nil
				continue
			}
			e := entry{server: ev.Server, dev: ev.Device.Device}
			switch ev.Type {
			case client.DeviceUpdated:
				m.upsert(e)
			case client.DeviceRemoved:
				m.remove(e.key())
			default:
				continue
			}

		case v := <-rofiEvents:
			e, ok := m.entries[v.Value]
			if !ok || (v.Cmd != "connect" && v.Cmd != "disconnect") {
				continue
			}
			rofi.SaveToHistory(deviceCacheName, v.Value)
			where := ""
			if e.server != "" {
				where = " on " + e.server
			}
			if v.Cmd == "connect" {
				sendNotification(fmt.Sprintf("Connecting to \"%s\"%s", e.name(), where))
			} else {
				sendNotification(fmt.Sprintf("Disconnecting \"%s\"%s", e.name(), where))
			}
			// Run in the background so the menu keeps updating meanwhile.
			go func(e entry, cmd string) {
				results <- result{key: e.key(), cmd: cmd, err: act(ctx, adapter, remote, e, cmd)}
			}(e, v.Cmd)
			continue

		case r := <-results:
			name := r.key
			if e, ok := m.entries[r.key]; ok {
				name = e.name()
			}
			switch {
			case r.err != nil:
				model.Message = fmt.Sprintf("Could not %s \"%s\": %s", r.cmd, name, r.err)
				sendNotification(model.Message)
			case r.cmd == "connect":
				// Give the device a moment to report its battery level.
				go func() {
					time.Sleep(time.Second)
					connected <- r.key
				}()
				continue
			default:
				sendNotification(fmt.Sprintf("Disconnected \"%s\"", name))
				continue
			}

		case key := <-connected:
			if e, ok := m.entries[key]; ok {
				msg := fmt.Sprintf("Connected to \"%s\"", e.name())
				if e.dev.HasBattery {
					msg += "\n" + batteryLabel(e.dev.BatteryPercent)
				}
				sendNotification(msg)
			}
			continue
		}
		m.render()
	}
}

func act(ctx context.Context, adapter *bluez.Adapter, remote *client.Client, e entry, cmd string) error {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	switch {
	case e.server == "" && cmd == "connect":
		return adapter.Connect(ctx, e.dev.Address)
	case e.server == "":
		return adapter.Disconnect(ctx, e.dev.Address)
	case cmd == "connect":
		return remote.Connect(ctx, e.server, e.dev.Address)
	default:
		return remote.Disconnect(ctx, e.server, e.dev.Address)
	}
}

// menu holds the devices shown and their order.
type menu struct {
	model   *rofi.Model
	entries map[string]entry
	order   []string
}

func (m *menu) upsert(e entry) {
	k := e.key()
	if _, ok := m.entries[k]; !ok {
		m.order = append(m.order, k)
	}
	m.entries[k] = e
}

func (m *menu) remove(key string) {
	if _, ok := m.entries[key]; !ok {
		return
	}
	delete(m.entries, key)
	for i, k := range m.order {
		if k == key {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
}

func (m *menu) render() {
	opts := make([]rofi.Option, 0, len(m.order))
	for _, k := range m.order {
		opts = append(opts, option(m.entries[k]))
	}
	m.model.Options = opts
	m.model.Render()
}

func option(e entry) rofi.Option {
	label, cmd := "  ", "connect"
	if e.dev.Connected {
		label, cmd = "  ", "disconnect"
	}

	info := []string{small(strings.TrimPrefix(e.key(), remoteSep))}
	if e.dev.Connected && e.dev.HasBattery {
		info = append(info, small(batteryLabel(e.dev.BatteryPercent)))
	}

	return rofi.Option{
		Label:       label + markupEscape(e.name()),
		Info:        info,
		Value:       e.key(),
		Icon:        e.dev.Icon,
		Cmds:        []string{cmd, "controls"},
		IsMultiline: true,
		UseMarkup:   true,
	}
}

func small(s string) string {
	return fmt.Sprintf("<span size=\"small\" color=\"#C3C3C3\">%s</span>", markupEscape(s))
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

func sendNotification(message string) {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return
	}
	_ = exec.Command("notify-send", "-a", appName, message).Run()
}
