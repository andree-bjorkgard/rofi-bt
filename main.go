package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/andree-bjorkgard/rofi"
	"github.com/muka/go-bluetooth/api"
	"github.com/muka/go-bluetooth/bluez"
	"github.com/muka/go-bluetooth/bluez/profile/battery"
	"github.com/muka/go-bluetooth/bluez/profile/device"
	"github.com/sirupsen/logrus"
)

const appName = "Bluetooth"
const deviceCacheName = "bluetooth-devices"

const BATTERY_UUID = "0000180f-0000-1000-8000-00805f9b34fb"

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

	var opts []rofi.Option
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
					case "Connected", "Disconnected":
						devUpdateCh <- dev.Properties.Address
					default:
					}
				}
			}(devCh)

		}
	}

	model.Options = rofi.SortUsingHistory(opts, deviceCacheName)
	model.Render()

	for {
		select {

		case v := <-eventCh:
			device, err := adapter.GetDeviceByAddress(v.Value)
			if err != nil {
				model.Message = fmt.Sprintf("Error getting device \"%s\": %s", v.Value, err)
			}

			rofi.SaveToHistory(deviceCacheName, v.Value)

			switch v.Cmd {
			case "connect":
				sendNotification(fmt.Sprintf("Connecting to device \"%s\"", device.Properties.Alias))
				err = device.Connect()
				if err != nil {
					model.Message = fmt.Sprintf("Error connecting to device \"%s\": %s", device.Properties.Alias, err)
					continue
				}

				// wait for connection to be established so the battery service is available
				time.Sleep(time.Second)
				if b := getBatteryLabel(device); b != "" {
					sendNotification(fmt.Sprintf("Connected to device \"%s\"\n%s", device.Properties.Alias, b))
				} else {
					sendNotification(fmt.Sprintf("Connected to device \"%s\"", device.Properties.Alias))
				}
			case "disconnect":
				sendNotification(fmt.Sprintf("Disconnecting device \"%s\"", device.Properties.Alias))
				err = device.Disconnect()
				if err != nil {
					model.Message = fmt.Sprintf("Error disconnecting device \"%s\": %s", device.Properties.Alias, err)
					continue
				}
				sendNotification(fmt.Sprintf("Disconnected from device \"%s\"", device.Properties.Alias))
			default:
				continue
			}

			for i, opt := range model.Options {
				if device.Properties.Address == opt.Value {
					model.Options[i] = createOption(device)
					break
				}
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

		}
		model.Render()
	}
}

func createOption(dev *device.Device1) rofi.Option {
	states := []string{}
	batteryLabel := ""

	baseCmd := "connect"
	connected, err := dev.GetConnected()
	if err != nil {
		log.Println("Error getting connected state:", err)
		return rofi.Option{}
	}
	if connected {
		baseCmd = "disconnect"
		batteryLabel = getBatteryLabel(dev)
	}

	paired, err := dev.GetPaired()
	if err != nil {
		log.Println("Error getting paired state:", err)
		return rofi.Option{}
	}
	if paired {
		states = append(states, "Paired")
	}

	trusted, err := dev.GetTrusted()
	if err != nil {
		log.Println("Error getting trusted state:", err)
		return rofi.Option{}
	}
	if trusted {
		states = append(states, "Trusted")
	}

	address, err := dev.GetAddress()
	if err != nil {
		log.Println("Error getting address:", err)
		return rofi.Option{}
	}

	icon, err := dev.GetIcon()
	if err != nil {
		log.Println("Error getting icon:", err)
		return rofi.Option{}
	}

	category := fmt.Sprintf("<span size=\"small\" color=\"#C3C3C3\">%s</span>", strings.Join(states, ", "))
	if batteryLabel != "" {
		category += fmt.Sprintf("\n<span size=\"small\" color=\"#C3C3C3\">%s</span>", batteryLabel)
	}

	opt := rofi.Option{
		Label:       formatLabel(dev),
		Category:    category,
		Value:       address,
		Icon:        icon,
		Cmds:        []string{baseCmd, "controls"},
		IsMultiline: true,
		UseMarkup:   true,
	}
	return opt
}

func formatLabel(device *device.Device1) string {
	label := "\uf0c1  "
	connected, err := device.GetConnected()
	if err != nil {
		log.Println("Error getting connected status:", err)
		return ""
	}
	if !connected {
		label = "\uf127  "
	}
	alias, err := device.GetAlias()
	if err != nil {
		log.Println("Error getting alias:", err)
		return ""
	}
	label += alias
	return label
}

func getBatteryLabel(dev *device.Device1) string {
	for _, uuid := range dev.Properties.UUIDs {
		if uuid == BATTERY_UUID {
			b, err := battery.NewBattery1(dev.Path())
			if err != nil {
				log.Println("Error getting battery service:", err)
				return ""
			}
			p, err := b.GetPercentage()
			if err != nil {
				log.Println("Error getting battery percentage:", err)
				return ""
			}
			icon := "\uf240"
			switch {
			case p >= 90:
				icon = "\uf240"
			case p >= 70:
				icon = "\uf241"
			case p >= 50:
				icon = "\uf242"
			case p >= 30:
				icon = "\uf243"
			default:
				icon = "\uf244"
			}
			return fmt.Sprintf("%s   %d%%", icon, p)
		}
	}

	return ""
}

func sendNotification(message string) {
	stat, _ := os.Stat("/usr/bin/notify-send")
	if stat == nil {
		return
	}
	exec.Command("notify-send", "-a", appName, message).Run()
}
