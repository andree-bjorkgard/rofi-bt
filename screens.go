package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/andree-bjorkgard/remote-bluetooth/v2/client"
	"github.com/andree-bjorkgard/rofi"
)

// Colours from the rofi theme.
const (
	colorOn       = "#61C766"
	colorDim      = "#928374"
	colorProgress = "#FDD835"
	colorError    = "#EC7875"
)

// Option values. rofi joins the value and the commands with "||", so values
// must not contain it; addresses and hostnames never do.
const (
	devPrefix = "dev|"
	actPrefix = "act|"
)

// listOptions builds the device list, one row per view in the given order.
func listOptions(views []deviceView, here string) []rofi.Option {
	opts := make([]rofi.Option, 0, len(views))
	for _, v := range views {
		status, detail, t := v.status()
		name := markupEscape(v.Alias)
		if name == "" {
			name = markupEscape(v.Address)
		}
		line := markupEscape(status)
		if t == toneOn {
			line = colored(colorOn, line)
		}
		if detail != "" {
			line += " " + markupEscape(detail)
		}
		if t == toneDim {
			name = colored(colorDim, name)
			line = colored(colorDim, line)
		}
		if v.Battery >= 0 {
			line += " · " + markupEscape(batteryLabel(uint8(v.Battery)))
		}
		opts = append(opts, rofi.Option{
			Label:       name,
			Info:        []string{smallMarkup(line)},
			Icon:        v.Icon,
			Value:       devPrefix + v.Address,
			Cmds:        []string{"open"},
			IsMultiline: true,
			UseMarkup:   true,
		})
	}
	return opts
}

// deviceOptions builds the rows of a device screen: its actions, the first
// one preselected by rofi. busy marks Play here as running; its row stays
// first even once the holders have let go and Play here is no longer offered.
func deviceOptions(v deviceView, busy bool) []rofi.Option {
	acts := v.actions()
	if busy && acts[0].Kind != actPlayHere {
		acts = append([]action{{Kind: actPlayHere}}, acts...)
	}
	opts := make([]rofi.Option, 0, len(acts))
	for _, a := range acts {
		o := rofi.Option{
			Label:       markupEscape(a.Label),
			Value:       fmt.Sprintf("%s%s|%d|%s", actPrefix, v.Address, int(a.Kind), a.Machine),
			Cmds:        []string{"run"},
			IsMultiline: true,
			UseMarkup:   true,
		}
		if a.Kind == actPlayHere && busy {
			o.Label = "Switching…"
		} else if a.Hint != "" {
			o.Info = []string{small(a.Hint)}
		}
		opts = append(opts, o)
	}
	return opts
}

// selection undoes rofi's split of a row's data on "||": a value ending in
// "|" (an action with no machine) comes back with that "|" on the command.
func selection(v rofi.Value) rofi.Value {
	if cmd, ok := strings.CutPrefix(v.Cmd, "|"); ok {
		v.Value += "|"
		v.Cmd = cmd
	}
	return v
}

// parseAction decodes an action row's value.
func parseAction(value string) (addr string, kind actionKind, machine string, ok bool) {
	rest, found := strings.CutPrefix(value, actPrefix)
	if !found {
		return "", 0, "", false
	}
	parts := strings.SplitN(rest, "|", 3)
	if len(parts) != 3 || parts[0] == "" {
		return "", 0, "", false
	}
	k, err := strconv.Atoi(parts[1])
	if err != nil || k < int(actPlayHere) || k > int(actBack) {
		return "", 0, "", false
	}
	return parts[0], actionKind(k), parts[2], true
}

// listMessage is the list screen's message: this machine and the others.
func listMessage(here string, others []string) string {
	msg := markupEscape(here) + " (this computer)"
	if len(others) > 0 {
		escaped := make([]string, len(others))
		for i, o := range others {
			escaped[i] = markupEscape(o)
		}
		msg += " · " + strings.Join(escaped, ", ")
	}
	return msg
}

// deviceMessage is a device screen's message. holders, when set, are the
// machines that let go for a running Play here.
func deviceMessage(v deviceView, holders []string) string {
	var lines []string
	if len(holders) > 0 {
		verb := "has"
		if len(holders) > 1 {
			verb = "have"
		}
		lines = append(lines, colored(colorProgress, markupEscape(
			fmt.Sprintf("… Moving the sound here: %s %s let go", strings.Join(holders, " and "), verb))))
	}
	for _, l := range v.message() {
		lines = append(lines, markupEscape(l))
	}
	return strings.Join(lines, "\n")
}

// errorLine is an error shown under a screen's message.
func errorLine(msg string) string {
	return colored(colorError, markupEscape(msg))
}

// playHereNotice is the notification summary and body for a finished Play here.
func playHereNotice(alias string, res client.PlayHereResult, err error) (summary, body string) {
	switch {
	case errors.Is(err, client.ErrNothingPlayingHere):
		return "Nothing is playing here", "start the sound here, then try again"
	case err != nil:
		return "Could not move the sound", err.Error()
	}
	verb := "is"
	if len(res.Released) > 1 {
		verb = "are"
	}
	return alias + ": sound moved here",
		fmt.Sprintf("%s let go and %s back to normal", strings.Join(res.Released, " and "), verb)
}

// stableOrder keeps the addresses of order that are still in current, in
// their order, and appends the new ones in current's order.
func stableOrder(order, current []string) []string {
	out := make([]string, 0, len(current))
	for _, a := range order {
		if slices.Contains(current, a) {
			out = append(out, a)
		}
	}
	for _, a := range current {
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	return out
}

func colored(color, markup string) string {
	return fmt.Sprintf("<span color=%q>%s</span>", color, markup)
}

// smallMarkup wraps already escaped markup in the small grey second line.
func smallMarkup(markup string) string {
	return fmt.Sprintf("<span size=\"small\" color=\"#C3C3C3\">%s</span>", markup)
}
