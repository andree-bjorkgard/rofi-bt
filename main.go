package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ingentingalls/rofi"
	"github.com/muka/go-bluetooth/api"
	"github.com/muka/go-bluetooth/bluez"
	"github.com/muka/go-bluetooth/bluez/profile/device"
	"github.com/sirupsen/logrus"
)

const appName = "Bluetooth"

func main() {
	model, eventCh := rofi.NewRofiBlock()
	model.Prompt = "Bluetooth devices"
	model.Message = "Loading devices..."
	model.Render()

	logrus.SetLevel(logrus.FatalLevel)

	adapter, err := api.GetDefaultAdapter()
	if err != nil {
		model.Message = "Error loading adapter"
		model.Render()
		return
	}

	devices, err := adapter.GetDevices()
	if err != nil {
		model.Message = "Error loading devices"
		model.Render()
		return
	}

	model.Message = "Select a device"
	model.Render()

	devUpdateCh := make(chan string)

	var opts rofi.Options
	for _, device := range devices {
		dev := device
		trusted, err := dev.GetTrusted()
		if err != nil {
			model.Message = "Error loading trusted devices"
			model.Render()
			return
		}

		if trusted {
			opts = append(opts, createOption(dev))

			devCh, err := dev.WatchProperties()
			if err != nil {
				return
			}

			go func(devCh chan *bluez.PropertyChanged) {
				defer func() {
					dev.UnwatchProperties(devCh)
				}()
				for {
					change := <-devCh

					switch change.Name {
					case "Connected":
						fallthrough
					case "Disconnected":
						devUpdateCh <- dev.Properties.Address
					default:
					}
				}
			}(devCh)

		}
	}

	model.Options = opts
	model.Render()

	for {
		select {

		case v := <-eventCh:
			device, err := adapter.GetDeviceByAddress(v.Value)
			if err != nil {
				model.Message = fmt.Sprintf("Error getting device \"%s\": %s", v.Value, err)
			}
			switch v.Cmd {
			case "connect":
				sendNotification(fmt.Sprintf("Connecting to device \"%s\"", device.Properties.Alias))
				err = device.Connect()
				if err != nil {
					model.Message = fmt.Sprintf("Error connecting to device \"%s\": %s", device.Properties.Alias, err)
					continue
				}
				sendNotification(fmt.Sprintf("Connected to device \"%s\"", device.Properties.Alias))
			case "disconnect":
				sendNotification(fmt.Sprintf("Disconnecting device \"%s\"", device.Properties.Alias))
				err = device.Disconnect()
				if err != nil {
					model.Message = fmt.Sprintf("Error disconnecting device \"%s\": %s", device.Properties.Alias, err)
					continue
				}
				sendNotification(fmt.Sprintf("Disconnected from device \"%s\"", device.Properties.Alias))
			default:
				return
			}

		case v := <-devUpdateCh:
			device, err := adapter.GetDeviceByAddress(v)
			if err != nil {
				return
			}

			for i, opt := range opts {
				if opt.Value == v {
					opts[i] = createOption(device)
					break
				}
			}

			model.Render()
		}
	}
}

func createOption(dev *device.Device1) rofi.Option {
	baseCmd := "connect"
	if dev.Properties.Connected {
		baseCmd = "disconnect"
	}

	states := []string{}
	if dev.Properties.Paired {
		states = append(states, "Paired")

	}
	if dev.Properties.Trusted {
		states = append(states, "Trusted")
	}

	opt := rofi.Option{
		Label:    formatLabel(dev),
		Category: fmt.Sprintf("<span size=\"small\" color=\"#C3C3C3\">%s</span>", strings.Join(states, ", ")),
		Value:    dev.Properties.Address,
		Icon:     dev.Properties.Icon,
		Cmds:     []string{baseCmd, "controls"},

		IsMultiline: true,
		UseMarkup:   true,
	}
	return opt
}

func formatLabel(device *device.Device1) string {
	label := "\uf0c1  "
	if !device.Properties.Connected {
		label = "\uf127  "
	}
	label += device.Properties.Alias
	return label
}

func sendNotification(message string) {
	stat, _ := os.Stat("/usr/bin/notify-send")
	if stat == nil {
		return
	}
	exec.Command("notify-send", "-a", appName, message).Run()
}
