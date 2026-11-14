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
	// noMachine stands for an empty machine in an action value, so the
	// value never holds "||". No hostname is "-".
	noMachine = "-"
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
			Value:       actionValue(v.Address, a),
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

// actionValue encodes an action row's value as act|<ADDR>|<machine>|<kind>.
// rofi joins value and commands with "||" and splits on it again, so the
// value must neither hold "||" nor end in "|": the kind comes last and an
// empty machine is written as noMachine.
func actionValue(addr string, a action) string {
	machine := a.Machine
	if machine == "" {
		machine = noMachine
	}
	return fmt.Sprintf("%s%s|%s|%d", actPrefix, addr, machine, int(a.Kind))
}

// parseAction decodes a value made by actionValue.
func parseAction(value string) (addr string, kind actionKind, machine string, ok bool) {
	rest, found := strings.CutPrefix(value, actPrefix)
	if !found {
		return "", 0, "", false
	}
	parts := strings.SplitN(rest, "|", 3)
	if len(parts) != 3 || parts[0] == "" {
		return "", 0, "", false
	}
	k, err := strconv.Atoi(parts[2])
	if err != nil || k < int(actPlayHere) || k > int(actBack) {
		return "", 0, "", false
	}
	machine = parts[1]
	if machine == noMachine {
		machine = ""
	}
	return parts[0], actionKind(k), machine, true
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
	// Only the servers that restored cleanly are back to normal; the others
	// restore themselves when their hold runs out.
	var restored, failed []string
	for _, m := range res.Released {
		if _, ok := res.Failed[m]; ok {
			failed = append(failed, m)
		} else {
			restored = append(restored, m)
		}
	}
	var sentences []string
	if len(restored) > 0 {
		verb := "is"
		if len(restored) > 1 {
			verb = "are"
		}
		sentences = append(sentences, fmt.Sprintf("%s let go and %s back to normal", strings.Join(restored, " and "), verb))
	}
	if len(failed) > 0 {
		self := "it will restore itself"
		if len(failed) > 1 {
			self = "they will restore themselves"
		}
		sentences = append(sentences, fmt.Sprintf("%s could not be restored yet; %s within 15 seconds.", strings.Join(failed, " and "), self))
	}
	return alias + ": sound moved here", strings.Join(sentences, ". ")
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
