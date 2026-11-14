package main

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"
)

// The action icons ship with rofi-bt: icon themes differ per machine and
// most lack light action icons for a dark menu. rofi takes icons by name or
// by file path, so they are written out once and passed by path.
//
//go:embed icons/*.svg
var iconFiles embed.FS

// iconDir is where installIcons wrote the icons; empty if it could not.
var iconDir string

// installIcons writes the icons to $XDG_CACHE_HOME/rofi-bt/icons, rewriting
// only those that changed. Without them the action rows have no icon.
func installIcons() error {
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(cache, "rofi-bt", "icons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := iconFiles.ReadDir("icons")
	if err != nil {
		return err
	}
	for _, e := range entries {
		data, err := iconFiles.ReadFile("icons/" + e.Name())
		if err != nil {
			return err
		}
		path := filepath.Join(dir, e.Name())
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
			continue
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	iconDir = dir
	return nil
}

// actionIcon is the icon path for an action kind, or "" without icons.
func actionIcon(k actionKind) string {
	if iconDir == "" {
		return ""
	}
	var name string
	switch k {
	case actPlayHere:
		name = "play-here"
	case actMoveHere:
		name = "move-here"
	case actConnectHere, actConnectOn:
		name = "connect"
	case actDisconnectHere, actDisconnectOn:
		name = "disconnect"
	case actBack:
		name = "back"
	default:
		return ""
	}
	return filepath.Join(iconDir, name+".svg")
}
