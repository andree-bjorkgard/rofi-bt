package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	rbt "github.com/andree-bjorkgard/remote-bluetooth/pkg/client"
	rconfig "github.com/andree-bjorkgard/remote-bluetooth/pkg/config"
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

type BtDevice interface {
	GetAlias() (string, error)
	GetAddress() (string, error)
	GetTrusted() (bool, error)
	GetPaired() (bool, error)
	GetConnected() (bool, error)
	GetIcon() (string, error)
}

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

	rcfg := rconfig.NewConfig()
	rclient := rbt.NewClient(rcfg)

	go rclient.FindServers()

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

	remoteDevices := map[string]*rbt.Device{}
	for {
		select {

		case de := <-rclient.GetDeviceEventsChannel():
			devAddr := fmt.Sprintf("%s/%s", de.Device.Host, de.Device.Address)
			remoteDevices[devAddr] = de.Device
			exists := false
			for i, opt := range model.Options {
				if opt.Value == devAddr {
					model.Options[i] = createOption(de.Device)
					exists = true
					break
				}
			}

			if !exists {
				model.Options = append(model.Options, createOption(de.Device))
			}

		case v := <-eventCh:
			var dev BtDevice

			// remote device connect/disconnect
			if s := strings.Split(v.Value, "/"); len(s) > 1 {
				device := remoteDevices[v.Value]
				switch v.Cmd {
				case "connect":
					sendNotification(fmt.Sprintf("Remotely connecting to device \"%s\"", device.Name))
					if err := rclient.ConnectToDevice(s[0], s[1]); err != nil {
						model.Message = fmt.Sprintf("Error connecting to device \"%s\": %s", v.Value, err)
						continue
					}
					device.Connected = true
					sendNotification(fmt.Sprintf("Remotely connected to device \"%s\"", device.Name))

				case "disconnect":
					sendNotification(fmt.Sprintf("Remotely disconnecting from device \"%s\"", device.Name))
					if err := rclient.DisconnectFromDevice(s[0], s[1]); err != nil {
						model.Message = fmt.Sprintf("Error disconnecting from device \"%s\": %s", v.Value, err)
						continue
					}
					device.Connected = false
					sendNotification(fmt.Sprintf("Remotely disconnected from device \"%s\"", device.Name))
				}

				dev = device

				// local device connect/disconnect
			} else {
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
					if b := getBatteryStatus(device); b != "" {
						sendNotification(fmt.Sprintf("Connected to device \"%s\"\n%s", device.Properties.Alias, getBatteryLabel(b)))
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

				dev = device
			}

			for i, opt := range model.Options {
				if v.Value == opt.Value {
					model.Options[i] = createOption(dev)
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

func createOption(dev BtDevice) rofi.Option {
	batteryLabel := ""

	baseCmd := "connect"
	connected, err := dev.GetConnected()
	if err != nil {
		log.Println("Error getting connected state:", err)
		return rofi.Option{}
	}

	if connected {
		baseCmd = "disconnect"
		switch v := dev.(type) {
		case *device.Device1:
			batteryLabel = getBatteryStatus(v)
		case *rbt.Device:
			status, _ := v.GetBatteryStatus()
			batteryLabel = status
		}
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

	var info []string
	switch v := dev.(type) {
	case *device.Device1:
		info = append(info, fmt.Sprintf("<span size=\"small\" color=\"#C3C3C3\">%s</span>", address))
	case *rbt.Device:
		info = append(info, fmt.Sprintf("<span size=\"small\" color=\"#C3C3C3\">%s/%s</span>", v.Host, address))
		address = fmt.Sprintf("%s/%s", v.Host, address)
	}

	if batteryLabel != "" {
		info = append(info, fmt.Sprintf("<span size=\"small\" color=\"#C3C3C3\">%s</span>", getBatteryLabel(batteryLabel)))
	}

	opt := rofi.Option{
		Label:       formatLabel(dev),
		Info:        info,
		Value:       address,
		Icon:        icon,
		Cmds:        []string{baseCmd, "controls"},
		IsMultiline: true,
		UseMarkup:   true,
	}

	return opt
}

func formatLabel(device BtDevice) string {
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

func getBatteryStatus(dev *device.Device1) string {
	isConnected, _ := dev.GetConnected()
	if isConnected {
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

				return fmt.Sprintf("%d", p)
			}
		}
	}

	return ""
}

func getBatteryLabel(percent string) string {
	icon := "\uf240"
	p, err := strconv.Atoi(percent)
	if err != nil {
		log.Println("Error converting percent to int:", err)
		return ""
	}
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
	return fmt.Sprintf("%s   %s%%", icon, percent)
}

func sendNotification(message string) {
	stat, _ := os.Stat("/usr/bin/notify-send")
	if stat == nil {
		return
	}
	exec.Command("notify-send", "-a", appName, message).Run()
}
