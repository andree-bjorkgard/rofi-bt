package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/andree-bjorkgard/remote-bluetooth/v2/bluez"
	"github.com/andree-bjorkgard/remote-bluetooth/v2/client"
)

// presence is one machine that knows the device.
type presence struct {
	Machine  string
	Here     bool
	Dev      bluez.Device
	Released bool
}

// deviceView is one device merged across this machine and the others.
type deviceView struct {
	Address string
	Alias   string
	Icon    string
	Battery int // percent, -1 when no connected machine reports one
	On      []presence
}

type tone int

const (
	toneOn tone = iota
	toneNeutral
	toneDim
)

type actionKind int

const (
	actPlayHere actionKind = iota
	actMoveHere
	actConnectHere
	actConnectOn
	actDisconnectHere
	actDisconnectOn
	actBack
)

type action struct {
	Kind    actionKind
	Machine string
	Label   string
	Hint    string
}

// merge joins trusted devices by address. Order is as first seen (local, then
// remote); presences are this machine first, then by machine name. here is the
// name of this machine.
func merge(here string, local []bluez.Device, remote []client.Device) []deviceView {
	var order []string
	byAddr := map[string]*deviceView{}
	add := func(p presence) {
		if !p.Dev.Trusted {
			return
		}
		v := byAddr[p.Dev.Address]
		if v == nil {
			v = &deviceView{Address: p.Dev.Address, Alias: p.Dev.Alias, Icon: p.Dev.Icon}
			byAddr[p.Dev.Address] = v
			order = append(order, p.Dev.Address)
		}
		v.On = append(v.On, p)
	}
	for _, d := range local {
		add(presence{Machine: here, Here: true, Dev: d})
	}
	for _, d := range remote {
		add(presence{Machine: d.Server, Dev: d.Device, Released: d.Released})
	}
	views := make([]deviceView, 0, len(order))
	for _, addr := range order {
		v := byAddr[addr]
		sort.SliceStable(v.On, func(i, j int) bool {
			a, b := v.On[i], v.On[j]
			if a.Here != b.Here {
				return a.Here
			}
			return a.Machine < b.Machine
		})
		if p := v.hereP(); p != nil && p.Dev.Alias != "" {
			v.Alias = p.Dev.Alias
		}
		v.Battery = -1
		for _, p := range v.On { // here first
			if p.Dev.Connected && p.Dev.HasBattery {
				v.Battery = int(p.Dev.BatteryPercent)
				break
			}
		}
		views = append(views, *v)
	}
	return views
}

func (v deviceView) hereP() *presence {
	for i := range v.On {
		if v.On[i].Here {
			return &v.On[i]
		}
	}
	return nil
}

func (v deviceView) connectedHere() bool {
	p := v.hereP()
	return p != nil && p.Dev.Connected
}

// machines returns the names of presences matching f, "here" for this machine.
func (v deviceView) machines(f func(presence) bool) []string {
	var out []string
	for _, p := range v.On {
		if f(p) {
			out = append(out, label(p))
		}
	}
	return out
}

// others returns the names of other machines' presences matching f.
func (v deviceView) others(f func(presence) bool) []string {
	var out []string
	for _, p := range v.On {
		if !p.Here && f(p) {
			out = append(out, p.Machine)
		}
	}
	return out
}

func label(p presence) string {
	if p.Here {
		return "here"
	}
	return p.Machine
}

func connected(p presence) bool { return p.Dev.Connected }
func sending(p presence) bool   { return p.Dev.Audio == bluez.AudioSending }

// soundFrom is the "▶ Sound from …" text, empty when nothing is sending.
func (v deviceView) soundFrom() string {
	s := v.machines(sending)
	switch len(s) {
	case 0:
		return ""
	case 1:
		return "▶ Sound from " + s[0]
	}
	return "▶ Sound from both"
}

func (v deviceView) status() (status, detail string, t tone) {
	// The sound line has no detail, so it fits beside the battery; the
	// device screen says where it is connected.
	if s := v.soundFrom(); s != "" {
		return s, "", toneOn
	}
	elsewhere := v.others(connected)
	switch {
	case v.connectedHere():
		if len(elsewhere) > 0 {
			detail = "· and on " + strings.Join(elsewhere, " and ")
		}
		return "Connected here", detail, toneOn
	case len(elsewhere) > 0:
		return "Connected on " + strings.Join(elsewhere, " and "), "", toneNeutral
	}
	if v.hereP() == nil {
		detail = "· paired on " + strings.Join(v.others(func(presence) bool { return true }), " and ")
	}
	return "Not connected", detail, toneDim
}

// toggle is what Enter does on the list: connect or disconnect, here when the
// device is paired here, otherwise on the machine that has it.
func (v deviceView) toggle() action {
	if v.hereP() != nil {
		if v.connectedHere() {
			return action{Kind: actDisconnectHere}
		}
		return action{Kind: actConnectHere}
	}
	if on := v.others(connected); len(on) > 0 {
		return action{Kind: actDisconnectOn, Machine: on[0]}
	}
	return action{Kind: actConnectOn, Machine: v.others(func(presence) bool { return true })[0]}
}

func (v deviceView) actions() []action {
	var acts []action
	here := v.hereP()
	connHere := v.connectedHere()
	elsewhere := v.others(connected)
	if s := v.others(sending); connHere && len(s) > 0 {
		acts = append(acts, action{Kind: actPlayHere, Label: "Play here",
			Hint: strings.Join(s, " and ") + " lets go for a moment and the sound comes here. Both stay connected."})
	}
	if !connHere && len(elsewhere) > 0 {
		acts = append(acts, action{Kind: actMoveHere, Label: "Move here",
			Hint: "Disconnect on " + strings.Join(elsewhere, " and ") + ", then connect here"})
	}
	if !connHere && len(elsewhere) == 0 {
		if here != nil {
			acts = append(acts, action{Kind: actConnectHere, Label: "Connect here"})
		}
		for _, m := range v.others(func(presence) bool { return true }) {
			acts = append(acts, action{Kind: actConnectOn, Machine: m, Label: "Connect on " + m})
		}
	}
	if connHere {
		a := action{Kind: actDisconnectHere, Label: "Disconnect here"}
		if len(elsewhere) > 0 {
			a.Hint = strings.Join(elsewhere, " and ") + " keeps them"
		}
		acts = append(acts, a)
	}
	for _, m := range elsewhere {
		hint := "It stays paired on both"
		if connHere {
			hint = "Only this computer keeps them"
		}
		acts = append(acts, action{Kind: actDisconnectOn, Machine: m, Label: "Disconnect on " + m, Hint: hint})
	}
	return append(acts, action{Kind: actBack, Label: "Back"})
}

func (v deviceView) message() []string {
	var lines []string
	if s := v.soundFrom(); s != "" {
		lines = append(lines, s)
	}
	var line string
	elsewhere := v.others(connected)
	switch {
	case v.connectedHere():
		line = "Connected here"
		if len(elsewhere) > 0 {
			line += " and on " + strings.Join(elsewhere, " and ")
		}
	case len(elsewhere) > 0:
		line = "Connected on " + strings.Join(elsewhere, " and ")
	default:
		line = "Not connected · paired " + pairedOn(v.machines(func(presence) bool { return true }))
	}
	if v.Battery >= 0 {
		line += fmt.Sprintf(" · battery %d%%", v.Battery)
	}
	return append(lines, line)
}

// pairedOn turns machine names into "here", "on m" or "here and on m".
func pairedOn(names []string) string {
	var out []string
	var on []string
	for _, n := range names {
		if n == "here" {
			out = append(out, "here")
		} else {
			on = append(on, n)
		}
	}
	if len(on) > 0 {
		out = append(out, "on "+strings.Join(on, " and "))
	}
	return strings.Join(out, " and ")
}
